package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/display"
)

var ErrNotFound = errors.New("not found")

const (
	activeJobStatuses      = "'queued', 'downloading_audio', 'converting_audio', 'splitting_audio', 'transcribing'"
	interruptedJobStatuses = "'downloading_audio', 'converting_audio', 'splitting_audio', 'transcribing'"
	transcriptionJobSelect = `
		SELECT id, media_item_id, status, attempt_count, COALESCE(last_error, ''), created_at, COALESCE(started_at, ''), COALESCE(finished_at, '')
		FROM transcription_jobs
	`
	summaryRequestColumns = `id, media_item_id, chat_id, user_id, COALESCE(message_id, 0), prompt_hash, prompt_text, status, COALESCE(summary_cache_id, 0), COALESCE(error, ''), created_at, updated_at`
	summaryRequestSelect  = `
		SELECT ` + summaryRequestColumns + `
		FROM summary_requests
	`
	summaryRequestMessageColumns = `id, summary_request_id, chat_id, telegram_message_id, kind, COALESCE(deleted_at, ''), created_at`
	watchFeedColumns             = `id, provider, provider_feed_id, canonical_url, COALESCE(title, ''), status, COALESCE(last_checked_at, ''), COALESCE(last_error, ''), created_at, updated_at`
	watchSubscriptionColumns     = `id, feed_id, chat_id, chat_type, COALESCE(chat_title, ''), created_by_user_id, created_at`
	watchEpisodeColumns          = `id, feed_id, provider_episode_id, COALESCE(media_item_id, 0), canonical_url, COALESCE(title, ''), COALESCE(pub_date, ''), status, first_seen_at, COALESCE(processed_at, ''), COALESCE(last_error, '')`
)

type Repository struct {
	db *sql.DB
}

func NewRepository(database *sql.DB) *Repository {
	return &Repository{db: database}
}

func (r *Repository) UpsertWhitelistedGroup(ctx context.Context, group WhitelistedGroup) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO auth_whitelisted_groups(chat_id, title, created_at, created_by_user_id)
		VALUES (?, ?, CURRENT_TIMESTAMP, ?)
		ON CONFLICT(chat_id) DO UPDATE SET title = excluded.title, created_by_user_id = excluded.created_by_user_id
	`, group.ChatID, group.Title, group.CreatedByUserID)
	return wrapErr(err, "upsert whitelisted group")
}

func (r *Repository) RemoveWhitelistedGroup(ctx context.Context, chatID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM auth_whitelisted_groups WHERE chat_id = ?`, chatID)
	return wrapErr(err, "remove whitelisted group")
}

func (r *Repository) ListWhitelistedGroups(ctx context.Context) ([]WhitelistedGroup, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT chat_id, COALESCE(title, ''), created_at, created_by_user_id FROM auth_whitelisted_groups ORDER BY chat_id`)
	if err != nil {
		return nil, fmt.Errorf("list whitelisted groups: %w", err)
	}
	defer rows.Close()

	var groups []WhitelistedGroup
	for rows.Next() {
		var group WhitelistedGroup
		if err := rows.Scan(&group.ChatID, &group.Title, &group.CreatedAt, &group.CreatedByUserID); err != nil {
			return nil, fmt.Errorf("scan whitelisted group: %w", err)
		}
		groups = append(groups, group)
	}
	return groups, wrapErr(rows.Err(), "list whitelisted groups")
}

func (r *Repository) IsGroupWhitelisted(ctx context.Context, chatID int64) (bool, error) {
	return exists(ctx, r.db, `SELECT 1 FROM auth_whitelisted_groups WHERE chat_id = ?`, chatID)
}

func (r *Repository) UpsertWhitelistedDMUser(ctx context.Context, user WhitelistedDMUser) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO auth_whitelisted_dm_users(user_id, username, first_name, created_at, created_by_user_id)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP, ?)
		ON CONFLICT(user_id) DO UPDATE SET username = excluded.username, first_name = excluded.first_name, created_by_user_id = excluded.created_by_user_id
	`, user.UserID, user.Username, user.FirstName, user.CreatedByUserID)
	return wrapErr(err, "upsert whitelisted DM user")
}

func (r *Repository) RemoveWhitelistedDMUser(ctx context.Context, userID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM auth_whitelisted_dm_users WHERE user_id = ?`, userID)
	return wrapErr(err, "remove whitelisted DM user")
}

func (r *Repository) ListWhitelistedDMUsers(ctx context.Context) ([]WhitelistedDMUser, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT user_id, COALESCE(username, ''), COALESCE(first_name, ''), created_at, created_by_user_id FROM auth_whitelisted_dm_users ORDER BY user_id`)
	if err != nil {
		return nil, fmt.Errorf("list whitelisted DM users: %w", err)
	}
	defer rows.Close()

	var users []WhitelistedDMUser
	for rows.Next() {
		var user WhitelistedDMUser
		if err := rows.Scan(&user.UserID, &user.Username, &user.FirstName, &user.CreatedAt, &user.CreatedByUserID); err != nil {
			return nil, fmt.Errorf("scan whitelisted DM user: %w", err)
		}
		users = append(users, user)
	}
	return users, wrapErr(rows.Err(), "list whitelisted DM users")
}

func (r *Repository) IsDMUserWhitelisted(ctx context.Context, userID int64) (bool, error) {
	return exists(ctx, r.db, `SELECT 1 FROM auth_whitelisted_dm_users WHERE user_id = ?`, userID)
}

func (r *Repository) CreateOrLoadMedia(ctx context.Context, provider, providerMediaID, canonicalURL string) (MediaItem, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return MediaItem{}, false, fmt.Errorf("begin create or load media: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO media_items(provider, provider_media_id, canonical_url, status, created_at, updated_at)
		VALUES (?, ?, ?, 'new', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, provider, providerMediaID, canonicalURL)
	if err != nil {
		return MediaItem{}, false, fmt.Errorf("insert media item: %w", err)
	}
	created := rowsAffected(result) > 0

	media, err := scanMedia(tx.QueryRowContext(ctx, `
		SELECT id, provider, provider_media_id, canonical_url, COALESCE(title, ''), COALESCE(duration_seconds, 0), status, COALESCE(status_detail, ''), COALESCE(transcript_source, ''), COALESCE(transcript_text, ''), created_at, updated_at
		FROM media_items WHERE provider = ? AND provider_media_id = ?
	`, provider, providerMediaID))
	if err != nil {
		return MediaItem{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return MediaItem{}, false, fmt.Errorf("commit create or load media: %w", err)
	}
	return media, created, nil
}

func (r *Repository) BackfillMediaTitle(ctx context.Context, mediaItemID int64, title string) (MediaItem, error) {
	if strings.TrimSpace(title) != "" {
		if _, err := r.db.ExecContext(ctx, `
			UPDATE media_items
			SET title = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ? AND COALESCE(title, '') = ''
		`, title, mediaItemID); err != nil {
			return MediaItem{}, fmt.Errorf("backfill media title: %w", err)
		}
	}
	return r.GetMedia(ctx, mediaItemID)
}

func (r *Repository) FindSummaryDisplayMetadata(ctx context.Context, mediaItemID int64) (display.SummaryMetadata, bool, error) {
	var metadata display.SummaryMetadata
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(wf.title, ''), wf.canonical_url, COALESCE(we.title, ''), COALESCE(we.pub_date, ''), we.canonical_url
		FROM watch_episodes we
		JOIN watch_feeds wf ON wf.id = we.feed_id
		WHERE we.media_item_id = ?
		ORDER BY we.first_seen_at DESC, we.id DESC
		LIMIT 1
	`, mediaItemID).Scan(&metadata.PodcastTitle, &metadata.PodcastURL, &metadata.EpisodeTitle, &metadata.PubDate, &metadata.Link)
	if errors.Is(err, sql.ErrNoRows) {
		return display.SummaryMetadata{}, false, nil
	}
	if err != nil {
		return display.SummaryMetadata{}, false, fmt.Errorf("find summary display metadata: %w", err)
	}
	return metadata, true, nil
}

func (r *Repository) UpdateMediaStatus(ctx context.Context, mediaItemID int64, status, detail string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE media_items SET status = ?, status_detail = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, status, detail, mediaItemID)
	return wrapErr(err, "update media status")
}

func (r *Repository) UpdateMediaTranscript(ctx context.Context, mediaItemID int64, source, transcriptText string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE media_items SET status = 'transcript_ready', status_detail = NULL, transcript_source = ?, transcript_text = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND COALESCE(transcript_text, '') = ''`, source, transcriptText, mediaItemID)
	return wrapErr(err, "update media transcript")
}

func (r *Repository) GetMedia(ctx context.Context, mediaItemID int64) (MediaItem, error) {
	return scanMedia(r.db.QueryRowContext(ctx, `
		SELECT id, provider, provider_media_id, canonical_url, COALESCE(title, ''), COALESCE(duration_seconds, 0), status, COALESCE(status_detail, ''), COALESCE(transcript_source, ''), COALESCE(transcript_text, ''), created_at, updated_at
		FROM media_items WHERE id = ?
	`, mediaItemID))
}

func (r *Repository) FindMedia(ctx context.Context, provider, providerMediaID string) (MediaItem, bool, error) {
	media, err := scanMedia(r.db.QueryRowContext(ctx, `
		SELECT id, provider, provider_media_id, canonical_url, COALESCE(title, ''), COALESCE(duration_seconds, 0), status, COALESCE(status_detail, ''), COALESCE(transcript_source, ''), COALESCE(transcript_text, ''), created_at, updated_at
		FROM media_items WHERE provider = ? AND provider_media_id = ?
	`, provider, providerMediaID))
	if errors.Is(err, ErrNotFound) {
		return MediaItem{}, false, nil
	}
	if err != nil {
		return MediaItem{}, false, err
	}
	return media, true, nil
}

func (r *Repository) FindLatestTranscriptionJob(ctx context.Context, mediaItemID int64) (TranscriptionJob, bool, error) {
	job, err := scanJob(r.db.QueryRowContext(ctx, transcriptionJobSelect+`
		WHERE media_item_id = ? ORDER BY created_at DESC, id DESC LIMIT 1
	`, mediaItemID))
	if errors.Is(err, ErrNotFound) {
		return TranscriptionJob{}, false, nil
	}
	if err != nil {
		return TranscriptionJob{}, false, err
	}
	return job, true, nil
}

func (r *Repository) CreateOrLoadActiveTranscriptionJob(ctx context.Context, mediaItemID int64) (TranscriptionJob, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return TranscriptionJob{}, false, fmt.Errorf("begin create or load active transcription job: %w", err)
	}
	defer tx.Rollback()

	transcriptReady, err := restoreTranscriptReadyMediaIfNeeded(ctx, tx, mediaItemID)
	if err != nil {
		return TranscriptionJob{}, false, err
	}
	if transcriptReady {
		if _, err := tx.ExecContext(ctx, `UPDATE transcription_jobs SET status = 'completed', finished_at = CURRENT_TIMESTAMP WHERE media_item_id = ? AND status IN (`+activeJobStatuses+`)`, mediaItemID); err != nil {
			return TranscriptionJob{}, false, fmt.Errorf("complete stale active transcription jobs: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return TranscriptionJob{}, false, fmt.Errorf("commit transcript-ready job skip: %w", err)
		}
		return TranscriptionJob{}, false, nil
	}

	job, found, err := loadActiveTranscriptionJob(ctx, tx, mediaItemID)
	if err != nil {
		return TranscriptionJob{}, false, err
	}
	if found {
		if err := tx.Commit(); err != nil {
			return TranscriptionJob{}, false, fmt.Errorf("commit load active transcription job: %w", err)
		}
		return job, false, nil
	}

	result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO transcription_jobs(media_item_id, status, created_at) VALUES (?, 'queued', CURRENT_TIMESTAMP)`, mediaItemID)
	if err != nil {
		return TranscriptionJob{}, false, fmt.Errorf("insert transcription job: %w", err)
	}
	created := rowsAffected(result) > 0
	job, found, err = loadActiveTranscriptionJob(ctx, tx, mediaItemID)
	if err != nil {
		return TranscriptionJob{}, false, err
	}
	if !found {
		return TranscriptionJob{}, false, fmt.Errorf("load active transcription job: %w", ErrNotFound)
	}
	if created {
		if _, err := tx.ExecContext(ctx, `UPDATE media_items SET status = 'queued', updated_at = CURRENT_TIMESTAMP WHERE id = ? AND COALESCE(transcript_text, '') = ''`, mediaItemID); err != nil {
			return TranscriptionJob{}, false, fmt.Errorf("set media queued: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return TranscriptionJob{}, false, fmt.Errorf("commit create transcription job: %w", err)
	}
	return job, created, nil
}

func loadActiveTranscriptionJob(ctx context.Context, tx *sql.Tx, mediaItemID int64) (TranscriptionJob, bool, error) {
	job, err := scanJob(tx.QueryRowContext(ctx, transcriptionJobSelect+`
		WHERE media_item_id = ? AND status IN (`+activeJobStatuses+`)
		ORDER BY created_at LIMIT 1
	`, mediaItemID))
	if errors.Is(err, ErrNotFound) {
		return TranscriptionJob{}, false, nil
	}
	if err != nil {
		return TranscriptionJob{}, false, err
	}
	return job, true, nil
}

func mediaHasTranscript(ctx context.Context, tx *sql.Tx, mediaItemID int64) (bool, error) {
	var transcriptText string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(transcript_text, '') FROM media_items WHERE id = ?`, mediaItemID).Scan(&transcriptText); err != nil {
		return false, fmt.Errorf("check media transcript: %w", err)
	}
	return transcriptText != "", nil
}

func restoreTranscriptReadyMedia(ctx context.Context, tx *sql.Tx, mediaItemID int64) error {
	_, err := tx.ExecContext(ctx, `UPDATE media_items SET status = 'transcript_ready', status_detail = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND COALESCE(transcript_text, '') != ''`, mediaItemID)
	return wrapErr(err, "restore transcript-ready media")
}

func restoreTranscriptReadyMediaIfNeeded(ctx context.Context, tx *sql.Tx, mediaItemID int64) (bool, error) {
	hasTranscript, err := mediaHasTranscript(ctx, tx, mediaItemID)
	if err != nil {
		return false, err
	}
	if !hasTranscript {
		return false, nil
	}
	return true, restoreTranscriptReadyMedia(ctx, tx, mediaItemID)
}

func restoreAllTranscriptReadyMedia(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `UPDATE media_items SET status = 'transcript_ready', status_detail = NULL, updated_at = CURRENT_TIMESTAMP WHERE COALESCE(transcript_text, '') != ''`)
	return wrapErr(err, "restore transcript-ready media")
}

func (r *Repository) RequeueInterruptedTranscriptionJobs(ctx context.Context) (int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin requeue interrupted transcription jobs: %w", err)
	}
	defer tx.Rollback()

	if err := restoreAllTranscriptReadyMedia(ctx, tx); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE transcription_jobs SET status = 'completed', finished_at = CURRENT_TIMESTAMP WHERE status IN (`+activeJobStatuses+`) AND media_item_id IN (SELECT id FROM media_items WHERE COALESCE(transcript_text, '') != '')`); err != nil {
		return 0, fmt.Errorf("complete transcript-ready transcription jobs: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE media_items SET status = 'queued', updated_at = CURRENT_TIMESTAMP WHERE COALESCE(transcript_text, '') = '' AND id IN (SELECT media_item_id FROM transcription_jobs WHERE status IN (`+interruptedJobStatuses+`))`); err != nil {
		return 0, fmt.Errorf("requeue interrupted media items: %w", err)
	}
	result, err := tx.ExecContext(ctx, `UPDATE transcription_jobs SET status = 'queued', started_at = NULL WHERE status IN (`+interruptedJobStatuses+`) AND media_item_id IN (SELECT id FROM media_items WHERE COALESCE(transcript_text, '') = '')`)
	if err != nil {
		return 0, fmt.Errorf("requeue interrupted transcription jobs: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count requeued transcription jobs: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit requeue interrupted transcription jobs: %w", err)
	}
	return count, nil
}

func (r *Repository) ClaimOldestQueuedTranscriptionJob(ctx context.Context) (TranscriptionJob, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return TranscriptionJob{}, false, fmt.Errorf("begin claim transcription job: %w", err)
	}
	defer tx.Rollback()

	if err := restoreAllTranscriptReadyMedia(ctx, tx); err != nil {
		return TranscriptionJob{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE transcription_jobs SET status = 'completed', finished_at = CURRENT_TIMESTAMP WHERE status = 'queued' AND media_item_id IN (SELECT id FROM media_items WHERE COALESCE(transcript_text, '') != '')`); err != nil {
		return TranscriptionJob{}, false, fmt.Errorf("complete transcript-ready queued jobs: %w", err)
	}

	job, err := scanJob(tx.QueryRowContext(ctx, transcriptionJobSelect+`
		WHERE status = 'queued'
		  AND media_item_id IN (SELECT id FROM media_items WHERE COALESCE(transcript_text, '') = '')
		ORDER BY created_at, id LIMIT 1
	`))
	if errors.Is(err, ErrNotFound) {
		return TranscriptionJob{}, false, tx.Commit()
	}
	if err != nil {
		return TranscriptionJob{}, false, err
	}

	if _, err := tx.ExecContext(ctx, `UPDATE transcription_jobs SET status = 'downloading_audio', started_at = CURRENT_TIMESTAMP WHERE id = ?`, job.ID); err != nil {
		return TranscriptionJob{}, false, fmt.Errorf("claim transcription job: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE media_items SET status = 'downloading_audio', updated_at = CURRENT_TIMESTAMP WHERE id = ?`, job.MediaItemID); err != nil {
		return TranscriptionJob{}, false, fmt.Errorf("set media downloading audio: %w", err)
	}
	job, err = scanJob(tx.QueryRowContext(ctx, transcriptionJobSelect+`
		WHERE id = ?
	`, job.ID))
	if err != nil {
		return TranscriptionJob{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return TranscriptionJob{}, false, fmt.Errorf("commit claim transcription job: %w", err)
	}
	return job, true, nil
}

func (r *Repository) UpdateTranscriptionJobStatus(ctx context.Context, jobID int64, status string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE transcription_jobs SET status = ? WHERE id = ?`, status, jobID)
	return wrapErr(err, "update transcription job status")
}

func (r *Repository) UpdateTranscriptionJobAndMediaStatus(ctx context.Context, jobID, mediaItemID int64, status string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin update job and media status: %w", err)
	}
	defer tx.Rollback()
	transcriptReady, err := restoreTranscriptReadyMediaIfNeeded(ctx, tx, mediaItemID)
	if err != nil {
		return err
	}
	if transcriptReady {
		if _, err := tx.ExecContext(ctx, `UPDATE transcription_jobs SET status = 'completed', finished_at = CURRENT_TIMESTAMP WHERE id = ?`, jobID); err != nil {
			return fmt.Errorf("complete transcript-ready transcription job: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit update job and media status: %w", err)
		}
		return nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE transcription_jobs SET status = ? WHERE id = ?`, status, jobID); err != nil {
		return fmt.Errorf("update transcription job status: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE media_items SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, status, mediaItemID); err != nil {
		return fmt.Errorf("update media status: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit update job and media status: %w", err)
	}
	return nil
}

func (r *Repository) CompleteTranscriptionJobWithTranscript(ctx context.Context, jobID, mediaItemID int64, source, transcriptText string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin complete transcription job: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE media_items SET status = 'transcript_ready', status_detail = NULL, transcript_source = ?, transcript_text = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND COALESCE(transcript_text, '') = ''`, source, transcriptText, mediaItemID); err != nil {
		return fmt.Errorf("store transcript: %w", err)
	}
	if err := restoreTranscriptReadyMedia(ctx, tx, mediaItemID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE transcription_jobs SET status = 'completed', finished_at = CURRENT_TIMESTAMP WHERE id = ?`, jobID); err != nil {
		return fmt.Errorf("mark transcription job complete: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit complete transcription job: %w", err)
	}
	return nil
}

func (r *Repository) MarkTranscriptionJobComplete(ctx context.Context, jobID int64) error {
	_, err := r.db.ExecContext(ctx, `UPDATE transcription_jobs SET status = 'completed', finished_at = CURRENT_TIMESTAMP WHERE id = ?`, jobID)
	return wrapErr(err, "mark transcription job complete")
}

func (r *Repository) MarkTranscriptionJobRetryQueued(ctx context.Context, jobID, mediaItemID int64, lastError string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin retry transcription job: %w", err)
	}
	defer tx.Rollback()
	transcriptReady, err := restoreTranscriptReadyMediaIfNeeded(ctx, tx, mediaItemID)
	if err != nil {
		return err
	}
	if transcriptReady {
		if _, err := tx.ExecContext(ctx, `UPDATE transcription_jobs SET status = 'completed', last_error = ?, finished_at = CURRENT_TIMESTAMP WHERE id = ?`, lastError, jobID); err != nil {
			return fmt.Errorf("complete transcript-ready retry job: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit retry transcription job: %w", err)
		}
		return nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE transcription_jobs SET status = 'queued', attempt_count = attempt_count + 1, last_error = ?, started_at = NULL, finished_at = NULL WHERE id = ?`, lastError, jobID); err != nil {
		return fmt.Errorf("mark transcription job retry queued: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE media_items SET status = 'queued', updated_at = CURRENT_TIMESTAMP WHERE id = ?`, mediaItemID); err != nil {
		return fmt.Errorf("mark media retry queued: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit retry transcription job: %w", err)
	}
	return nil
}

func (r *Repository) MarkTranscriptionJobFailed(ctx context.Context, jobID int64, lastError string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE transcription_jobs SET status = 'failed', attempt_count = attempt_count + 1, last_error = ?, finished_at = CURRENT_TIMESTAMP WHERE id = ?`, lastError, jobID)
	return wrapErr(err, "mark transcription job failed")
}

func (r *Repository) MarkTranscriptionJobFailedAndMediaFailed(ctx context.Context, jobID, mediaItemID int64, lastError string) (bool, []SummaryRequest, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, nil, fmt.Errorf("begin fail transcription job: %w", err)
	}
	defer tx.Rollback()
	transcriptReady, err := restoreTranscriptReadyMediaIfNeeded(ctx, tx, mediaItemID)
	if err != nil {
		return false, nil, err
	}
	if transcriptReady {
		if _, err := tx.ExecContext(ctx, `UPDATE transcription_jobs SET status = 'completed', last_error = ?, finished_at = CURRENT_TIMESTAMP WHERE id = ?`, lastError, jobID); err != nil {
			return false, nil, fmt.Errorf("complete transcript-ready failed job: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return false, nil, fmt.Errorf("commit fail transcription job: %w", err)
		}
		return false, nil, nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE transcription_jobs SET status = 'failed', attempt_count = attempt_count + 1, last_error = ?, finished_at = CURRENT_TIMESTAMP WHERE id = ?`, lastError, jobID); err != nil {
		return false, nil, fmt.Errorf("mark transcription job failed: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE media_items SET status = 'failed', status_detail = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, lastError, mediaItemID); err != nil {
		return false, nil, fmt.Errorf("mark media failed: %w", err)
	}
	failedRequests, err := markPendingSummaryRequestsFailed(ctx, tx, mediaItemID, lastError)
	if err != nil {
		return false, nil, err
	}
	if err := tx.Commit(); err != nil {
		return false, nil, fmt.Errorf("commit fail transcription job: %w", err)
	}
	return true, failedRequests, nil
}

func markPendingSummaryRequestsFailed(ctx context.Context, tx *sql.Tx, mediaItemID int64, message string) ([]SummaryRequest, error) {
	rows, err := tx.QueryContext(ctx, `
		UPDATE summary_requests
		SET status = 'failed', error = ?, updated_at = CURRENT_TIMESTAMP
		WHERE media_item_id = ? AND status IN ('pending_transcript', 'pending_summary')
		RETURNING `+summaryRequestColumns+`
	`, message, mediaItemID)
	if err != nil {
		return nil, fmt.Errorf("mark pending summary requests failed: %w", err)
	}
	defer rows.Close()

	var requests []SummaryRequest
	for rows.Next() {
		request, err := scanSummaryRequestRows(rows)
		if err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	return requests, wrapErr(rows.Err(), "mark pending summary requests failed")
}

func (r *Repository) CreateSummaryRequest(ctx context.Context, request SummaryRequest) (SummaryRequest, error) {
	result, err := r.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO summary_requests(media_item_id, chat_id, user_id, message_id, prompt_hash, prompt_text, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, request.MediaItemID, request.ChatID, request.UserID, nullableInt64(request.MessageID), request.PromptHash, request.PromptText, request.Status)
	if err != nil {
		return SummaryRequest{}, fmt.Errorf("create summary request: %w", err)
	}
	if rowsAffected(result) == 0 {
		if request.MessageID != 0 {
			return r.getSummaryRequestByMessage(ctx, request.ChatID, request.MessageID)
		}
		return r.getSummaryRequestByWatchKey(ctx, request.MediaItemID, request.ChatID, request.PromptHash)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return SummaryRequest{}, fmt.Errorf("get summary request id: %w", err)
	}
	return r.getSummaryRequest(ctx, id)
}

func (r *Repository) ListPendingRequestsForMedia(ctx context.Context, mediaItemID int64) ([]SummaryRequest, error) {
	rows, err := r.db.QueryContext(ctx, summaryRequestSelect+`
		WHERE media_item_id = ? AND status IN ('pending_transcript', 'pending_summary')
		ORDER BY created_at, id
	`, mediaItemID)
	if err != nil {
		return nil, fmt.Errorf("list pending summary requests: %w", err)
	}
	defer rows.Close()

	var requests []SummaryRequest
	for rows.Next() {
		request, err := scanSummaryRequestRows(rows)
		if err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	return requests, wrapErr(rows.Err(), "list pending summary requests")
}

func (r *Repository) IsFirstRequestForMediaChat(ctx context.Context, requestID, mediaItemID, chatID int64) (bool, error) {
	var firstID int64
	err := r.db.QueryRowContext(ctx, `
		SELECT id FROM summary_requests
		WHERE media_item_id = ? AND chat_id = ? AND COALESCE(message_id, 0) != 0
		ORDER BY created_at, id
		LIMIT 1
	`, mediaItemID, chatID).Scan(&firstID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("find first summary request for chat: %w", err)
	}
	return firstID == requestID, nil
}

func (r *Repository) ListProgressOwnerRequestsForMedia(ctx context.Context, mediaItemID int64) ([]SummaryRequest, error) {
	rows, err := r.db.QueryContext(ctx, summaryRequestSelect+`
		WHERE id IN (
			SELECT MIN(id)
			FROM summary_requests
			WHERE media_item_id = ? AND COALESCE(message_id, 0) != 0 AND status IN ('pending_transcript', 'pending_summary', 'summarizing', 'sending')
			GROUP BY chat_id
		)
		ORDER BY created_at, id
	`, mediaItemID)
	if err != nil {
		return nil, fmt.Errorf("list progress owner requests: %w", err)
	}
	defer rows.Close()

	var requests []SummaryRequest
	for rows.Next() {
		request, err := scanSummaryRequestRows(rows)
		if err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	return requests, wrapErr(rows.Err(), "list progress owner requests")
}

func (r *Repository) CreateSummaryRequestMessage(ctx context.Context, message SummaryRequestMessage) (SummaryRequestMessage, error) {
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO summary_request_messages(summary_request_id, chat_id, telegram_message_id, kind, created_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, message.SummaryRequestID, message.ChatID, message.TelegramMessageID, message.Kind)
	if err != nil {
		return SummaryRequestMessage{}, fmt.Errorf("create summary request message: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return SummaryRequestMessage{}, fmt.Errorf("get summary request message id: %w", err)
	}
	return scanSummaryRequestMessage(r.db.QueryRowContext(ctx, `
		SELECT `+summaryRequestMessageColumns+` FROM summary_request_messages WHERE id = ?
	`, id))
}

func (r *Repository) LatestActiveSummaryRequestMessage(ctx context.Context, requestID int64, kind string) (SummaryRequestMessage, bool, error) {
	message, err := scanSummaryRequestMessage(r.db.QueryRowContext(ctx, `
		SELECT `+summaryRequestMessageColumns+`
		FROM summary_request_messages
		WHERE summary_request_id = ? AND kind = ? AND deleted_at IS NULL
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, requestID, kind))
	if errors.Is(err, ErrNotFound) {
		return SummaryRequestMessage{}, false, nil
	}
	if err != nil {
		return SummaryRequestMessage{}, false, err
	}
	return message, true, nil
}

func (r *Repository) ListActiveSummaryRequestMessagesByKind(ctx context.Context, requestID int64, kinds []string) ([]SummaryRequestMessage, error) {
	if len(kinds) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(kinds))
	args := make([]any, 0, len(kinds)+1)
	args = append(args, requestID)
	for _, kind := range kinds {
		placeholders[len(args)-1] = "?"
		args = append(args, kind)
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+summaryRequestMessageColumns+`
		FROM summary_request_messages
		WHERE deleted_at IS NULL AND summary_request_id = ? AND kind IN (`+strings.Join(placeholders, ", ")+`)
		ORDER BY created_at, id
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("list active summary request messages by kind: %w", err)
	}
	defer rows.Close()

	var messages []SummaryRequestMessage
	for rows.Next() {
		message, err := scanSummaryRequestMessageRows(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, wrapErr(rows.Err(), "list active summary request messages by kind")
}

func (r *Repository) ListActiveSummaryRequestMessages(ctx context.Context, requestIDs []int64) ([]SummaryRequestMessage, error) {
	if len(requestIDs) == 0 {
		return nil, nil
	}
	placeholders, args := placeholdersForInt64s(requestIDs)
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+summaryRequestMessageColumns+`
		FROM summary_request_messages
		WHERE deleted_at IS NULL AND summary_request_id IN (`+placeholders+`)
		ORDER BY created_at, id
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("list active summary request messages: %w", err)
	}
	defer rows.Close()

	var messages []SummaryRequestMessage
	for rows.Next() {
		message, err := scanSummaryRequestMessageRows(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, wrapErr(rows.Err(), "list active summary request messages")
}

func (r *Repository) MarkSummaryRequestMessageDeleted(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `UPDATE summary_request_messages SET deleted_at = COALESCE(deleted_at, CURRENT_TIMESTAMP) WHERE id = ?`, id)
	return wrapErr(err, "mark summary request message deleted")
}

func (r *Repository) MarkSummaryRequestMessagesDeleted(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders, args := placeholdersForInt64s(ids)
	_, err := r.db.ExecContext(ctx, `UPDATE summary_request_messages SET deleted_at = COALESCE(deleted_at, CURRENT_TIMESTAMP) WHERE id IN (`+placeholders+`)`, args...)
	return wrapErr(err, "mark summary request messages deleted")
}

func (r *Repository) FindSummaryCache(ctx context.Context, mediaItemID int64, promptHash, model string) (SummaryCache, bool, error) {
	cache, err := scanSummaryCache(r.db.QueryRowContext(ctx, `
		SELECT id, media_item_id, prompt_hash, prompt_text, summary_text, model, created_at
		FROM summary_cache WHERE media_item_id = ? AND prompt_hash = ? AND model = ?
	`, mediaItemID, promptHash, model))
	if errors.Is(err, ErrNotFound) {
		return SummaryCache{}, false, nil
	}
	if err != nil {
		return SummaryCache{}, false, err
	}
	return cache, true, nil
}

func (r *Repository) InsertSummaryCache(ctx context.Context, cache SummaryCache) (SummaryCache, error) {
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO summary_cache(media_item_id, prompt_hash, prompt_text, summary_text, model, created_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, cache.MediaItemID, cache.PromptHash, cache.PromptText, cache.SummaryText, cache.Model)
	if err != nil {
		return SummaryCache{}, fmt.Errorf("insert summary cache: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return SummaryCache{}, fmt.Errorf("get summary cache id: %w", err)
	}
	return scanSummaryCache(r.db.QueryRowContext(ctx, `
		SELECT id, media_item_id, prompt_hash, prompt_text, summary_text, model, created_at FROM summary_cache WHERE id = ?
	`, id))
}

func (r *Repository) RequeueInterruptedSummaryRequests(ctx context.Context) (int64, error) {
	return r.requeueSummaryRequests(ctx, "")
}

func (r *Repository) RequeueStaleSummaryRequests(ctx context.Context, before time.Time) (int64, error) {
	return r.requeueSummaryRequests(ctx, before.UTC().Format(time.DateTime))
}

func (r *Repository) EnqueuePendingTranscriptionRequests(ctx context.Context) (int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin enqueue pending transcription requests: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO transcription_jobs(media_item_id, status, created_at)
		SELECT DISTINCT sr.media_item_id, 'queued', CURRENT_TIMESTAMP
		FROM summary_requests sr
		JOIN media_items mi ON mi.id = sr.media_item_id
		WHERE sr.status = 'pending_transcript'
		  AND COALESCE(mi.transcript_text, '') = ''
		  AND NOT EXISTS (
			  SELECT 1 FROM transcription_jobs tj
			  WHERE tj.media_item_id = sr.media_item_id AND tj.status IN (`+activeJobStatuses+`)
		  )
	`)
	if err != nil {
		return 0, fmt.Errorf("enqueue pending transcription requests: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count enqueued transcription requests: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE media_items
		SET status = 'queued', updated_at = CURRENT_TIMESTAMP
		WHERE COALESCE(transcript_text, '') = ''
		  AND id IN (SELECT media_item_id FROM transcription_jobs WHERE status = 'queued')
	`); err != nil {
		return 0, fmt.Errorf("mark pending transcription media queued: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit enqueue pending transcription requests: %w", err)
	}
	return count, nil
}

func (r *Repository) requeueSummaryRequests(ctx context.Context, updatedBefore string) (int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin requeue summary requests: %w", err)
	}
	defer tx.Rollback()

	condition := ""
	args := []any{}
	if updatedBefore != "" {
		condition = " AND updated_at < ?"
		args = append(args, updatedBefore)
	}

	requeued, err := requeueSummaryRequestsByStatus(ctx, tx, "summarizing", "pending_summary", "", condition, args...)
	if err != nil {
		return 0, err
	}
	deliveryUnknown, err := requeueSummaryRequestsByStatus(ctx, tx, "sending", "delivery_unknown", "delivery status unknown after interruption", condition, args...)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit requeue summary requests: %w", err)
	}
	return requeued + deliveryUnknown, nil
}

func requeueSummaryRequestsByStatus(ctx context.Context, tx *sql.Tx, from, to, message, condition string, args ...any) (int64, error) {
	setError := ""
	queryArgs := []any{to}
	if message != "" {
		setError = ", error = ?"
		queryArgs = append(queryArgs, message)
	}
	queryArgs = append(queryArgs, from)
	queryArgs = append(queryArgs, args...)

	result, err := tx.ExecContext(ctx, `UPDATE summary_requests SET status = ?`+setError+`, updated_at = CURRENT_TIMESTAMP WHERE status = ?`+condition, queryArgs...)
	if err != nil {
		return 0, fmt.Errorf("requeue summary requests from %s to %s: %w", from, to, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count requeued summary requests from %s to %s: %w", from, to, err)
	}
	return count, nil
}

func (r *Repository) ListMediaIDsWithPendingSummaryRequests(ctx context.Context) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT sr.media_item_id
		FROM summary_requests sr
		JOIN media_items mi ON mi.id = sr.media_item_id
		WHERE COALESCE(mi.transcript_text, '') != '' AND sr.status IN ('pending_transcript', 'pending_summary')
		ORDER BY sr.media_item_id
	`)
	if err != nil {
		return nil, fmt.Errorf("list media with pending summary requests: %w", err)
	}
	defer rows.Close()

	var mediaIDs []int64
	for rows.Next() {
		var mediaID int64
		if err := rows.Scan(&mediaID); err != nil {
			return nil, fmt.Errorf("scan media with pending summary requests: %w", err)
		}
		mediaIDs = append(mediaIDs, mediaID)
	}
	return mediaIDs, wrapErr(rows.Err(), "list media with pending summary requests")
}

func (r *Repository) MarkSummaryRequestSummarizing(ctx context.Context, requestID int64) error {
	result, err := r.db.ExecContext(ctx, `UPDATE summary_requests SET status = 'summarizing', updated_at = CURRENT_TIMESTAMP WHERE id = ? AND status = 'pending_summary'`, requestID)
	return wrapUpdateErr(result, err, "mark summary request summarizing")
}

func (r *Repository) MarkSummaryRequestPendingSummary(ctx context.Context, requestID int64) error {
	result, err := r.db.ExecContext(ctx, `UPDATE summary_requests SET status = 'pending_summary', updated_at = CURRENT_TIMESTAMP WHERE id = ? AND status = 'pending_transcript'`, requestID)
	return wrapUpdateErr(result, err, "mark summary request pending summary")
}

func (r *Repository) MarkSummaryRequestSending(ctx context.Context, requestID int64) error {
	result, err := r.db.ExecContext(ctx, `UPDATE summary_requests SET status = 'sending', updated_at = CURRENT_TIMESTAMP WHERE id = ? AND status = 'summarizing'`, requestID)
	return wrapUpdateErr(result, err, "mark summary request sending")
}

func (r *Repository) MarkSummaryRequestSent(ctx context.Context, requestID, summaryCacheID int64) error {
	result, err := r.db.ExecContext(ctx, `UPDATE summary_requests SET status = 'sent', summary_cache_id = ?, error = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND status = 'sending'`, summaryCacheID, requestID)
	return wrapUpdateErr(result, err, "mark summary request sent")
}

func (r *Repository) UpdateSummaryRequestCache(ctx context.Context, requestID, summaryCacheID int64) error {
	result, err := r.db.ExecContext(ctx, `UPDATE summary_requests SET summary_cache_id = ?, error = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND status = 'sent'`, summaryCacheID, requestID)
	return wrapUpdateErr(result, err, "update summary request cache")
}

func (r *Repository) MarkSummaryRequestFailed(ctx context.Context, requestID int64, message string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE summary_requests SET status = 'failed', error = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND status IN ('pending_transcript', 'pending_summary', 'summarizing', 'sending')`, message, requestID)
	return wrapUpdateErr(result, err, "mark summary request failed")
}

func (r *Repository) MarkSummaryRequestRetryPending(ctx context.Context, requestID int64, message string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE summary_requests SET status = 'pending_summary', error = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND status IN ('summarizing', 'sending')`, message, requestID)
	return wrapUpdateErr(result, err, "mark summary request retry pending")
}

func (r *Repository) MarkSummaryRequestDeliveryUnknown(ctx context.Context, requestID int64, message string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE summary_requests SET status = 'delivery_unknown', error = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND status = 'sending'`, message, requestID)
	return wrapUpdateErr(result, err, "mark summary request delivery unknown")
}

func (r *Repository) GetSummaryRequest(ctx context.Context, id int64) (SummaryRequest, error) {
	return r.getSummaryRequest(ctx, id)
}

func (r *Repository) getSummaryRequest(ctx context.Context, id int64) (SummaryRequest, error) {
	return scanSummaryRequest(r.db.QueryRowContext(ctx, summaryRequestSelect+`
		WHERE id = ?
	`, id))
}

func (r *Repository) getSummaryRequestByMessage(ctx context.Context, chatID, messageID int64) (SummaryRequest, error) {
	return scanSummaryRequest(r.db.QueryRowContext(ctx, summaryRequestSelect+`
		WHERE chat_id = ? AND message_id = ?
	`, chatID, messageID))
}

func (r *Repository) getSummaryRequestByWatchKey(ctx context.Context, mediaItemID, chatID int64, promptHash string) (SummaryRequest, error) {
	return scanSummaryRequest(r.db.QueryRowContext(ctx, summaryRequestSelect+`
		WHERE media_item_id = ? AND chat_id = ? AND prompt_hash = ? AND message_id IS NULL AND status != 'failed'
	`, mediaItemID, chatID, promptHash))
}

func (r *Repository) CreateOrLoadWatchFeed(ctx context.Context, feed WatchFeed) (WatchFeed, bool, error) {
	status := feed.Status
	if status == "" {
		status = "active"
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return WatchFeed{}, false, fmt.Errorf("begin create or load watch feed: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO watch_feeds(provider, provider_feed_id, canonical_url, title, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, feed.Provider, feed.ProviderFeedID, feed.CanonicalURL, feed.Title, status)
	if err != nil {
		return WatchFeed{}, false, fmt.Errorf("insert watch feed: %w", err)
	}
	created := rowsAffected(result) > 0

	loaded, err := scanWatchFeed(tx.QueryRowContext(ctx, `
		SELECT `+watchFeedColumns+`
		FROM watch_feeds
		WHERE provider = ? AND provider_feed_id = ?
	`, feed.Provider, feed.ProviderFeedID))
	if err != nil {
		return WatchFeed{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return WatchFeed{}, false, fmt.Errorf("commit create or load watch feed: %w", err)
	}
	return loaded, created, nil
}

func (r *Repository) BackfillWatchFeedTitle(ctx context.Context, feedID int64, title string) (WatchFeed, error) {
	if strings.TrimSpace(title) != "" {
		if _, err := r.db.ExecContext(ctx, `
			UPDATE watch_feeds
			SET title = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ? AND COALESCE(title, '') = ''
		`, title, feedID); err != nil {
			return WatchFeed{}, fmt.Errorf("backfill watch feed title: %w", err)
		}
	}
	return scanWatchFeed(r.db.QueryRowContext(ctx, `
		SELECT `+watchFeedColumns+`
		FROM watch_feeds
		WHERE id = ?
	`, feedID))
}

func (r *Repository) UpsertWatchSubscription(ctx context.Context, sub WatchSubscription) (WatchSubscription, error) {
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO watch_subscriptions(feed_id, chat_id, chat_type, chat_title, created_by_user_id, created_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(feed_id, chat_id) DO UPDATE SET
			chat_type = excluded.chat_type,
			chat_title = excluded.chat_title,
			created_by_user_id = excluded.created_by_user_id
	`, sub.FeedID, sub.ChatID, sub.ChatType, sub.ChatTitle, sub.CreatedByUserID)
	if err != nil {
		return WatchSubscription{}, fmt.Errorf("upsert watch subscription: %w", err)
	}
	if rowsAffected(result) == 0 {
		return WatchSubscription{}, fmt.Errorf("upsert watch subscription: %w", ErrNotFound)
	}
	return scanWatchSubscription(r.db.QueryRowContext(ctx, `
		SELECT `+watchSubscriptionColumns+`
		FROM watch_subscriptions
		WHERE feed_id = ? AND chat_id = ?
	`, sub.FeedID, sub.ChatID))
}

func (r *Repository) RemoveWatchSubscription(ctx context.Context, provider, providerFeedID string, chatID int64) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
		DELETE FROM watch_subscriptions
		WHERE chat_id = ?
		  AND feed_id IN (SELECT id FROM watch_feeds WHERE provider = ? AND provider_feed_id = ?)
	`, chatID, provider, providerFeedID)
	if err != nil {
		return false, fmt.Errorf("remove watch subscription: %w", err)
	}
	return rowsAffected(result) > 0, nil
}

func (r *Repository) ListWatchSubscriptionsForChat(ctx context.Context, chatID int64) ([]WatchSubscription, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT ws.id, ws.feed_id, ws.chat_id, ws.chat_type, COALESCE(ws.chat_title, ''), ws.created_by_user_id, ws.created_at,
		       wf.id, wf.provider, wf.provider_feed_id, wf.canonical_url, COALESCE(wf.title, ''), wf.status, COALESCE(wf.last_checked_at, ''), COALESCE(wf.last_error, ''), wf.created_at, wf.updated_at
		FROM watch_subscriptions ws
		JOIN watch_feeds wf ON wf.id = ws.feed_id
		WHERE ws.chat_id = ? AND wf.status = 'active'
		ORDER BY wf.provider, COALESCE(wf.title, wf.canonical_url), ws.id
	`, chatID)
	if err != nil {
		return nil, fmt.Errorf("list watch subscriptions for chat: %w", err)
	}
	defer rows.Close()
	return scanWatchSubscriptionsWithFeeds(rows, "list watch subscriptions for chat")
}

func (r *Repository) ListActiveWatchFeeds(ctx context.Context) ([]WatchFeed, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT wf.id, wf.provider, wf.provider_feed_id, wf.canonical_url, COALESCE(wf.title, ''), wf.status, COALESCE(wf.last_checked_at, ''), COALESCE(wf.last_error, ''), wf.created_at, wf.updated_at
		FROM watch_feeds wf
		WHERE wf.status = 'active'
		  AND EXISTS (SELECT 1 FROM watch_subscriptions ws WHERE ws.feed_id = wf.id)
		ORDER BY COALESCE(wf.last_checked_at, ''), wf.id
	`)
	if err != nil {
		return nil, fmt.Errorf("list active watch feeds: %w", err)
	}
	defer rows.Close()

	var feeds []WatchFeed
	for rows.Next() {
		feed, err := scanWatchFeedRows(rows)
		if err != nil {
			return nil, err
		}
		feeds = append(feeds, feed)
	}
	return feeds, wrapErr(rows.Err(), "list active watch feeds")
}

func (r *Repository) ListWatchSubscriptionsForFeed(ctx context.Context, feedID int64) ([]WatchSubscription, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+watchSubscriptionColumns+`
		FROM watch_subscriptions
		WHERE feed_id = ?
		ORDER BY created_at, id
	`, feedID)
	if err != nil {
		return nil, fmt.Errorf("list watch subscriptions for feed: %w", err)
	}
	defer rows.Close()

	var subs []WatchSubscription
	for rows.Next() {
		sub, err := scanWatchSubscriptionRows(rows)
		if err != nil {
			return nil, err
		}
		subs = append(subs, sub)
	}
	return subs, wrapErr(rows.Err(), "list watch subscriptions for feed")
}

func (r *Repository) InsertBaselineWatchEpisodes(ctx context.Context, feedID int64, episodes []WatchEpisode) (int64, error) {
	if len(episodes) == 0 {
		return 0, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin insert baseline watch episodes: %w", err)
	}
	defer tx.Rollback()

	var inserted int64
	for _, episode := range episodes {
		result, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO watch_episodes(feed_id, provider_episode_id, canonical_url, title, pub_date, status, first_seen_at, processed_at)
			VALUES (?, ?, ?, ?, ?, 'baseline', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		`, feedID, episode.ProviderEpisodeID, episode.CanonicalURL, episode.Title, nullableString(episode.PubDate))
		if err != nil {
			return 0, fmt.Errorf("insert baseline watch episode: %w", err)
		}
		inserted += rowsAffected(result)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit insert baseline watch episodes: %w", err)
	}
	return inserted, nil
}

func (r *Repository) FindWatchEpisode(ctx context.Context, feedID int64, providerEpisodeID string) (WatchEpisode, bool, error) {
	episode, err := scanWatchEpisode(r.db.QueryRowContext(ctx, `
		SELECT `+watchEpisodeColumns+`
		FROM watch_episodes
		WHERE feed_id = ? AND provider_episode_id = ?
	`, feedID, providerEpisodeID))
	if errors.Is(err, ErrNotFound) {
		return WatchEpisode{}, false, nil
	}
	if err != nil {
		return WatchEpisode{}, false, err
	}
	return episode, true, nil
}

func (r *Repository) InsertWatchEpisodeQueued(ctx context.Context, episode WatchEpisode, mediaItemID int64) (WatchEpisode, bool, error) {
	if mediaItemID == 0 {
		return WatchEpisode{}, false, fmt.Errorf("insert queued watch episode: %w", ErrNotFound)
	}
	return r.insertWatchEpisode(ctx, episode, mediaItemID, "queued")
}

func (r *Repository) InsertWatchEpisodeSkipped(ctx context.Context, episode WatchEpisode) (WatchEpisode, bool, error) {
	return r.insertWatchEpisode(ctx, episode, 0, "skipped")
}

func (r *Repository) insertWatchEpisode(ctx context.Context, episode WatchEpisode, mediaItemID int64, status string) (WatchEpisode, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return WatchEpisode{}, false, fmt.Errorf("begin insert watch episode %s: %w", status, err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO watch_episodes(feed_id, provider_episode_id, media_item_id, canonical_url, title, pub_date, status, first_seen_at, processed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, episode.FeedID, episode.ProviderEpisodeID, nullableInt64(mediaItemID), episode.CanonicalURL, episode.Title, nullableString(episode.PubDate), status)
	if err != nil {
		return WatchEpisode{}, false, fmt.Errorf("insert watch episode %s: %w", status, err)
	}
	created := rowsAffected(result) > 0

	loaded, err := scanWatchEpisode(tx.QueryRowContext(ctx, `
		SELECT `+watchEpisodeColumns+`
		FROM watch_episodes
		WHERE feed_id = ? AND provider_episode_id = ?
	`, episode.FeedID, episode.ProviderEpisodeID))
	if err != nil {
		return WatchEpisode{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return WatchEpisode{}, false, fmt.Errorf("commit insert watch episode %s: %w", status, err)
	}
	return loaded, created, nil
}

func (r *Repository) MarkWatchEpisodeFailed(ctx context.Context, episodeID int64, message string) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE watch_episodes
		SET status = 'failed', last_error = ?, processed_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, message, episodeID)
	return wrapUpdateErr(result, err, "mark watch episode failed")
}

func (r *Repository) MarkWatchFeedChecked(ctx context.Context, feedID int64) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE watch_feeds
		SET last_checked_at = CURRENT_TIMESTAMP, last_error = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, feedID)
	return wrapUpdateErr(result, err, "mark watch feed checked")
}

func (r *Repository) MarkWatchFeedError(ctx context.Context, feedID int64, message string) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE watch_feeds
		SET last_error = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, message, feedID)
	return wrapUpdateErr(result, err, "mark watch feed error")
}

func scanMedia(row *sql.Row) (MediaItem, error) {
	var media MediaItem
	err := row.Scan(&media.ID, &media.Provider, &media.ProviderMediaID, &media.CanonicalURL, &media.Title, &media.DurationSeconds, &media.Status, &media.StatusDetail, &media.TranscriptSource, &media.TranscriptText, &media.CreatedAt, &media.UpdatedAt)
	return media, scanErr(err, "media item")
}

func scanJob(row *sql.Row) (TranscriptionJob, error) {
	var job TranscriptionJob
	err := row.Scan(&job.ID, &job.MediaItemID, &job.Status, &job.AttemptCount, &job.LastError, &job.CreatedAt, &job.StartedAt, &job.FinishedAt)
	return job, scanErr(err, "transcription job")
}

func scanSummaryCache(row *sql.Row) (SummaryCache, error) {
	var cache SummaryCache
	err := row.Scan(&cache.ID, &cache.MediaItemID, &cache.PromptHash, &cache.PromptText, &cache.SummaryText, &cache.Model, &cache.CreatedAt)
	return cache, scanErr(err, "summary cache")
}

func scanSummaryRequest(row *sql.Row) (SummaryRequest, error) {
	var request SummaryRequest
	err := row.Scan(&request.ID, &request.MediaItemID, &request.ChatID, &request.UserID, &request.MessageID, &request.PromptHash, &request.PromptText, &request.Status, &request.SummaryCacheID, &request.Error, &request.CreatedAt, &request.UpdatedAt)
	return request, scanErr(err, "summary request")
}

func scanSummaryRequestRows(rows *sql.Rows) (SummaryRequest, error) {
	var request SummaryRequest
	err := rows.Scan(&request.ID, &request.MediaItemID, &request.ChatID, &request.UserID, &request.MessageID, &request.PromptHash, &request.PromptText, &request.Status, &request.SummaryCacheID, &request.Error, &request.CreatedAt, &request.UpdatedAt)
	return request, scanErr(err, "summary request")
}

func scanSummaryRequestMessage(row *sql.Row) (SummaryRequestMessage, error) {
	var message SummaryRequestMessage
	err := row.Scan(&message.ID, &message.SummaryRequestID, &message.ChatID, &message.TelegramMessageID, &message.Kind, &message.DeletedAt, &message.CreatedAt)
	return message, scanErr(err, "summary request message")
}

func scanSummaryRequestMessageRows(rows *sql.Rows) (SummaryRequestMessage, error) {
	var message SummaryRequestMessage
	err := rows.Scan(&message.ID, &message.SummaryRequestID, &message.ChatID, &message.TelegramMessageID, &message.Kind, &message.DeletedAt, &message.CreatedAt)
	return message, scanErr(err, "summary request message")
}

func scanWatchFeed(row *sql.Row) (WatchFeed, error) {
	var feed WatchFeed
	err := row.Scan(&feed.ID, &feed.Provider, &feed.ProviderFeedID, &feed.CanonicalURL, &feed.Title, &feed.Status, &feed.LastCheckedAt, &feed.LastError, &feed.CreatedAt, &feed.UpdatedAt)
	return feed, scanErr(err, "watch feed")
}

func scanWatchFeedRows(rows *sql.Rows) (WatchFeed, error) {
	var feed WatchFeed
	err := rows.Scan(&feed.ID, &feed.Provider, &feed.ProviderFeedID, &feed.CanonicalURL, &feed.Title, &feed.Status, &feed.LastCheckedAt, &feed.LastError, &feed.CreatedAt, &feed.UpdatedAt)
	return feed, scanErr(err, "watch feed")
}

func scanWatchSubscription(row *sql.Row) (WatchSubscription, error) {
	var sub WatchSubscription
	err := row.Scan(&sub.ID, &sub.FeedID, &sub.ChatID, &sub.ChatType, &sub.ChatTitle, &sub.CreatedByUserID, &sub.CreatedAt)
	return sub, scanErr(err, "watch subscription")
}

func scanWatchSubscriptionRows(rows *sql.Rows) (WatchSubscription, error) {
	var sub WatchSubscription
	err := rows.Scan(&sub.ID, &sub.FeedID, &sub.ChatID, &sub.ChatType, &sub.ChatTitle, &sub.CreatedByUserID, &sub.CreatedAt)
	return sub, scanErr(err, "watch subscription")
}

func scanWatchSubscriptionsWithFeeds(rows *sql.Rows, action string) ([]WatchSubscription, error) {
	var subs []WatchSubscription
	for rows.Next() {
		var sub WatchSubscription
		err := rows.Scan(
			&sub.ID, &sub.FeedID, &sub.ChatID, &sub.ChatType, &sub.ChatTitle, &sub.CreatedByUserID, &sub.CreatedAt,
			&sub.Feed.ID, &sub.Feed.Provider, &sub.Feed.ProviderFeedID, &sub.Feed.CanonicalURL, &sub.Feed.Title, &sub.Feed.Status, &sub.Feed.LastCheckedAt, &sub.Feed.LastError, &sub.Feed.CreatedAt, &sub.Feed.UpdatedAt,
		)
		if err != nil {
			return nil, scanErr(err, "watch subscription with feed")
		}
		subs = append(subs, sub)
	}
	return subs, wrapErr(rows.Err(), action)
}

func scanWatchEpisode(row *sql.Row) (WatchEpisode, error) {
	var episode WatchEpisode
	err := row.Scan(&episode.ID, &episode.FeedID, &episode.ProviderEpisodeID, &episode.MediaItemID, &episode.CanonicalURL, &episode.Title, &episode.PubDate, &episode.Status, &episode.FirstSeenAt, &episode.ProcessedAt, &episode.LastError)
	return episode, scanErr(err, "watch episode")
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func placeholdersForInt64s(values []int64) (string, []any) {
	placeholders := make([]string, len(values))
	args := make([]any, len(values))
	for i, value := range values {
		placeholders[i] = "?"
		args[i] = value
	}
	return strings.Join(placeholders, ", "), args
}

func exists(ctx context.Context, database *sql.DB, query string, args ...any) (bool, error) {
	var value int
	err := database.QueryRowContext(ctx, query, args...).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func scanErr(err error, name string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s: %w", name, ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("scan %s: %w", name, err)
	}
	return nil
}

func rowsAffected(result sql.Result) int64 {
	count, err := result.RowsAffected()
	if err != nil {
		return 0
	}
	return count
}

func nullableInt64(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}

func wrapErr(err error, action string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", action, err)
}

func wrapUpdateErr(result sql.Result, err error, action string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	if rowsAffected(result) == 0 {
		return fmt.Errorf("%s: %w", action, ErrNotFound)
	}
	return nil
}
