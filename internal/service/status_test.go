package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/auth"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/db"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/provider"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/summarize"
)

const (
	statusProvider     = "youtube"
	statusMediaID      = "abc12345678"
	statusCanonicalURL = "https://www.youtube.com/watch?v=abc12345678"
	statusModel        = "model"
	statusRawURL       = "https://youtu.be/abc12345678"
)

func TestStatusServiceRejectsUnauthorizedCaller(t *testing.T) {
	service := newStatusService(fakeStatusAuth{allowed: false}, fakeStatusRegistry{}, newStatusRepo())

	report, err := service.Status(context.Background(), StatusQuery{ChatID: 10, UserID: 20, ChatType: auth.ChatTypePrivate, RawURL: statusRawURL})
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if report.Authorized || !strings.Contains(report.Text, "not authorized") {
		t.Fatalf("report = %#v", report)
	}
}

func TestStatusServiceReportsInvalidURL(t *testing.T) {
	service := newStatusService(fakeStatusAuth{allowed: true}, fakeStatusRegistry{err: errors.New("unsupported")}, newStatusRepo())

	report, err := service.Status(context.Background(), StatusQuery{ChatID: 10, UserID: 20, ChatType: auth.ChatTypePrivate, RawURL: "https://example.com"})
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if !report.Authorized || !report.InvalidURL || !strings.Contains(report.Text, "valid YouTube, Xiaoyuzhou, or SoundOn episode URL") {
		t.Fatalf("report = %#v", report)
	}
}

func TestStatusServiceReportsUnknownMedia(t *testing.T) {
	service := newStatusService(fakeStatusAuth{allowed: true}, fakeStatusRegistry{ref: statusRef()}, newStatusRepo())

	report, err := service.Status(context.Background(), StatusQuery{ChatID: 10, UserID: 20, ChatType: auth.ChatTypePrivate, RawURL: statusRawURL})
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if !report.Authorized || report.Found || !strings.Contains(report.Text, "No status found") {
		t.Fatalf("report = %#v", report)
	}
}

func TestStatusServiceReportsQueuedMedia(t *testing.T) {
	repo := newStatusRepo()
	media := repo.addMedia(db.MediaItem{Provider: statusProvider, ProviderMediaID: statusMediaID, CanonicalURL: statusCanonicalURL, Status: "queued"})
	repo.addJob(db.TranscriptionJob{MediaItemID: media.ID, Status: "queued"})
	service := newStatusService(fakeStatusAuth{allowed: true}, fakeStatusRegistry{ref: statusRef()}, repo)

	report, err := service.Status(context.Background(), StatusQuery{ChatID: 10, UserID: 20, ChatType: auth.ChatTypePrivate, RawURL: statusRawURL})
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	assertStatusReport(t, report, "queued", "queued", false, false)
}

func TestStatusServiceReportsTranscriptReadyMedia(t *testing.T) {
	repo := newStatusRepo()
	media := repo.addMedia(db.MediaItem{Provider: statusProvider, ProviderMediaID: statusMediaID, CanonicalURL: statusCanonicalURL, Status: "transcript_ready", TranscriptText: "transcript"})
	repo.addJob(db.TranscriptionJob{MediaItemID: media.ID, Status: "completed"})
	service := newStatusService(fakeStatusAuth{allowed: true}, fakeStatusRegistry{ref: statusRef()}, repo)

	report, err := service.Status(context.Background(), StatusQuery{ChatID: 10, UserID: 20, ChatType: auth.ChatTypePrivate, RawURL: statusRawURL})
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	assertStatusReport(t, report, "transcript_ready", "completed", true, false)
}

func TestStatusServiceReportsCompletedSummarizedMedia(t *testing.T) {
	repo := newStatusRepo()
	prompt := summarize.ResolvePrompt("")
	media := repo.addMedia(db.MediaItem{Provider: statusProvider, ProviderMediaID: statusMediaID, CanonicalURL: statusCanonicalURL, Status: "summarized", TranscriptText: "transcript"})
	repo.addJob(db.TranscriptionJob{MediaItemID: media.ID, Status: "completed"})
	repo.addCache(db.SummaryCache{MediaItemID: media.ID, PromptHash: summarize.PromptHash(prompt), Model: statusModel, SummaryText: "summary"})
	service := newStatusService(fakeStatusAuth{allowed: true}, fakeStatusRegistry{ref: statusRef()}, repo)

	report, err := service.Status(context.Background(), StatusQuery{ChatID: 10, UserID: 20, ChatType: auth.ChatTypePrivate, RawURL: statusRawURL})
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	assertStatusReport(t, report, "summarized", "completed", true, true)
}

func newStatusService(auth StatusAuthorizer, registry StatusProviderRegistry, repo StatusRepository) StatusService {
	return StatusService{Auth: auth, Registry: registry, Repo: repo, Model: statusModel}
}

func assertStatusReport(t *testing.T, report StatusReport, mediaStatus, jobStatus string, hasTranscript, hasSummaryCache bool) {
	t.Helper()
	if !report.Authorized || !report.Found {
		t.Fatalf("report = %#v", report)
	}
	if report.Media.Status != mediaStatus || report.Job.Status != jobStatus || report.HasTranscript != hasTranscript || report.HasSummaryCache != hasSummaryCache {
		t.Fatalf("report = %#v", report)
	}
	for _, want := range []string{"Provider: " + statusProvider, "Media ID: " + statusMediaID, "Media status: " + mediaStatus, "Job status: " + jobStatus} {
		if !strings.Contains(report.Text, want) {
			t.Fatalf("text = %q, want it to contain %q", report.Text, want)
		}
	}
}

type fakeStatusAuth struct {
	allowed bool
}

func (f fakeStatusAuth) CanUse(_ context.Context, _, _ int64, _ auth.ChatType) (bool, error) {
	return f.allowed, nil
}

type fakeStatusRegistry struct {
	ref provider.MediaRef
	err error
}

func (f fakeStatusRegistry) Find(_ string) (provider.Provider, provider.MediaRef, error) {
	return nil, f.ref, f.err
}

type statusRepo struct {
	nextMediaID int64
	nextJobID   int64
	media       map[string]db.MediaItem
	jobs        map[int64]db.TranscriptionJob
	caches      map[string]db.SummaryCache
}

func newStatusRepo() *statusRepo {
	return &statusRepo{nextMediaID: 1, nextJobID: 1, media: map[string]db.MediaItem{}, jobs: map[int64]db.TranscriptionJob{}, caches: map[string]db.SummaryCache{}}
}

func (r *statusRepo) addMedia(media db.MediaItem) db.MediaItem {
	media.ID = r.nextMediaID
	r.nextMediaID++
	r.media[statusMediaKey(media.Provider, media.ProviderMediaID)] = media
	return media
}

func (r *statusRepo) addJob(job db.TranscriptionJob) db.TranscriptionJob {
	job.ID = r.nextJobID
	r.nextJobID++
	r.jobs[job.MediaItemID] = job
	return job
}

func (r *statusRepo) addCache(cache db.SummaryCache) db.SummaryCache {
	r.caches[cacheKey(cache.MediaItemID, cache.PromptHash, cache.Model)] = cache
	return cache
}

func (r *statusRepo) FindMedia(_ context.Context, providerName, providerMediaID string) (db.MediaItem, bool, error) {
	media, ok := r.media[statusMediaKey(providerName, providerMediaID)]
	return media, ok, nil
}

func (r *statusRepo) FindLatestTranscriptionJob(_ context.Context, mediaItemID int64) (db.TranscriptionJob, bool, error) {
	job, ok := r.jobs[mediaItemID]
	return job, ok, nil
}

func (r *statusRepo) FindSummaryCache(_ context.Context, mediaItemID int64, promptHash, model string) (db.SummaryCache, bool, error) {
	cache, ok := r.caches[cacheKey(mediaItemID, promptHash, model)]
	return cache, ok, nil
}

func statusRef() provider.MediaRef {
	return provider.MediaRef{Provider: statusProvider, ProviderMediaID: statusMediaID, CanonicalURL: statusCanonicalURL}
}

func statusMediaKey(providerName, providerMediaID string) string {
	return providerName + ":" + providerMediaID
}
