package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestRunMigrationsCreatesSchema(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer database.Close()

	if err := RunMigrations(ctx, database); err != nil {
		t.Fatalf("RunMigrations returned error: %v", err)
	}

	for _, table := range []string{
		"schema_migrations",
		"auth_whitelisted_groups",
		"auth_whitelisted_dm_users",
		"media_items",
		"transcription_jobs",
		"summary_cache",
		"summary_requests",
		"watch_feeds",
		"watch_subscriptions",
		"watch_episodes",
	} {
		if !tableExists(t, database, table) {
			t.Fatalf("table %s does not exist", table)
		}
	}

	for _, index := range []string{
		"sqlite_autoindex_media_items_1",
		"sqlite_autoindex_summary_cache_1",
		"idx_summary_requests_media_status",
		"idx_summary_requests_chat_message",
		"idx_summary_requests_watch_idempotency",
		"idx_transcription_jobs_status_created",
		"idx_transcription_jobs_one_active_per_media",
		"sqlite_autoindex_watch_feeds_1",
		"sqlite_autoindex_watch_subscriptions_1",
		"sqlite_autoindex_watch_episodes_1",
		"idx_watch_feeds_status_checked",
		"idx_watch_subscriptions_chat",
		"idx_watch_episodes_feed_status",
		"idx_watch_episodes_feed_pub_date",
	} {
		if !indexExists(t, database, index) {
			t.Fatalf("index %s does not exist", index)
		}
	}
}

func TestRunMigrationsIsIdempotent(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer database.Close()

	if err := RunMigrations(ctx, database); err != nil {
		t.Fatalf("first RunMigrations returned error: %v", err)
	}
	if err := RunMigrations(ctx, database); err != nil {
		t.Fatalf("second RunMigrations returned error: %v", err)
	}
}

func TestMigrationUniqueConstraints(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer database.Close()
	if err := RunMigrations(ctx, database); err != nil {
		t.Fatalf("RunMigrations returned error: %v", err)
	}

	mustExec(t, database, `INSERT INTO media_items(provider, provider_media_id, canonical_url, status, created_at, updated_at) VALUES ('youtube', 'abc12345678', 'https://www.youtube.com/watch?v=abc12345678', 'transcript_ready', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
	if _, err := database.ExecContext(ctx, `INSERT INTO media_items(provider, provider_media_id, canonical_url, status, created_at, updated_at) VALUES ('youtube', 'abc12345678', 'https://www.youtube.com/watch?v=abc12345678', 'transcript_ready', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`); err == nil {
		t.Fatal("expected duplicate media provider ID to fail")
	}

	mustExec(t, database, `INSERT INTO summary_cache(media_item_id, prompt_hash, prompt_text, summary_text, model, created_at) VALUES (1, 'hash', 'prompt', 'summary', 'model', CURRENT_TIMESTAMP)`)
	if _, err := database.ExecContext(ctx, `INSERT INTO summary_cache(media_item_id, prompt_hash, prompt_text, summary_text, model, created_at) VALUES (1, 'hash', 'prompt', 'summary', 'model', CURRENT_TIMESTAMP)`); err == nil {
		t.Fatal("expected duplicate summary cache key to fail")
	}
}

func TestMigrationDeduplicatesWatchSummaryRequests(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer database.Close()
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations returned error: %v", err)
	}
	if _, err := database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("create schema_migrations returned error: %v", err)
	}
	for _, migration := range migrations[:4] {
		if err := applyMigration(ctx, database, migration); err != nil {
			t.Fatalf("apply migration %s returned error: %v", migration.Version, err)
		}
	}
	mustExec(t, database, `INSERT INTO media_items(provider, provider_media_id, canonical_url, status, created_at, updated_at) VALUES ('youtube', 'abc12345678', 'https://www.youtube.com/watch?v=abc12345678', 'transcript_ready', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
	mustExec(t, database, `INSERT INTO summary_requests(media_item_id, chat_id, user_id, message_id, prompt_hash, prompt_text, status, created_at, updated_at) VALUES (1, 10, 20, NULL, 'hash', 'prompt', 'pending_summary', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
	mustExec(t, database, `INSERT INTO summary_requests(media_item_id, chat_id, user_id, message_id, prompt_hash, prompt_text, status, created_at, updated_at) VALUES (1, 10, 21, NULL, 'hash', 'prompt', 'sending', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
	mustExec(t, database, `INSERT INTO summary_requests(media_item_id, chat_id, user_id, message_id, prompt_hash, prompt_text, status, created_at, updated_at) VALUES (1, 10, 22, NULL, 'hash', 'prompt', 'failed', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)

	if err := RunMigrations(ctx, database); err != nil {
		t.Fatalf("RunMigrations returned error: %v", err)
	}
	if !indexExists(t, database, "idx_summary_requests_watch_idempotency") {
		t.Fatal("idx_summary_requests_watch_idempotency does not exist")
	}
	var activeDuplicates int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM summary_requests WHERE message_id IS NULL AND status != 'failed'`).Scan(&activeDuplicates); err != nil {
		t.Fatalf("count active duplicates returned error: %v", err)
	}
	if activeDuplicates != 1 {
		t.Fatalf("active duplicate requests = %d", activeDuplicates)
	}
}

func TestMigrationDeduplicatesSummaryRequestMessages(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer database.Close()
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations returned error: %v", err)
	}
	if _, err := database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("create schema_migrations returned error: %v", err)
	}
	if err := applyMigration(ctx, database, migrations[0]); err != nil {
		t.Fatalf("apply first migration returned error: %v", err)
	}
	mustExec(t, database, `INSERT INTO media_items(provider, provider_media_id, canonical_url, status, created_at, updated_at) VALUES ('youtube', 'abc12345678', 'https://www.youtube.com/watch?v=abc12345678', 'transcript_ready', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
	mustExec(t, database, `INSERT INTO summary_requests(media_item_id, chat_id, user_id, message_id, prompt_hash, prompt_text, status, created_at, updated_at) VALUES (1, 10, 20, 30, 'hash', 'prompt', 'pending_summary', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
	mustExec(t, database, `INSERT INTO summary_requests(media_item_id, chat_id, user_id, message_id, prompt_hash, prompt_text, status, created_at, updated_at) VALUES (1, 10, 20, 30, 'hash2', 'prompt2', 'sent', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
	mustExec(t, database, `INSERT INTO summary_requests(media_item_id, chat_id, user_id, message_id, prompt_hash, prompt_text, status, created_at, updated_at) VALUES (1, 11, 21, 31, 'hash3', 'prompt3', 'pending_summary', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
	mustExec(t, database, `INSERT INTO summary_requests(media_item_id, chat_id, user_id, message_id, prompt_hash, prompt_text, status, created_at, updated_at) VALUES (1, 11, 21, 31, 'hash4', 'prompt4', 'pending_summary', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)

	if err := RunMigrations(ctx, database); err != nil {
		t.Fatalf("RunMigrations returned error: %v", err)
	}
	if !indexExists(t, database, "idx_summary_requests_chat_message") {
		t.Fatal("idx_summary_requests_chat_message does not exist")
	}
	var sentSurvivor int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM summary_requests WHERE chat_id = 10 AND message_id = 30 AND status = 'sent'`).Scan(&sentSurvivor); err != nil {
		t.Fatalf("count sent survivor returned error: %v", err)
	}
	if sentSurvivor != 1 {
		t.Fatalf("sent survivor requests = %d", sentSurvivor)
	}
	var processable int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM summary_requests WHERE chat_id = 11 AND status IN ('pending_transcript', 'pending_summary')`).Scan(&processable); err != nil {
		t.Fatalf("count processable requests returned error: %v", err)
	}
	if processable != 1 {
		t.Fatalf("processable duplicate requests = %d", processable)
	}
	var failedDuplicate int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM summary_requests WHERE message_id IS NULL AND status = 'failed'`).Scan(&failedDuplicate); err != nil {
		t.Fatalf("count failed duplicates returned error: %v", err)
	}
	if failedDuplicate != 2 {
		t.Fatalf("failed duplicate requests = %d", failedDuplicate)
	}
}

func TestActiveTranscriptionJobUniqueness(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer database.Close()
	if err := RunMigrations(ctx, database); err != nil {
		t.Fatalf("RunMigrations returned error: %v", err)
	}

	mustExec(t, database, `INSERT INTO media_items(provider, provider_media_id, canonical_url, status, created_at, updated_at) VALUES ('youtube', 'abc12345678', 'https://www.youtube.com/watch?v=abc12345678', 'queued', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
	mustExec(t, database, `INSERT INTO transcription_jobs(media_item_id, status, created_at) VALUES (1, 'queued', CURRENT_TIMESTAMP)`)

	if _, err := database.ExecContext(ctx, `INSERT INTO transcription_jobs(media_item_id, status, created_at) VALUES (1, 'transcribing', CURRENT_TIMESTAMP)`); err == nil {
		t.Fatal("expected duplicate active transcription job to fail")
	}

	mustExec(t, database, `INSERT INTO transcription_jobs(media_item_id, status, created_at) VALUES (1, 'failed', CURRENT_TIMESTAMP)`)
}

func tableExists(t *testing.T, database *sql.DB, table string) bool {
	t.Helper()
	var name string
	err := database.QueryRowContext(context.Background(), `SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
	return err == nil
}

func indexExists(t *testing.T, database *sql.DB, index string) bool {
	t.Helper()
	var name string
	err := database.QueryRowContext(context.Background(), `SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?`, index).Scan(&name)
	return err == nil
}
