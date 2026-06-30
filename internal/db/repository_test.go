package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestRepositoryWhitelists(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepository(t)

	if err := repo.UpsertWhitelistedGroup(ctx, WhitelistedGroup{ChatID: -100, Title: "group", CreatedByUserID: 1}); err != nil {
		t.Fatalf("UpsertWhitelistedGroup returned error: %v", err)
	}
	if err := repo.UpsertWhitelistedDMUser(ctx, WhitelistedDMUser{UserID: 42, Username: "alice", FirstName: "Alice", CreatedByUserID: 1}); err != nil {
		t.Fatalf("UpsertWhitelistedDMUser returned error: %v", err)
	}

	groupAllowed, err := repo.IsGroupWhitelisted(ctx, -100)
	if err != nil || !groupAllowed {
		t.Fatalf("IsGroupWhitelisted = %v, %v", groupAllowed, err)
	}
	userAllowed, err := repo.IsDMUserWhitelisted(ctx, 42)
	if err != nil || !userAllowed {
		t.Fatalf("IsDMUserWhitelisted = %v, %v", userAllowed, err)
	}

	groups, err := repo.ListWhitelistedGroups(ctx)
	if err != nil {
		t.Fatalf("ListWhitelistedGroups returned error: %v", err)
	}
	if len(groups) != 1 || groups[0].ChatID != -100 || groups[0].Title != "group" {
		t.Fatalf("groups = %#v", groups)
	}
	users, err := repo.ListWhitelistedDMUsers(ctx)
	if err != nil {
		t.Fatalf("ListWhitelistedDMUsers returned error: %v", err)
	}
	if len(users) != 1 || users[0].UserID != 42 || users[0].Username != "alice" {
		t.Fatalf("users = %#v", users)
	}

	if err := repo.RemoveWhitelistedGroup(ctx, -100); err != nil {
		t.Fatalf("RemoveWhitelistedGroup returned error: %v", err)
	}
	if err := repo.RemoveWhitelistedDMUser(ctx, 42); err != nil {
		t.Fatalf("RemoveWhitelistedDMUser returned error: %v", err)
	}
	groupAllowed, _ = repo.IsGroupWhitelisted(ctx, -100)
	userAllowed, _ = repo.IsDMUserWhitelisted(ctx, 42)
	if groupAllowed || userAllowed {
		t.Fatalf("removed whitelist entries still allowed: group=%v user=%v", groupAllowed, userAllowed)
	}
}

func TestRepositoryMediaInsertLoadAndUpdate(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepository(t)

	media, created, err := repo.CreateOrLoadMedia(ctx, "youtube", "abc12345678", "https://www.youtube.com/watch?v=abc12345678")
	if err != nil {
		t.Fatalf("CreateOrLoadMedia returned error: %v", err)
	}
	if !created || media.ID == 0 || media.Status != "new" {
		t.Fatalf("media = %#v created=%v", media, created)
	}

	loaded, created, err := repo.CreateOrLoadMedia(ctx, "youtube", "abc12345678", "https://www.youtube.com/watch?v=abc12345678")
	if err != nil {
		t.Fatalf("second CreateOrLoadMedia returned error: %v", err)
	}
	if created || loaded.ID != media.ID {
		t.Fatalf("loaded = %#v created=%v", loaded, created)
	}

	if err := repo.UpdateMediaStatus(ctx, media.ID, "checking_subtitles", "looking"); err != nil {
		t.Fatalf("UpdateMediaStatus returned error: %v", err)
	}
	if err := repo.UpdateMediaTranscript(ctx, media.ID, "manual_subtitle", "transcript"); err != nil {
		t.Fatalf("UpdateMediaTranscript returned error: %v", err)
	}
	updated, err := repo.GetMedia(ctx, media.ID)
	if err != nil {
		t.Fatalf("GetMedia returned error: %v", err)
	}
	if updated.Status != "transcript_ready" || updated.TranscriptSource != "manual_subtitle" || updated.TranscriptText != "transcript" {
		t.Fatalf("updated media = %#v", updated)
	}
}

func TestRepositoryTranscriptionJobs(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepository(t)
	first := createTestMedia(t, repo, "first000001")
	second := createTestMedia(t, repo, "second00002")

	firstJob, created, err := repo.CreateOrLoadActiveTranscriptionJob(ctx, first.ID)
	if err != nil {
		t.Fatalf("CreateOrLoadActiveTranscriptionJob returned error: %v", err)
	}
	if !created || firstJob.Status != "queued" {
		t.Fatalf("firstJob = %#v created=%v", firstJob, created)
	}
	loadedJob, created, err := repo.CreateOrLoadActiveTranscriptionJob(ctx, first.ID)
	if err != nil {
		t.Fatalf("second CreateOrLoadActiveTranscriptionJob returned error: %v", err)
	}
	if created || loadedJob.ID != firstJob.ID {
		t.Fatalf("loadedJob = %#v created=%v", loadedJob, created)
	}
	readyMedia := createTestMedia(t, repo, "readyjob001")
	if err := repo.UpdateMediaTranscript(ctx, readyMedia.ID, "subtitle", "transcript"); err != nil {
		t.Fatalf("UpdateMediaTranscript ready media returned error: %v", err)
	}
	readyJob, created, err := repo.CreateOrLoadActiveTranscriptionJob(ctx, readyMedia.ID)
	if err != nil {
		t.Fatalf("CreateOrLoadActiveTranscriptionJob ready media returned error: %v", err)
	}
	if created || readyJob.ID != 0 {
		t.Fatalf("readyJob = %#v created=%v", readyJob, created)
	}
	readyMedia, err = repo.GetMedia(ctx, readyMedia.ID)
	if err != nil {
		t.Fatalf("GetMedia ready returned error: %v", err)
	}
	if readyMedia.Status != "transcript_ready" {
		t.Fatalf("ready media = %#v", readyMedia)
	}

	secondJob, created, err := repo.CreateOrLoadActiveTranscriptionJob(ctx, second.ID)
	if err != nil {
		t.Fatalf("CreateOrLoadActiveTranscriptionJob for second returned error: %v", err)
	}
	if !created || secondJob.ID == firstJob.ID {
		t.Fatalf("secondJob = %#v created=%v", secondJob, created)
	}

	claimed, ok, err := repo.ClaimOldestQueuedTranscriptionJob(ctx)
	if err != nil {
		t.Fatalf("ClaimOldestQueuedTranscriptionJob returned error: %v", err)
	}
	if !ok || claimed.ID != firstJob.ID || claimed.Status != "downloading_audio" {
		t.Fatalf("claimed = %#v ok=%v", claimed, ok)
	}
	if err := repo.UpdateTranscriptionJobAndMediaStatus(ctx, claimed.ID, claimed.MediaItemID, "transcribing"); err != nil {
		t.Fatalf("UpdateTranscriptionJobAndMediaStatus returned error: %v", err)
	}
	updatedFirst, err := repo.GetMedia(ctx, first.ID)
	if err != nil {
		t.Fatalf("GetMedia returned error: %v", err)
	}
	if updatedFirst.Status != "transcribing" {
		t.Fatalf("updatedFirst = %#v", updatedFirst)
	}
	if err := repo.CompleteTranscriptionJobWithTranscript(ctx, claimed.ID, claimed.MediaItemID, "whisper", "transcript"); err != nil {
		t.Fatalf("CompleteTranscriptionJobWithTranscript returned error: %v", err)
	}
	updatedFirst, err = repo.GetMedia(ctx, first.ID)
	if err != nil {
		t.Fatalf("GetMedia after complete returned error: %v", err)
	}
	if updatedFirst.Status != "transcript_ready" || updatedFirst.TranscriptSource != "whisper" || updatedFirst.TranscriptText != "transcript" {
		t.Fatalf("completed media = %#v", updatedFirst)
	}
	failedRequest, err := repo.CreateSummaryRequest(ctx, SummaryRequest{MediaItemID: second.ID, ChatID: 10, PromptHash: "hash", PromptText: "prompt", Status: "pending_transcript"})
	if err != nil {
		t.Fatalf("CreateSummaryRequest failed media returned error: %v", err)
	}
	failed, failedRequests, err := repo.MarkTranscriptionJobFailedAndMediaFailed(ctx, secondJob.ID, secondJob.MediaItemID, "boom")
	if err != nil {
		t.Fatalf("MarkTranscriptionJobFailedAndMediaFailed returned error: %v", err)
	}
	if !failed || len(failedRequests) != 1 || failedRequests[0].ID != failedRequest.ID {
		t.Fatalf("failed=%v failedRequests=%#v", failed, failedRequests)
	}
	updatedSecond, err := repo.GetMedia(ctx, second.ID)
	if err != nil {
		t.Fatalf("GetMedia for failed media returned error: %v", err)
	}
	if updatedSecond.Status != "failed" || updatedSecond.StatusDetail != "boom" {
		t.Fatalf("failed media = %#v", updatedSecond)
	}
	failedRequest, err = repo.getSummaryRequest(ctx, failedRequest.ID)
	if err != nil {
		t.Fatalf("get failed request returned error: %v", err)
	}
	if failedRequest.Status != "failed" || failedRequest.Error != "boom" {
		t.Fatalf("failed request = %#v", failedRequest)
	}
}

func TestRepositoryRequeuesInterruptedTranscriptionJobs(t *testing.T) {
	ctx := context.Background()
	for _, status := range []string{"downloading_audio", "converting_audio", "splitting_audio", "transcribing"} {
		t.Run(status, func(t *testing.T) {
			repo := newTestRepository(t)
			activeMedia := createTestMedia(t, repo, "active"+status[:5])
			queuedMedia := createTestMedia(t, repo, "queued"+status[:5])
			orphanMedia := createTestMedia(t, repo, "orphan"+status[:5])

			activeJob, _, err := repo.CreateOrLoadActiveTranscriptionJob(ctx, activeMedia.ID)
			if err != nil {
				t.Fatalf("CreateOrLoadActiveTranscriptionJob active returned error: %v", err)
			}
			queuedJob, _, err := repo.CreateOrLoadActiveTranscriptionJob(ctx, queuedMedia.ID)
			if err != nil {
				t.Fatalf("CreateOrLoadActiveTranscriptionJob queued returned error: %v", err)
			}
			if err := repo.UpdateTranscriptionJobAndMediaStatus(ctx, activeJob.ID, activeMedia.ID, status); err != nil {
				t.Fatalf("UpdateTranscriptionJobAndMediaStatus returned error: %v", err)
			}
			if err := repo.UpdateMediaStatus(ctx, orphanMedia.ID, status, "orphan"); err != nil {
				t.Fatalf("UpdateMediaStatus orphan returned error: %v", err)
			}

			count, err := repo.RequeueInterruptedTranscriptionJobs(ctx)
			if err != nil {
				t.Fatalf("RequeueInterruptedTranscriptionJobs returned error: %v", err)
			}
			if count != 1 {
				t.Fatalf("requeued count = %d", count)
			}
			activeJob, found, err := repo.FindLatestTranscriptionJob(ctx, activeMedia.ID)
			if err != nil || !found {
				t.Fatalf("FindLatestTranscriptionJob active found=%v err=%v", found, err)
			}
			queuedJob, found, err = repo.FindLatestTranscriptionJob(ctx, queuedMedia.ID)
			if err != nil || !found {
				t.Fatalf("FindLatestTranscriptionJob queued found=%v err=%v", found, err)
			}
			activeMedia, err = repo.GetMedia(ctx, activeMedia.ID)
			if err != nil {
				t.Fatalf("GetMedia active returned error: %v", err)
			}
			queuedMedia, err = repo.GetMedia(ctx, queuedMedia.ID)
			if err != nil {
				t.Fatalf("GetMedia queued returned error: %v", err)
			}
			orphanMedia, err = repo.GetMedia(ctx, orphanMedia.ID)
			if err != nil {
				t.Fatalf("GetMedia orphan returned error: %v", err)
			}
			if activeJob.Status != "queued" || activeMedia.Status != "queued" {
				t.Fatalf("active job/media = %#v %#v", activeJob, activeMedia)
			}
			if queuedJob.Status != "queued" || queuedMedia.Status != "queued" {
				t.Fatalf("queued job/media = %#v %#v", queuedJob, queuedMedia)
			}
			if orphanMedia.Status != status {
				t.Fatalf("orphan media = %#v", orphanMedia)
			}
		})
	}
}

func TestRepositorySkipsTranscriptReadyTranscriptionJobs(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepository(t)

	queuedMedia := createTestMedia(t, repo, "readyqueue1")
	queuedJob, _, err := repo.CreateOrLoadActiveTranscriptionJob(ctx, queuedMedia.ID)
	if err != nil {
		t.Fatalf("CreateOrLoadActiveTranscriptionJob queued returned error: %v", err)
	}
	if err := repo.UpdateMediaTranscript(ctx, queuedMedia.ID, "subtitle", "existing transcript"); err != nil {
		t.Fatalf("UpdateMediaTranscript queued returned error: %v", err)
	}
	claimed, ok, err := repo.ClaimOldestQueuedTranscriptionJob(ctx)
	if err != nil {
		t.Fatalf("ClaimOldestQueuedTranscriptionJob returned error: %v", err)
	}
	if ok {
		t.Fatalf("claimed transcript-ready job = %#v", claimed)
	}
	queuedJob, _, err = repo.FindLatestTranscriptionJob(ctx, queuedMedia.ID)
	if err != nil {
		t.Fatalf("FindLatestTranscriptionJob queued returned error: %v", err)
	}
	if queuedJob.Status != "completed" {
		t.Fatalf("queued job = %#v", queuedJob)
	}

	interruptedMedia := createTestMedia(t, repo, "readyintr1")
	interruptedJob, _, err := repo.CreateOrLoadActiveTranscriptionJob(ctx, interruptedMedia.ID)
	if err != nil {
		t.Fatalf("CreateOrLoadActiveTranscriptionJob interrupted returned error: %v", err)
	}
	if err := repo.UpdateTranscriptionJobAndMediaStatus(ctx, interruptedJob.ID, interruptedMedia.ID, "downloading_audio"); err != nil {
		t.Fatalf("UpdateTranscriptionJobAndMediaStatus interrupted returned error: %v", err)
	}
	if err := repo.UpdateMediaTranscript(ctx, interruptedMedia.ID, "subtitle", "existing transcript"); err != nil {
		t.Fatalf("UpdateMediaTranscript interrupted returned error: %v", err)
	}
	count, err := repo.RequeueInterruptedTranscriptionJobs(ctx)
	if err != nil {
		t.Fatalf("RequeueInterruptedTranscriptionJobs returned error: %v", err)
	}
	if count != 0 {
		t.Fatalf("requeued count = %d", count)
	}
	interruptedJob, _, err = repo.FindLatestTranscriptionJob(ctx, interruptedMedia.ID)
	if err != nil {
		t.Fatalf("FindLatestTranscriptionJob interrupted returned error: %v", err)
	}
	if interruptedJob.Status != "completed" {
		t.Fatalf("interrupted job = %#v", interruptedJob)
	}

	completingMedia := createTestMedia(t, repo, "readycomp1")
	completingJob, _, err := repo.CreateOrLoadActiveTranscriptionJob(ctx, completingMedia.ID)
	if err != nil {
		t.Fatalf("CreateOrLoadActiveTranscriptionJob completing returned error: %v", err)
	}
	if err := repo.UpdateMediaTranscript(ctx, completingMedia.ID, "subtitle", "existing transcript"); err != nil {
		t.Fatalf("UpdateMediaTranscript completing returned error: %v", err)
	}
	if err := repo.UpdateTranscriptionJobAndMediaStatus(ctx, completingJob.ID, completingMedia.ID, "transcribing"); err != nil {
		t.Fatalf("UpdateTranscriptionJobAndMediaStatus completing returned error: %v", err)
	}
	if err := repo.CompleteTranscriptionJobWithTranscript(ctx, completingJob.ID, completingMedia.ID, "whisper", "new transcript"); err != nil {
		t.Fatalf("CompleteTranscriptionJobWithTranscript returned error: %v", err)
	}
	completingMedia, err = repo.GetMedia(ctx, completingMedia.ID)
	if err != nil {
		t.Fatalf("GetMedia completing returned error: %v", err)
	}
	if completingMedia.Status != "transcript_ready" || completingMedia.TranscriptSource != "subtitle" || completingMedia.TranscriptText != "existing transcript" {
		t.Fatalf("completing media = %#v", completingMedia)
	}

	retryMedia := createTestMedia(t, repo, "readyretry")
	retryJob, _, err := repo.CreateOrLoadActiveTranscriptionJob(ctx, retryMedia.ID)
	if err != nil {
		t.Fatalf("CreateOrLoadActiveTranscriptionJob retry returned error: %v", err)
	}
	if err := repo.UpdateMediaTranscript(ctx, retryMedia.ID, "subtitle", "existing transcript"); err != nil {
		t.Fatalf("UpdateMediaTranscript retry returned error: %v", err)
	}
	if err := repo.MarkTranscriptionJobRetryQueued(ctx, retryJob.ID, retryMedia.ID, "temporary failure"); err != nil {
		t.Fatalf("MarkTranscriptionJobRetryQueued returned error: %v", err)
	}
	retryJob, _, err = repo.FindLatestTranscriptionJob(ctx, retryMedia.ID)
	if err != nil {
		t.Fatalf("FindLatestTranscriptionJob retry returned error: %v", err)
	}
	retryMedia, err = repo.GetMedia(ctx, retryMedia.ID)
	if err != nil {
		t.Fatalf("GetMedia retry returned error: %v", err)
	}
	if retryJob.Status != "completed" || retryMedia.Status != "transcript_ready" {
		t.Fatalf("retry job/media = %#v %#v", retryJob, retryMedia)
	}

	failedMedia := createTestMedia(t, repo, "readyfail1")
	failedJob, _, err := repo.CreateOrLoadActiveTranscriptionJob(ctx, failedMedia.ID)
	if err != nil {
		t.Fatalf("CreateOrLoadActiveTranscriptionJob failed returned error: %v", err)
	}
	failedRequest, err := repo.CreateSummaryRequest(ctx, SummaryRequest{MediaItemID: failedMedia.ID, ChatID: 30, PromptHash: "hash", PromptText: "prompt", Status: "pending_transcript"})
	if err != nil {
		t.Fatalf("CreateSummaryRequest failed returned error: %v", err)
	}
	if err := repo.UpdateMediaTranscript(ctx, failedMedia.ID, "subtitle", "existing transcript"); err != nil {
		t.Fatalf("UpdateMediaTranscript failed returned error: %v", err)
	}
	if err := repo.UpdateMediaStatus(ctx, failedMedia.ID, "transcribing", "stale status"); err != nil {
		t.Fatalf("UpdateMediaStatus failed returned error: %v", err)
	}
	failed, failedRequests, err := repo.MarkTranscriptionJobFailedAndMediaFailed(ctx, failedJob.ID, failedMedia.ID, "boom")
	if err != nil {
		t.Fatalf("MarkTranscriptionJobFailedAndMediaFailed ready returned error: %v", err)
	}
	if failed || len(failedRequests) != 0 {
		t.Fatalf("failed=%v failedRequests=%#v", failed, failedRequests)
	}
	failedJob, _, err = repo.FindLatestTranscriptionJob(ctx, failedMedia.ID)
	if err != nil {
		t.Fatalf("FindLatestTranscriptionJob failed returned error: %v", err)
	}
	failedMedia, err = repo.GetMedia(ctx, failedMedia.ID)
	if err != nil {
		t.Fatalf("GetMedia failed returned error: %v", err)
	}
	failedRequest, err = repo.getSummaryRequest(ctx, failedRequest.ID)
	if err != nil {
		t.Fatalf("get failed request returned error: %v", err)
	}
	mediaIDs, err := repo.ListMediaIDsWithPendingSummaryRequests(ctx)
	if err != nil {
		t.Fatalf("ListMediaIDsWithPendingSummaryRequests returned error: %v", err)
	}
	if failedJob.Status != "completed" || failedMedia.Status != "transcript_ready" || failedRequest.Status != "pending_transcript" || len(mediaIDs) != 1 || mediaIDs[0] != failedMedia.ID {
		t.Fatalf("failed job/media/request/mediaIDs = %#v %#v %#v %#v", failedJob, failedMedia, failedRequest, mediaIDs)
	}
}

func TestRepositoryDuplicateRequestsSubscribeToOneActiveJob(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepository(t)
	media := createTestMedia(t, repo, "subscribe01")

	job, created, err := repo.CreateOrLoadActiveTranscriptionJob(ctx, media.ID)
	if err != nil {
		t.Fatalf("CreateOrLoadActiveTranscriptionJob returned error: %v", err)
	}
	if !created {
		t.Fatal("expected first active job call to create job")
	}
	loadedJob, created, err := repo.CreateOrLoadActiveTranscriptionJob(ctx, media.ID)
	if err != nil {
		t.Fatalf("second CreateOrLoadActiveTranscriptionJob returned error: %v", err)
	}
	if created || loadedJob.ID != job.ID {
		t.Fatalf("loadedJob = %#v created=%v", loadedJob, created)
	}

	firstRequest, err := repo.CreateSummaryRequest(ctx, SummaryRequest{MediaItemID: media.ID, ChatID: 10, UserID: 20, PromptHash: "hash", PromptText: "prompt", Status: "pending_transcript"})
	if err != nil {
		t.Fatalf("CreateSummaryRequest first returned error: %v", err)
	}
	secondRequest, err := repo.CreateSummaryRequest(ctx, SummaryRequest{MediaItemID: media.ID, ChatID: 11, UserID: 21, PromptHash: "hash2", PromptText: "prompt2", Status: "pending_transcript"})
	if err != nil {
		t.Fatalf("CreateSummaryRequest second returned error: %v", err)
	}
	pending, err := repo.ListPendingRequestsForMedia(ctx, media.ID)
	if err != nil {
		t.Fatalf("ListPendingRequestsForMedia returned error: %v", err)
	}
	if len(pending) != 2 || pending[0].ID != firstRequest.ID || pending[1].ID != secondRequest.ID {
		t.Fatalf("pending = %#v", pending)
	}
}

func TestRepositoryIsFirstRequestForMediaChatIgnoresWatchRequests(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepository(t)
	media := createTestMedia(t, repo, "firstwatch1")
	if _, err := repo.CreateSummaryRequest(ctx, SummaryRequest{MediaItemID: media.ID, ChatID: 10, UserID: 20, MessageID: 0, PromptHash: "hash", PromptText: "prompt", Status: "pending_summary"}); err != nil {
		t.Fatalf("CreateSummaryRequest watch returned error: %v", err)
	}
	manual, err := repo.CreateSummaryRequest(ctx, SummaryRequest{MediaItemID: media.ID, ChatID: 10, UserID: 21, MessageID: 30, PromptHash: "hash2", PromptText: "prompt2", Status: "pending_summary"})
	if err != nil {
		t.Fatalf("CreateSummaryRequest manual returned error: %v", err)
	}
	isFirst, err := repo.IsFirstRequestForMediaChat(ctx, manual.ID, media.ID, 10)
	if err != nil {
		t.Fatalf("IsFirstRequestForMediaChat returned error: %v", err)
	}
	if !isFirst {
		t.Fatal("manual request was not first after ignoring watch request")
	}
}

func TestRepositoryListProgressOwnerRequestsExcludesWatchRequests(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepository(t)
	media := createTestMedia(t, repo, "watchprog01")
	if _, err := repo.CreateSummaryRequest(ctx, SummaryRequest{MediaItemID: media.ID, ChatID: 10, UserID: 20, MessageID: 0, PromptHash: "hash", PromptText: "prompt", Status: "pending_summary"}); err != nil {
		t.Fatalf("CreateSummaryRequest watch returned error: %v", err)
	}
	requests, err := repo.ListProgressOwnerRequestsForMedia(ctx, media.ID)
	if err != nil {
		t.Fatalf("ListProgressOwnerRequestsForMedia returned error: %v", err)
	}
	if len(requests) != 0 {
		t.Fatalf("requests = %#v", requests)
	}
}

func TestRepositoryListProgressOwnerRequestsForMedia(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepository(t)
	media := createTestMedia(t, repo, "progress001")

	firstChatRequest, err := repo.CreateSummaryRequest(ctx, SummaryRequest{MediaItemID: media.ID, ChatID: 10, UserID: 20, MessageID: 30, PromptHash: "hash", PromptText: "prompt", Status: "pending_transcript"})
	if err != nil {
		t.Fatalf("CreateSummaryRequest first chat returned error: %v", err)
	}
	if _, err := repo.CreateSummaryRequest(ctx, SummaryRequest{MediaItemID: media.ID, ChatID: 10, UserID: 21, MessageID: 31, PromptHash: "hash2", PromptText: "prompt2", Status: "pending_transcript"}); err != nil {
		t.Fatalf("CreateSummaryRequest later same chat returned error: %v", err)
	}
	secondChatRequest, err := repo.CreateSummaryRequest(ctx, SummaryRequest{MediaItemID: media.ID, ChatID: 11, UserID: 22, MessageID: 32, PromptHash: "hash3", PromptText: "prompt3", Status: "sending"})
	if err != nil {
		t.Fatalf("CreateSummaryRequest second chat returned error: %v", err)
	}
	if _, err := repo.CreateSummaryRequest(ctx, SummaryRequest{MediaItemID: media.ID, ChatID: 12, UserID: 23, MessageID: 33, PromptHash: "hash4", PromptText: "prompt4", Status: "failed"}); err != nil {
		t.Fatalf("CreateSummaryRequest failed returned error: %v", err)
	}
	if _, err := repo.CreateSummaryRequest(ctx, SummaryRequest{MediaItemID: media.ID, ChatID: 13, UserID: 24, MessageID: 0, PromptHash: "hash5", PromptText: "prompt5", Status: "pending_summary"}); err != nil {
		t.Fatalf("CreateSummaryRequest watch returned error: %v", err)
	}

	requests, err := repo.ListProgressOwnerRequestsForMedia(ctx, media.ID)
	if err != nil {
		t.Fatalf("ListProgressOwnerRequestsForMedia returned error: %v", err)
	}
	if len(requests) != 2 || requests[0].ID != firstChatRequest.ID || requests[1].ID != secondChatRequest.ID {
		t.Fatalf("requests = %#v", requests)
	}
}

func TestRepositoryRecoversInterruptedSummaryRequests(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepository(t)
	readyMedia := createTestMedia(t, repo, "ready000001")
	queuedMedia := createTestMedia(t, repo, "queued00002")
	if err := repo.UpdateMediaTranscript(ctx, readyMedia.ID, "whisper", "transcript"); err != nil {
		t.Fatalf("UpdateMediaTranscript returned error: %v", err)
	}
	summarizing, err := repo.CreateSummaryRequest(ctx, SummaryRequest{MediaItemID: readyMedia.ID, ChatID: 10, PromptHash: "hash", PromptText: "prompt", Status: "summarizing"})
	if err != nil {
		t.Fatalf("CreateSummaryRequest summarizing returned error: %v", err)
	}
	pending, err := repo.CreateSummaryRequest(ctx, SummaryRequest{MediaItemID: readyMedia.ID, ChatID: 11, PromptHash: "hash2", PromptText: "prompt2", Status: "pending_summary"})
	if err != nil {
		t.Fatalf("CreateSummaryRequest pending returned error: %v", err)
	}
	pendingTranscript, err := repo.CreateSummaryRequest(ctx, SummaryRequest{MediaItemID: readyMedia.ID, ChatID: 12, PromptHash: "hash3", PromptText: "prompt3", Status: "pending_transcript"})
	if err != nil {
		t.Fatalf("CreateSummaryRequest pending transcript returned error: %v", err)
	}
	sending, err := repo.CreateSummaryRequest(ctx, SummaryRequest{MediaItemID: readyMedia.ID, ChatID: 14, PromptHash: "hash5", PromptText: "prompt5", Status: "sending"})
	if err != nil {
		t.Fatalf("CreateSummaryRequest sending returned error: %v", err)
	}
	queuedPending, err := repo.CreateSummaryRequest(ctx, SummaryRequest{MediaItemID: queuedMedia.ID, ChatID: 13, PromptHash: "hash4", PromptText: "prompt4", Status: "pending_summary"})
	if err != nil {
		t.Fatalf("CreateSummaryRequest queued pending returned error: %v", err)
	}

	count, err := repo.RequeueInterruptedSummaryRequests(ctx)
	if err != nil {
		t.Fatalf("RequeueInterruptedSummaryRequests returned error: %v", err)
	}
	if count != 2 {
		t.Fatalf("requeued count = %d", count)
	}
	mediaIDs, err := repo.ListMediaIDsWithPendingSummaryRequests(ctx)
	if err != nil {
		t.Fatalf("ListMediaIDsWithPendingSummaryRequests returned error: %v", err)
	}
	if len(mediaIDs) != 1 || mediaIDs[0] != readyMedia.ID {
		t.Fatalf("mediaIDs = %#v", mediaIDs)
	}
	requests, err := repo.ListPendingRequestsForMedia(ctx, readyMedia.ID)
	if err != nil {
		t.Fatalf("ListPendingRequestsForMedia returned error: %v", err)
	}
	if len(requests) != 3 || requests[0].ID != summarizing.ID || requests[1].ID != pending.ID || requests[2].ID != pendingTranscript.ID {
		t.Fatalf("ready pending requests = %#v", requests)
	}
	sending, err = repo.getSummaryRequest(ctx, sending.ID)
	if err != nil {
		t.Fatalf("get sending request returned error: %v", err)
	}
	if sending.Status != "delivery_unknown" || sending.Error != "delivery status unknown after interruption" {
		t.Fatalf("sending request = %#v", sending)
	}
	queuedRequests, err := repo.ListPendingRequestsForMedia(ctx, queuedMedia.ID)
	if err != nil {
		t.Fatalf("ListPendingRequestsForMedia queued returned error: %v", err)
	}
	if len(queuedRequests) != 1 || queuedRequests[0].ID != queuedPending.ID {
		t.Fatalf("queued pending requests = %#v", queuedRequests)
	}
}

func TestRepositoryEnqueuesPendingTranscriptionRequests(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepository(t)
	media := createTestMedia(t, repo, "recover0001")
	readyMedia := createTestMedia(t, repo, "readyrecover")
	activeMedia := createTestMedia(t, repo, "activerecov")
	if err := repo.UpdateMediaTranscript(ctx, readyMedia.ID, "whisper", "transcript"); err != nil {
		t.Fatalf("UpdateMediaTranscript returned error: %v", err)
	}
	if _, _, err := repo.CreateOrLoadActiveTranscriptionJob(ctx, activeMedia.ID); err != nil {
		t.Fatalf("CreateOrLoadActiveTranscriptionJob returned error: %v", err)
	}
	for _, mediaID := range []int64{media.ID, readyMedia.ID, activeMedia.ID} {
		if _, err := repo.CreateSummaryRequest(ctx, SummaryRequest{MediaItemID: mediaID, ChatID: mediaID, PromptHash: "hash", PromptText: "prompt", Status: "pending_transcript"}); err != nil {
			t.Fatalf("CreateSummaryRequest returned error: %v", err)
		}
	}

	count, err := repo.EnqueuePendingTranscriptionRequests(ctx)
	if err != nil {
		t.Fatalf("EnqueuePendingTranscriptionRequests returned error: %v", err)
	}
	if count != 1 {
		t.Fatalf("enqueued count = %d", count)
	}
	job, found, err := repo.FindLatestTranscriptionJob(ctx, media.ID)
	if err != nil {
		t.Fatalf("FindLatestTranscriptionJob returned error: %v", err)
	}
	if !found || job.Status != "queued" {
		t.Fatalf("job = %#v found=%v", job, found)
	}
	updatedMedia, err := repo.GetMedia(ctx, media.ID)
	if err != nil {
		t.Fatalf("GetMedia returned error: %v", err)
	}
	if updatedMedia.Status != "queued" {
		t.Fatalf("media = %#v", updatedMedia)
	}
	if _, found, err := repo.FindLatestTranscriptionJob(ctx, readyMedia.ID); err != nil || found {
		t.Fatalf("ready media job found=%v err=%v", found, err)
	}
}

func TestRepositoryRequeuesOnlyStaleInFlightSummaryRequests(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepository(t)
	media := createTestMedia(t, repo, "stale000001")
	oldSummarizing, err := repo.CreateSummaryRequest(ctx, SummaryRequest{MediaItemID: media.ID, ChatID: 10, PromptHash: "hash", PromptText: "prompt", Status: "summarizing"})
	if err != nil {
		t.Fatalf("CreateSummaryRequest old summarizing returned error: %v", err)
	}
	freshSummarizing, err := repo.CreateSummaryRequest(ctx, SummaryRequest{MediaItemID: media.ID, ChatID: 11, PromptHash: "hash2", PromptText: "prompt2", Status: "summarizing"})
	if err != nil {
		t.Fatalf("CreateSummaryRequest fresh summarizing returned error: %v", err)
	}
	oldSending, err := repo.CreateSummaryRequest(ctx, SummaryRequest{MediaItemID: media.ID, ChatID: 12, PromptHash: "hash3", PromptText: "prompt3", Status: "sending"})
	if err != nil {
		t.Fatalf("CreateSummaryRequest old sending returned error: %v", err)
	}
	if _, err := repo.db.ExecContext(ctx, `UPDATE summary_requests SET updated_at = '2000-01-01 00:00:00' WHERE id IN (?, ?)`, oldSummarizing.ID, oldSending.ID); err != nil {
		t.Fatalf("backdate summary requests returned error: %v", err)
	}

	count, err := repo.RequeueStaleSummaryRequests(ctx, mustParseTime(t, "2020-01-01 00:00:00"))
	if err != nil {
		t.Fatalf("RequeueStaleSummaryRequests returned error: %v", err)
	}
	if count != 2 {
		t.Fatalf("requeued count = %d", count)
	}
	oldSummarizing, err = repo.getSummaryRequest(ctx, oldSummarizing.ID)
	if err != nil {
		t.Fatalf("get old summarizing returned error: %v", err)
	}
	freshSummarizing, err = repo.getSummaryRequest(ctx, freshSummarizing.ID)
	if err != nil {
		t.Fatalf("get fresh summarizing returned error: %v", err)
	}
	oldSending, err = repo.getSummaryRequest(ctx, oldSending.ID)
	if err != nil {
		t.Fatalf("get old sending returned error: %v", err)
	}
	if oldSummarizing.Status != "pending_summary" || freshSummarizing.Status != "summarizing" || oldSending.Status != "delivery_unknown" {
		t.Fatalf("requests = %#v %#v %#v", oldSummarizing, freshSummarizing, oldSending)
	}
}

func TestRepositoryWatchSummaryRequestsAreIdempotent(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepository(t)
	media := createTestMedia(t, repo, "watchreq001")
	first, err := repo.CreateSummaryRequest(ctx, SummaryRequest{MediaItemID: media.ID, ChatID: 10, UserID: 20, MessageID: 0, PromptHash: "hash", PromptText: "prompt", Status: "pending_transcript"})
	if err != nil {
		t.Fatalf("CreateSummaryRequest first returned error: %v", err)
	}
	second, err := repo.CreateSummaryRequest(ctx, SummaryRequest{MediaItemID: media.ID, ChatID: 10, UserID: 21, MessageID: 0, PromptHash: "hash", PromptText: "changed", Status: "pending_summary"})
	if err != nil {
		t.Fatalf("CreateSummaryRequest duplicate returned error: %v", err)
	}
	if second.ID != first.ID || second.UserID != first.UserID || second.PromptText != first.PromptText || second.Status != first.Status {
		t.Fatalf("second = %#v first = %#v", second, first)
	}
	third, err := repo.CreateSummaryRequest(ctx, SummaryRequest{MediaItemID: media.ID, ChatID: 11, UserID: 22, MessageID: 0, PromptHash: "hash", PromptText: "prompt", Status: "pending_transcript"})
	if err != nil {
		t.Fatalf("CreateSummaryRequest other chat returned error: %v", err)
	}
	if third.ID == first.ID {
		t.Fatalf("third = %#v first = %#v", third, first)
	}
}

func TestRepositorySummaryRequestsAndCache(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepository(t)
	media := createTestMedia(t, repo, "summary0001")

	request, err := repo.CreateSummaryRequest(ctx, SummaryRequest{MediaItemID: media.ID, ChatID: 10, UserID: 20, MessageID: 30, PromptHash: "hash", PromptText: "prompt", Status: "pending_transcript"})
	if err != nil {
		t.Fatalf("CreateSummaryRequest returned error: %v", err)
	}
	if request.ID == 0 || request.Status != "pending_transcript" || request.MessageID != 30 {
		t.Fatalf("request = %#v", request)
	}
	loadedRequest, err := repo.CreateSummaryRequest(ctx, SummaryRequest{MediaItemID: media.ID, ChatID: 10, UserID: 20, MessageID: 30, PromptHash: "different", PromptText: "different", Status: "pending_summary"})
	if err != nil {
		t.Fatalf("duplicate CreateSummaryRequest returned error: %v", err)
	}
	if loadedRequest.ID != request.ID || loadedRequest.PromptHash != request.PromptHash || loadedRequest.Status != request.Status {
		t.Fatalf("loaded duplicate request = %#v", loadedRequest)
	}

	pending, err := repo.ListPendingRequestsForMedia(ctx, media.ID)
	if err != nil {
		t.Fatalf("ListPendingRequestsForMedia returned error: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != request.ID {
		t.Fatalf("pending = %#v", pending)
	}

	if _, found, err := repo.FindSummaryCache(ctx, media.ID, "hash", "model"); err != nil || found {
		t.Fatalf("FindSummaryCache before insert found=%v err=%v", found, err)
	}
	cache, err := repo.InsertSummaryCache(ctx, SummaryCache{MediaItemID: media.ID, PromptHash: "hash", PromptText: "prompt", SummaryText: "summary", Model: "model"})
	if err != nil {
		t.Fatalf("InsertSummaryCache returned error: %v", err)
	}
	foundCache, found, err := repo.FindSummaryCache(ctx, media.ID, "hash", "model")
	if err != nil || !found {
		t.Fatalf("FindSummaryCache after insert found=%v err=%v", found, err)
	}
	if foundCache.ID != cache.ID || foundCache.SummaryText != "summary" {
		t.Fatalf("foundCache = %#v", foundCache)
	}
	if _, err := repo.InsertSummaryCache(ctx, SummaryCache{MediaItemID: media.ID, PromptHash: "hash", PromptText: "prompt", SummaryText: "summary", Model: "model"}); err == nil {
		t.Fatal("expected duplicate summary cache insert to fail")
	}

	if err := repo.MarkSummaryRequestPendingSummary(ctx, request.ID); err != nil {
		t.Fatalf("MarkSummaryRequestPendingSummary returned error: %v", err)
	}
	if err := repo.MarkSummaryRequestSummarizing(ctx, request.ID); err != nil {
		t.Fatalf("MarkSummaryRequestSummarizing returned error: %v", err)
	}
	if err := repo.MarkSummaryRequestSending(ctx, request.ID); err != nil {
		t.Fatalf("MarkSummaryRequestSending returned error: %v", err)
	}
	unknown, err := repo.CreateSummaryRequest(ctx, SummaryRequest{MediaItemID: media.ID, ChatID: 12, UserID: 22, PromptHash: "hash3", PromptText: "prompt3", Status: "pending_summary"})
	if err != nil {
		t.Fatalf("CreateSummaryRequest delivery unknown returned error: %v", err)
	}
	if err := repo.MarkSummaryRequestSummarizing(ctx, unknown.ID); err != nil {
		t.Fatalf("MarkSummaryRequestSummarizing delivery unknown returned error: %v", err)
	}
	if err := repo.MarkSummaryRequestSending(ctx, unknown.ID); err != nil {
		t.Fatalf("MarkSummaryRequestSending delivery unknown returned error: %v", err)
	}
	if err := repo.MarkSummaryRequestDeliveryUnknown(ctx, unknown.ID, "ambiguous send"); err != nil {
		t.Fatalf("MarkSummaryRequestDeliveryUnknown returned error: %v", err)
	}
	unknown, err = repo.getSummaryRequest(ctx, unknown.ID)
	if err != nil {
		t.Fatalf("get delivery unknown request returned error: %v", err)
	}
	if unknown.Status != "delivery_unknown" || unknown.Error != "ambiguous send" {
		t.Fatalf("delivery unknown request = %#v", unknown)
	}
	if err := repo.MarkSummaryRequestSent(ctx, request.ID, cache.ID); err != nil {
		t.Fatalf("MarkSummaryRequestSent returned error: %v", err)
	}
	pending, err = repo.ListPendingRequestsForMedia(ctx, media.ID)
	if err != nil {
		t.Fatalf("ListPendingRequestsForMedia after sent returned error: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending after sent = %#v", pending)
	}

	failed, err := repo.CreateSummaryRequest(ctx, SummaryRequest{MediaItemID: media.ID, ChatID: 11, UserID: 21, PromptHash: "hash2", PromptText: "prompt2", Status: "pending_summary"})
	if err != nil {
		t.Fatalf("CreateSummaryRequest failed request returned error: %v", err)
	}
	if err := repo.MarkSummaryRequestSummarizing(ctx, failed.ID); err != nil {
		t.Fatalf("MarkSummaryRequestSummarizing failed request returned error: %v", err)
	}
	if err := repo.MarkSummaryRequestFailed(ctx, failed.ID, "nope"); err != nil {
		t.Fatalf("MarkSummaryRequestFailed returned error: %v", err)
	}
	pending, err = repo.ListPendingRequestsForMedia(ctx, media.ID)
	if err != nil {
		t.Fatalf("ListPendingRequestsForMedia after failed returned error: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending after failed = %#v", pending)
	}
}

func TestRepositoryWatchPersistence(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepository(t)

	feed, created, err := repo.CreateOrLoadWatchFeed(ctx, WatchFeed{Provider: "soundon", ProviderFeedID: "podcast-1", CanonicalURL: "https://player.soundon.fm/p/podcast-1", Title: "First title"})
	if err != nil {
		t.Fatalf("CreateOrLoadWatchFeed returned error: %v", err)
	}
	if !created || feed.ID == 0 || feed.Status != "active" || feed.Title != "First title" {
		t.Fatalf("feed = %#v created=%v", feed, created)
	}
	loadedFeed, created, err := repo.CreateOrLoadWatchFeed(ctx, WatchFeed{Provider: "soundon", ProviderFeedID: "podcast-1", CanonicalURL: "https://changed.example", Title: "Changed title"})
	if err != nil {
		t.Fatalf("duplicate CreateOrLoadWatchFeed returned error: %v", err)
	}
	if created || loadedFeed.ID != feed.ID || loadedFeed.CanonicalURL != feed.CanonicalURL || loadedFeed.Title != feed.Title {
		t.Fatalf("loadedFeed = %#v created=%v", loadedFeed, created)
	}

	sub, err := repo.UpsertWatchSubscription(ctx, WatchSubscription{FeedID: feed.ID, ChatID: 10, ChatType: "group", ChatTitle: "Old title", CreatedByUserID: 1})
	if err != nil {
		t.Fatalf("UpsertWatchSubscription returned error: %v", err)
	}
	if sub.ID == 0 || sub.ChatTitle != "Old title" {
		t.Fatalf("sub = %#v", sub)
	}
	updatedSub, err := repo.UpsertWatchSubscription(ctx, WatchSubscription{FeedID: feed.ID, ChatID: 10, ChatType: "supergroup", ChatTitle: "New title", CreatedByUserID: 2})
	if err != nil {
		t.Fatalf("second UpsertWatchSubscription returned error: %v", err)
	}
	if updatedSub.ID != sub.ID || updatedSub.ChatType != "supergroup" || updatedSub.ChatTitle != "New title" || updatedSub.CreatedByUserID != 2 {
		t.Fatalf("updatedSub = %#v", updatedSub)
	}

	otherFeed, _, err := repo.CreateOrLoadWatchFeed(ctx, WatchFeed{Provider: "xiaoyuzhou", ProviderFeedID: "podcast-2", CanonicalURL: "https://www.xiaoyuzhoufm.com/podcast/podcast-2"})
	if err != nil {
		t.Fatalf("CreateOrLoadWatchFeed other returned error: %v", err)
	}
	if _, err := repo.UpsertWatchSubscription(ctx, WatchSubscription{FeedID: otherFeed.ID, ChatID: 11, ChatType: "private", CreatedByUserID: 1}); err != nil {
		t.Fatalf("UpsertWatchSubscription other returned error: %v", err)
	}

	chatSubs, err := repo.ListWatchSubscriptionsForChat(ctx, 10)
	if err != nil {
		t.Fatalf("ListWatchSubscriptionsForChat returned error: %v", err)
	}
	if len(chatSubs) != 1 || chatSubs[0].ID != sub.ID || chatSubs[0].Feed.ID != feed.ID || chatSubs[0].Feed.Provider != "soundon" {
		t.Fatalf("chatSubs = %#v", chatSubs)
	}
	activeFeeds, err := repo.ListActiveWatchFeeds(ctx)
	if err != nil {
		t.Fatalf("ListActiveWatchFeeds returned error: %v", err)
	}
	if len(activeFeeds) != 2 {
		t.Fatalf("activeFeeds = %#v", activeFeeds)
	}
	feedSubs, err := repo.ListWatchSubscriptionsForFeed(ctx, feed.ID)
	if err != nil {
		t.Fatalf("ListWatchSubscriptionsForFeed returned error: %v", err)
	}
	if len(feedSubs) != 1 || feedSubs[0].ID != sub.ID {
		t.Fatalf("feedSubs = %#v", feedSubs)
	}

	removed, err := repo.RemoveWatchSubscription(ctx, "soundon", "podcast-1", 11)
	if err != nil {
		t.Fatalf("RemoveWatchSubscription wrong chat returned error: %v", err)
	}
	if removed {
		t.Fatal("removed wrong chat subscription")
	}
	removed, err = repo.RemoveWatchSubscription(ctx, "soundon", "podcast-1", 10)
	if err != nil {
		t.Fatalf("RemoveWatchSubscription returned error: %v", err)
	}
	if !removed {
		t.Fatal("did not remove matching subscription")
	}
	activeFeeds, err = repo.ListActiveWatchFeeds(ctx)
	if err != nil {
		t.Fatalf("ListActiveWatchFeeds after removal returned error: %v", err)
	}
	if len(activeFeeds) != 1 || activeFeeds[0].ID != otherFeed.ID {
		t.Fatalf("activeFeeds after removal = %#v", activeFeeds)
	}
	if _, err := repo.db.ExecContext(ctx, `UPDATE watch_feeds SET status = 'inactive' WHERE id = ?`, otherFeed.ID); err != nil {
		t.Fatalf("deactivate watch feed returned error: %v", err)
	}
	activeFeeds, err = repo.ListActiveWatchFeeds(ctx)
	if err != nil {
		t.Fatalf("ListActiveWatchFeeds inactive returned error: %v", err)
	}
	if len(activeFeeds) != 0 {
		t.Fatalf("activeFeeds inactive = %#v", activeFeeds)
	}
	chatSubs, err = repo.ListWatchSubscriptionsForChat(ctx, 11)
	if err != nil {
		t.Fatalf("ListWatchSubscriptionsForChat inactive returned error: %v", err)
	}
	if len(chatSubs) != 0 {
		t.Fatalf("inactive chatSubs = %#v", chatSubs)
	}
}

func TestRepositoryWatchEpisodes(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepository(t)
	feed, _, err := repo.CreateOrLoadWatchFeed(ctx, WatchFeed{Provider: "soundon", ProviderFeedID: "podcast-1", CanonicalURL: "https://player.soundon.fm/p/podcast-1"})
	if err != nil {
		t.Fatalf("CreateOrLoadWatchFeed returned error: %v", err)
	}

	episodes := []WatchEpisode{
		{ProviderEpisodeID: "ep-1", CanonicalURL: "https://example.com/ep-1", Title: "Episode 1", PubDate: "2026-05-01 00:00:00"},
		{ProviderEpisodeID: "ep-2", CanonicalURL: "https://example.com/ep-2", Title: "Episode 2", PubDate: "2026-05-02 00:00:00"},
	}
	inserted, err := repo.InsertBaselineWatchEpisodes(ctx, feed.ID, episodes)
	if err != nil {
		t.Fatalf("InsertBaselineWatchEpisodes returned error: %v", err)
	}
	if inserted != 2 {
		t.Fatalf("inserted baseline count = %d", inserted)
	}
	inserted, err = repo.InsertBaselineWatchEpisodes(ctx, feed.ID, episodes)
	if err != nil {
		t.Fatalf("second InsertBaselineWatchEpisodes returned error: %v", err)
	}
	if inserted != 0 {
		t.Fatalf("duplicate inserted baseline count = %d", inserted)
	}
	for table, want := range map[string]int64{"media_items": 0, "transcription_jobs": 0, "summary_requests": 0} {
		if got := countRows(t, repo, table); got != want {
			t.Fatalf("%s rows after baseline = %d, want %d", table, got, want)
		}
	}
	baseline, found, err := repo.FindWatchEpisode(ctx, feed.ID, "ep-1")
	if err != nil || !found {
		t.Fatalf("FindWatchEpisode baseline found=%v err=%v", found, err)
	}
	if baseline.Status != "baseline" || baseline.MediaItemID != 0 || baseline.ProcessedAt == "" {
		t.Fatalf("baseline = %#v", baseline)
	}

	media := createTestMedia(t, repo, "watchep0001")
	if _, _, err := repo.InsertWatchEpisodeQueued(ctx, WatchEpisode{FeedID: feed.ID, ProviderEpisodeID: "ep-zero", CanonicalURL: "https://example.com/ep-zero"}, 0); err == nil {
		t.Fatal("expected zero-media queued watch episode to fail")
	}
	loadedBaseline, created, err := repo.InsertWatchEpisodeQueued(ctx, WatchEpisode{FeedID: feed.ID, ProviderEpisodeID: "ep-1", CanonicalURL: "https://example.com/changed"}, media.ID)
	if err != nil {
		t.Fatalf("InsertWatchEpisodeQueued duplicate baseline returned error: %v", err)
	}
	if created || loadedBaseline.Status != "baseline" || loadedBaseline.MediaItemID != 0 || loadedBaseline.CanonicalURL != baseline.CanonicalURL {
		t.Fatalf("loadedBaseline = %#v created=%v", loadedBaseline, created)
	}

	queued, created, err := repo.InsertWatchEpisodeQueued(ctx, WatchEpisode{FeedID: feed.ID, ProviderEpisodeID: "ep-3", CanonicalURL: "https://example.com/ep-3", Title: "Episode 3"}, media.ID)
	if err != nil {
		t.Fatalf("InsertWatchEpisodeQueued returned error: %v", err)
	}
	if !created || queued.Status != "queued" || queued.MediaItemID != media.ID {
		t.Fatalf("queued = %#v created=%v", queued, created)
	}
	loadedQueued, created, err := repo.InsertWatchEpisodeQueued(ctx, WatchEpisode{FeedID: feed.ID, ProviderEpisodeID: "ep-3", CanonicalURL: "https://example.com/changed"}, 999)
	if err != nil {
		t.Fatalf("second InsertWatchEpisodeQueued returned error: %v", err)
	}
	if created || loadedQueued.ID != queued.ID || loadedQueued.MediaItemID != media.ID || loadedQueued.CanonicalURL != queued.CanonicalURL {
		t.Fatalf("loadedQueued = %#v created=%v", loadedQueued, created)
	}

	loadedQueuedSkipped, created, err := repo.InsertWatchEpisodeSkipped(ctx, WatchEpisode{FeedID: feed.ID, ProviderEpisodeID: "ep-3", CanonicalURL: "https://example.com/skipped"})
	if err != nil {
		t.Fatalf("InsertWatchEpisodeSkipped duplicate queued returned error: %v", err)
	}
	if created || loadedQueuedSkipped.Status != "queued" || loadedQueuedSkipped.MediaItemID != media.ID || loadedQueuedSkipped.CanonicalURL != queued.CanonicalURL {
		t.Fatalf("loadedQueuedSkipped = %#v created=%v", loadedQueuedSkipped, created)
	}

	skipped, created, err := repo.InsertWatchEpisodeSkipped(ctx, WatchEpisode{FeedID: feed.ID, ProviderEpisodeID: "ep-4", CanonicalURL: "https://example.com/ep-4", Title: "Episode 4"})
	if err != nil {
		t.Fatalf("InsertWatchEpisodeSkipped returned error: %v", err)
	}
	if !created || skipped.Status != "skipped" || skipped.MediaItemID != 0 {
		t.Fatalf("skipped = %#v created=%v", skipped, created)
	}
	loadedSkipped, created, err := repo.InsertWatchEpisodeSkipped(ctx, WatchEpisode{FeedID: feed.ID, ProviderEpisodeID: "ep-4", CanonicalURL: "https://example.com/changed"})
	if err != nil {
		t.Fatalf("second InsertWatchEpisodeSkipped returned error: %v", err)
	}
	if created || loadedSkipped.ID != skipped.ID || loadedSkipped.CanonicalURL != skipped.CanonicalURL {
		t.Fatalf("loadedSkipped = %#v created=%v", loadedSkipped, created)
	}

	if err := repo.MarkWatchEpisodeFailed(ctx, queued.ID, "boom"); err != nil {
		t.Fatalf("MarkWatchEpisodeFailed returned error: %v", err)
	}
	failed, found, err := repo.FindWatchEpisode(ctx, feed.ID, "ep-3")
	if err != nil || !found {
		t.Fatalf("FindWatchEpisode failed found=%v err=%v", found, err)
	}
	if failed.Status != "failed" || failed.LastError != "boom" {
		t.Fatalf("failed = %#v", failed)
	}
	if err := repo.MarkWatchFeedError(ctx, feed.ID, "fetch failed"); err != nil {
		t.Fatalf("MarkWatchFeedError returned error: %v", err)
	}
	lastChecked, lastError := watchFeedState(t, repo, feed.ID)
	if lastError != "fetch failed" || lastChecked != "" {
		t.Fatalf("feed state after error lastChecked=%q lastError=%q", lastChecked, lastError)
	}
	if err := repo.MarkWatchFeedChecked(ctx, feed.ID); err != nil {
		t.Fatalf("MarkWatchFeedChecked returned error: %v", err)
	}
	lastChecked, lastError = watchFeedState(t, repo, feed.ID)
	if lastError != "" || lastChecked == "" {
		t.Fatalf("feed state after checked lastChecked=%q lastError=%q", lastChecked, lastError)
	}
}

func countRows(t *testing.T, repo *Repository, table string) int64 {
	t.Helper()
	var count int64
	if err := repo.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
		t.Fatalf("count %s returned error: %v", table, err)
	}
	return count
}

func watchFeedState(t *testing.T, repo *Repository, feedID int64) (string, string) {
	t.Helper()
	var lastChecked string
	var lastError string
	if err := repo.db.QueryRowContext(context.Background(), `SELECT COALESCE(last_checked_at, ''), COALESCE(last_error, '') FROM watch_feeds WHERE id = ?`, feedID).Scan(&lastChecked, &lastError); err != nil {
		t.Fatalf("query watch feed state returned error: %v", err)
	}
	return lastChecked, lastError
}

func newTestRepository(t *testing.T) *Repository {
	t.Helper()
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := RunMigrations(ctx, database); err != nil {
		t.Fatalf("RunMigrations returned error: %v", err)
	}
	return NewRepository(database)
}

func createTestMedia(t *testing.T, repo *Repository, mediaID string) MediaItem {
	t.Helper()
	media, _, err := repo.CreateOrLoadMedia(context.Background(), "youtube", mediaID, "https://www.youtube.com/watch?v="+mediaID)
	if err != nil {
		t.Fatalf("CreateOrLoadMedia returned error: %v", err)
	}
	return media
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.DateTime, value)
	if err != nil {
		t.Fatalf("time.Parse returned error: %v", err)
	}
	return parsed
}
