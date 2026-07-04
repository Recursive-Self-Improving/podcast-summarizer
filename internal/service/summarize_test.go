package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/db"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/display"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/provider"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/summarize"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/transcript"
)

func TestSummaryServiceReusesDefaultPromptCache(t *testing.T) {
	ctx := context.Background()
	repo := newSummaryRepo()
	media := db.MediaItem{ID: 1, TranscriptText: "transcript"}
	prompt := summarize.ResolvePrompt("")
	promptHash := summarize.PromptHash(prompt)
	cached := repo.addCache(db.SummaryCache{MediaItemID: media.ID, PromptHash: promptHash, PromptText: prompt, SummaryText: "cached summary", Model: "model"})
	request := repo.addRequest(db.SummaryRequest{MediaItemID: media.ID, ChatID: 10, PromptHash: promptHash, PromptText: prompt, Status: "pending_summary"})
	summarizer := &fakeSummaryLLM{}
	sender := &fakeSummarySender{}

	err := SummaryService{Repo: repo, Summarizer: summarizer, Sender: sender, Model: "model"}.ProcessRequests(ctx, media, []db.SummaryRequest{request})
	if err != nil {
		t.Fatalf("ProcessRequests returned error: %v", err)
	}
	if len(summarizer.calls) != 0 {
		t.Fatalf("summarizer calls = %#v", summarizer.calls)
	}
	if len(sender.messages) != 1 || sender.messages[0] != "cached summary" {
		t.Fatalf("messages = %#v", sender.messages)
	}
	if repo.requests[request.ID].Status != "sent" || repo.requests[request.ID].SummaryCacheID != cached.ID {
		t.Fatalf("request = %#v", repo.requests[request.ID])
	}
}

func TestSummaryServiceUsesFinalSummarySenderWhenAvailable(t *testing.T) {
	ctx := context.Background()
	repo := newSummaryRepo()
	media := db.MediaItem{ID: 1, TranscriptText: "transcript"}
	prompt := summarize.ResolvePrompt("")
	promptHash := summarize.PromptHash(prompt)
	cached := repo.addCache(db.SummaryCache{MediaItemID: media.ID, PromptHash: promptHash, PromptText: prompt, SummaryText: "cached summary", Model: "model"})
	request := repo.addRequest(db.SummaryRequest{MediaItemID: media.ID, ChatID: 10, MessageID: 20, PromptHash: promptHash, PromptText: prompt, Status: "pending_summary"})
	sender := &fakeFinalSummarySender{}

	err := SummaryService{Repo: repo, Summarizer: &fakeSummaryLLM{}, Sender: sender, Model: "model"}.ProcessRequests(ctx, media, []db.SummaryRequest{request})
	if err != nil {
		t.Fatalf("ProcessRequests returned error: %v", err)
	}
	if !slices.Equal(sender.finalSummaries, []string{"cached summary"}) || !slices.Equal(sender.finalReplyTargets, []int64{20}) {
		t.Fatalf("final summaries = %#v targets = %#v", sender.finalSummaries, sender.finalReplyTargets)
	}
	if len(sender.messages) != 0 {
		t.Fatalf("plain messages = %#v", sender.messages)
	}
	if repo.requests[request.ID].Status != "sent" || repo.requests[request.ID].SummaryCacheID != cached.ID {
		t.Fatalf("request = %#v", repo.requests[request.ID])
	}
}

func TestSummaryServiceWatchSummaryQueuesFanoutWithoutProgress(t *testing.T) {
	ctx := context.Background()
	repo := newSummaryRepo()
	progress := &fakeSummaryProgress{}
	service := SummaryService{Repo: repo, Summarizer: &fakeSummaryLLM{}, Sender: &fakeSummarySender{}, Progress: progress, Model: "model"}

	result, err := service.RequestWatchSummary(ctx, WatchSummaryCommand{
		Provider:        "soundon",
		ProviderMediaID: "podcast/episode",
		CanonicalURL:    "https://player.soundon.fm/p/podcast/episodes/episode",
		Subscribers: []WatchSummarySubscriber{
			{ChatID: 10, UserID: 100},
			{ChatID: 11, UserID: 101},
		},
	})
	if err != nil {
		t.Fatalf("RequestWatchSummary returned error: %v", err)
	}
	if !result.CreatedMedia || !result.CreatedTranscriptionJob || result.Job.ID == 0 || len(result.Requests) != 2 {
		t.Fatalf("result = %#v", result)
	}
	if result.Requests[0].MessageID != 0 || result.Requests[1].MessageID != 0 || result.Requests[0].Status != summaryRequestPendingTranscript {
		t.Fatalf("requests = %#v", result.Requests)
	}
	defaultPrompt := summarize.DefaultSummaryVariant().Prompt()
	if result.Requests[0].PromptHash != summarize.PromptHash(defaultPrompt) || result.Requests[0].PromptText != defaultPrompt {
		t.Fatalf("request prompt = %#v", result.Requests[0])
	}
	if len(progress.mediaProgress) != 0 || len(progress.finalSummaries) != 0 {
		t.Fatalf("progress = %#v", progress)
	}
}

func TestSummaryServiceWatchSummaryUsesConfiguredDefaultVariant(t *testing.T) {
	ctx := context.Background()
	repo := newSummaryRepo()
	service := SummaryService{Repo: repo, Summarizer: &fakeSummaryLLM{}, Sender: &fakeSummarySender{}, Model: "model", DefaultSummaryVariant: summarize.VariantTraditional}

	result, err := service.RequestWatchSummary(ctx, WatchSummaryCommand{
		Provider:        "soundon",
		ProviderMediaID: "podcast/episode",
		CanonicalURL:    "https://player.soundon.fm/p/podcast/episodes/episode",
		Subscribers: []WatchSummarySubscriber{
			{ChatID: 10, UserID: 100},
			{ChatID: 11, UserID: 101},
		},
	})
	if err != nil {
		t.Fatalf("RequestWatchSummary returned error: %v", err)
	}
	wantPrompt := summarize.VariantTraditional.Prompt()
	for _, request := range result.Requests {
		if request.PromptText != wantPrompt || request.PromptHash != summarize.PromptHash(wantPrompt) {
			t.Fatalf("request prompt = %#v", request)
		}
	}
}

func TestSummaryServiceWatchSummaryRetryDoesNotDuplicateRequests(t *testing.T) {
	ctx := context.Background()
	repo := newSummaryRepo()
	service := SummaryService{Repo: repo, Summarizer: &fakeSummaryLLM{}, Sender: &fakeSummarySender{}, Model: "model"}
	command := WatchSummaryCommand{
		Provider:        "soundon",
		ProviderMediaID: "podcast/episode",
		CanonicalURL:    "https://player.soundon.fm/p/podcast/episodes/episode",
		Subscribers:     []WatchSummarySubscriber{{ChatID: 10, UserID: 100}, {ChatID: 11, UserID: 101}},
	}
	first, err := service.RequestWatchSummary(ctx, command)
	if err != nil {
		t.Fatalf("first RequestWatchSummary returned error: %v", err)
	}
	second, err := service.RequestWatchSummary(ctx, command)
	if err != nil {
		t.Fatalf("second RequestWatchSummary returned error: %v", err)
	}
	if len(repo.requests) != 2 || len(repo.jobs) != 1 || first.Requests[0].ID != second.Requests[0].ID || first.Requests[1].ID != second.Requests[1].ID {
		t.Fatalf("repo requests=%#v jobs=%#v first=%#v second=%#v", repo.requests, repo.jobs, first.Requests, second.Requests)
	}
}

func TestSummaryServiceWatchSummaryProcessesReadyTranscriptAsNormalMessages(t *testing.T) {
	ctx := context.Background()
	repo := newSummaryRepo()
	repo.addMedia(db.MediaItem{Provider: "xiaoyuzhou", ProviderMediaID: "episode", CanonicalURL: "https://www.xiaoyuzhoufm.com/episode/episode", Status: mediaStatusTranscriptReady, TranscriptText: "transcript"})
	sender := &fakeFinalSummarySender{}
	service := SummaryService{Repo: repo, Summarizer: &fakeSummaryLLM{responses: []string{"watch summary"}}, Sender: sender, Model: "model"}

	result, err := service.RequestWatchSummary(ctx, WatchSummaryCommand{
		Provider:        "xiaoyuzhou",
		ProviderMediaID: "episode",
		CanonicalURL:    "https://www.xiaoyuzhoufm.com/episode/episode",
		Subscribers:     []WatchSummarySubscriber{{ChatID: 10, UserID: 100}, {ChatID: 11, UserID: 101}},
	})
	if err != nil {
		t.Fatalf("RequestWatchSummary returned error: %v", err)
	}
	if !result.Summarized || len(result.Requests) != 2 {
		t.Fatalf("result = %#v", result)
	}
	if !slices.Equal(sender.finalReplyTargets, []int64{0, 0}) || !slices.Equal(sender.finalSummaries, []string{"watch summary", "watch summary"}) {
		t.Fatalf("final summaries=%#v targets=%#v", sender.finalSummaries, sender.finalReplyTargets)
	}
}

func TestSummaryServiceWatchSummarySendsMetadata(t *testing.T) {
	ctx := context.Background()
	repo := newSummaryRepo()
	repo.addMedia(db.MediaItem{Provider: "xiaoyuzhou", ProviderMediaID: "episode", CanonicalURL: "https://www.xiaoyuzhoufm.com/episode/episode", Status: mediaStatusTranscriptReady, TranscriptText: "transcript"})
	sender := &fakeFinalSummarySender{}
	service := SummaryService{Repo: repo, Summarizer: &fakeSummaryLLM{responses: []string{"watch summary"}}, Sender: sender, Model: "model"}
	metadata := display.SummaryMetadata{PodcastTitle: "Podcast", PodcastURL: "podcast-url", EpisodeTitle: "Episode", PubDate: "2026-05-30 00:00:00", Link: "episode-url"}

	result, err := service.RequestWatchSummary(ctx, WatchSummaryCommand{Provider: "xiaoyuzhou", ProviderMediaID: "episode", CanonicalURL: "https://www.xiaoyuzhoufm.com/episode/episode", Subscribers: []WatchSummarySubscriber{{ChatID: 10, UserID: 100}}, Metadata: metadata})
	if err != nil {
		t.Fatalf("RequestWatchSummary returned error: %v", err)
	}
	if !result.Summarized || !slices.Equal(sender.metadata, []display.SummaryMetadata{metadata}) {
		t.Fatalf("result=%#v metadata=%#v", result, sender.metadata)
	}
	if result.Media.Title != "Episode" {
		t.Fatalf("media title = %q", result.Media.Title)
	}
}

func TestSummaryServiceWatchRequestsSkipMediaProgressButSendFinal(t *testing.T) {
	ctx := context.Background()
	repo := newSummaryRepo()
	media := db.MediaItem{ID: 1, TranscriptText: "transcript"}
	prompt := summarize.ResolvePrompt("")
	request := repo.addRequest(db.SummaryRequest{MediaItemID: media.ID, ChatID: 10, MessageID: 0, PromptHash: summarize.PromptHash(prompt), PromptText: prompt, Status: "pending_summary"})
	progress := &fakeSummaryProgress{}

	if err := (SummaryService{Repo: repo, Summarizer: &fakeSummaryLLM{responses: []string{"summary"}}, Sender: &fakeSummarySender{}, Progress: progress, Model: "model"}).ProcessRequests(ctx, media, []db.SummaryRequest{request}); err != nil {
		t.Fatalf("ProcessRequests returned error: %v", err)
	}
	if len(progress.mediaProgress) != 0 || !slices.Equal(progress.finalSummaries, []string{"summary"}) {
		t.Fatalf("progress = %#v", progress)
	}
}

func TestSummaryServiceManualSummaryStillReplies(t *testing.T) {
	ctx := context.Background()
	repo := newSummaryRepo()
	media := db.MediaItem{ID: 1, TranscriptText: "transcript"}
	prompt := summarize.ResolvePrompt("")
	request := repo.addRequest(db.SummaryRequest{MediaItemID: media.ID, ChatID: 10, MessageID: 20, PromptHash: summarize.PromptHash(prompt), PromptText: prompt, Status: "pending_summary"})
	sender := &fakeFinalSummarySender{}

	if err := (SummaryService{Repo: repo, Summarizer: &fakeSummaryLLM{responses: []string{"summary"}}, Sender: sender, Model: "model"}).ProcessRequests(ctx, media, []db.SummaryRequest{request}); err != nil {
		t.Fatalf("ProcessRequests returned error: %v", err)
	}
	if !slices.Equal(sender.finalReplyTargets, []int64{20}) {
		t.Fatalf("reply targets = %#v", sender.finalReplyTargets)
	}
}

func TestSummaryServiceBroadcastsGeneratedSummaryAfterSuccessfulDelivery(t *testing.T) {
	ctx := context.Background()
	repo := newSummaryRepo()
	media := db.MediaItem{ID: 1, TranscriptText: "transcript"}
	prompt := summarize.ResolvePrompt("")
	promptHash := summarize.PromptHash(prompt)
	request1 := repo.addRequest(db.SummaryRequest{MediaItemID: media.ID, ChatID: 10, PromptHash: promptHash, PromptText: prompt, Status: "pending_summary"})
	request2 := repo.addRequest(db.SummaryRequest{MediaItemID: media.ID, ChatID: 11, PromptHash: promptHash, PromptText: prompt, Status: "pending_summary"})
	sender := &fakeFinalSummarySender{}
	broadcaster := &fakeSummaryBroadcaster{}

	err := SummaryService{Repo: repo, Summarizer: &fakeSummaryLLM{responses: []string{"generated summary"}}, Sender: sender, SummaryBroadcastChannelID: -1001234567890, SummaryBroadcaster: broadcaster, Model: "model"}.ProcessRequests(ctx, media, []db.SummaryRequest{request1, request2})
	if err != nil {
		t.Fatalf("ProcessRequests returned error: %v", err)
	}
	if !slices.Equal(sender.finalSummaries, []string{"generated summary", "generated summary"}) {
		t.Fatalf("final summaries = %#v", sender.finalSummaries)
	}
	if len(broadcaster.calls) != 1 {
		t.Fatalf("broadcast calls = %#v", broadcaster.calls)
	}
	if broadcaster.calls[0].chatID != -1001234567890 || broadcaster.calls[0].text != "generated summary" {
		t.Fatalf("broadcast call = %#v", broadcaster.calls[0])
	}
}

func TestSummaryServiceDoesNotBroadcastCachedSummary(t *testing.T) {
	ctx := context.Background()
	repo := newSummaryRepo()
	media := db.MediaItem{ID: 1, TranscriptText: "transcript"}
	prompt := summarize.ResolvePrompt("")
	promptHash := summarize.PromptHash(prompt)
	repo.addCache(db.SummaryCache{MediaItemID: media.ID, PromptHash: promptHash, PromptText: prompt, SummaryText: "cached summary", Model: "model"})
	request := repo.addRequest(db.SummaryRequest{MediaItemID: media.ID, ChatID: 10, PromptHash: promptHash, PromptText: prompt, Status: "pending_summary"})
	broadcaster := &fakeSummaryBroadcaster{}

	err := SummaryService{Repo: repo, Summarizer: &fakeSummaryLLM{}, Sender: &fakeFinalSummarySender{}, SummaryBroadcastChannelID: -1001234567890, SummaryBroadcaster: broadcaster, Model: "model"}.ProcessRequests(ctx, media, []db.SummaryRequest{request})
	if err != nil {
		t.Fatalf("ProcessRequests returned error: %v", err)
	}
	if len(broadcaster.calls) != 0 {
		t.Fatalf("broadcast calls = %#v", broadcaster.calls)
	}
}

func TestSummaryServiceDoesNotBroadcastWhenFinalSendFails(t *testing.T) {
	ctx := context.Background()
	repo := newSummaryRepo()
	media := db.MediaItem{ID: 1, TranscriptText: "transcript"}
	prompt := summarize.ResolvePrompt("")
	request := repo.addRequest(db.SummaryRequest{MediaItemID: media.ID, ChatID: 10, PromptHash: summarize.PromptHash(prompt), PromptText: prompt, Status: "pending_summary"})
	broadcaster := &fakeSummaryBroadcaster{}
	sender := &fakeFinalSummarySender{finalErrors: map[int64]error{10: errors.New("send failed")}}

	err := SummaryService{Repo: repo, Summarizer: &fakeSummaryLLM{responses: []string{"generated summary"}}, Sender: sender, SummaryBroadcastChannelID: -1001234567890, SummaryBroadcaster: broadcaster, Model: "model"}.ProcessRequests(ctx, media, []db.SummaryRequest{request})
	if !IsDurableSummaryDeliveryError(err) {
		t.Fatalf("expected durable delivery error, got %v", err)
	}
	if len(broadcaster.calls) != 0 {
		t.Fatalf("broadcast calls = %#v", broadcaster.calls)
	}
}

func TestSummaryServiceBroadcastFailureIsNonFatal(t *testing.T) {
	ctx := context.Background()
	repo := newSummaryRepo()
	media := db.MediaItem{ID: 1, TranscriptText: "transcript"}
	prompt := summarize.ResolvePrompt("")
	request := repo.addRequest(db.SummaryRequest{MediaItemID: media.ID, ChatID: 10, PromptHash: summarize.PromptHash(prompt), PromptText: prompt, Status: "pending_summary"})
	broadcaster := &fakeSummaryBroadcaster{err: errors.New("channel send failed")}

	err := SummaryService{Repo: repo, Summarizer: &fakeSummaryLLM{responses: []string{"generated summary"}}, Sender: &fakeFinalSummarySender{}, SummaryBroadcastChannelID: -1001234567890, SummaryBroadcaster: broadcaster, Model: "model"}.ProcessRequests(ctx, media, []db.SummaryRequest{request})
	if err != nil {
		t.Fatalf("ProcessRequests returned error: %v", err)
	}
	if repo.requests[request.ID].Status != "sent" {
		t.Fatalf("request = %#v", repo.requests[request.ID])
	}
	if len(broadcaster.calls) != 1 {
		t.Fatalf("broadcast calls = %#v", broadcaster.calls)
	}
}

func TestSummaryServiceBroadcastsWatchGeneratedSummaryOnce(t *testing.T) {
	ctx := context.Background()
	repo := newSummaryRepo()
	repo.addMedia(db.MediaItem{Provider: "xiaoyuzhou", ProviderMediaID: "episode", CanonicalURL: "https://www.xiaoyuzhoufm.com/episode/episode", Status: mediaStatusTranscriptReady, TranscriptText: "transcript"})
	sender := &fakeFinalSummarySender{}
	broadcaster := &fakeSummaryBroadcaster{}
	service := SummaryService{Repo: repo, Summarizer: &fakeSummaryLLM{responses: []string{"watch summary"}}, Sender: sender, SummaryBroadcastChannelID: -1001234567890, SummaryBroadcaster: broadcaster, Model: "model"}

	result, err := service.RequestWatchSummary(ctx, WatchSummaryCommand{Provider: "xiaoyuzhou", ProviderMediaID: "episode", CanonicalURL: "https://www.xiaoyuzhoufm.com/episode/episode", Subscribers: []WatchSummarySubscriber{{ChatID: 10, UserID: 100}, {ChatID: 11, UserID: 101}}})
	if err != nil {
		t.Fatalf("RequestWatchSummary returned error: %v", err)
	}
	if !result.Summarized || !slices.Equal(sender.finalSummaries, []string{"watch summary", "watch summary"}) {
		t.Fatalf("result=%#v final summaries=%#v", result, sender.finalSummaries)
	}
	if len(broadcaster.calls) != 1 || broadcaster.calls[0].text != "watch summary" {
		t.Fatalf("broadcast calls = %#v", broadcaster.calls)
	}
}

func TestSummaryServiceSwitchSummaryVariantReusesCacheAndEditsStoredMessages(t *testing.T) {
	ctx := context.Background()
	repo := newSummaryRepo()
	media := repo.addMedia(db.MediaItem{Provider: "youtube", ProviderMediaID: "abc12345678", TranscriptText: "transcript", Status: mediaStatusTranscriptReady})
	request := repo.addRequest(db.SummaryRequest{MediaItemID: media.ID, ChatID: 10, UserID: 20, PromptHash: summarize.PromptHash(summarize.DefaultPrompt), PromptText: summarize.DefaultPrompt, Status: "sent", SummaryCacheID: 1})
	targetPrompt := summarize.VariantTraditional.Prompt()
	targetCache := repo.addCache(db.SummaryCache{MediaItemID: media.ID, PromptHash: summarize.PromptHash(targetPrompt), PromptText: targetPrompt, SummaryText: "cached traditional", Model: "model"})
	repo.CreateSummaryRequestMessage(ctx, db.SummaryRequestMessage{SummaryRequestID: request.ID, ChatID: request.ChatID, TelegramMessageID: 101, Kind: RequestMessageKindSummaryPart1})
	repo.CreateSummaryRequestMessage(ctx, db.SummaryRequestMessage{SummaryRequestID: request.ID, ChatID: request.ChatID, TelegramMessageID: 102, Kind: RequestMessageKindSummaryPart2})
	sender := &fakeSummaryEditor{}

	err := SummaryService{Repo: repo, Summarizer: &fakeSummaryLLM{}, Sender: sender, Model: "model"}.SwitchSummaryVariant(ctx, SwitchSummaryVariantCommand{
		RequestID: request.ID,
		ChatID:    request.ChatID,
		UserID:    30,
		MessageID: 102,
		Variant:   summarize.VariantTraditional,
	})
	if err != nil {
		t.Fatalf("SwitchSummaryVariant returned error: %v", err)
	}
	if len(sender.edits) != 1 || !slices.Equal(sender.edits[0].messageIDs, []int64{101, 102}) || sender.edits[0].text != "cached traditional" {
		t.Fatalf("edits = %#v", sender.edits)
	}
	if repo.requests[request.ID].SummaryCacheID != targetCache.ID {
		t.Fatalf("request = %#v", repo.requests[request.ID])
	}
}

func TestSummaryServiceSwitchSummaryVariantRejectsMismatchedMessage(t *testing.T) {
	ctx := context.Background()
	repo := newSummaryRepo()
	media := repo.addMedia(db.MediaItem{Provider: "youtube", ProviderMediaID: "abc12345678", TranscriptText: "transcript", Status: mediaStatusTranscriptReady})
	request := repo.addRequest(db.SummaryRequest{MediaItemID: media.ID, ChatID: 10, Status: "sent"})
	repo.CreateSummaryRequestMessage(ctx, db.SummaryRequestMessage{SummaryRequestID: request.ID, ChatID: request.ChatID, TelegramMessageID: 101, Kind: RequestMessageKindSummaryPart1})
	repo.CreateSummaryRequestMessage(ctx, db.SummaryRequestMessage{SummaryRequestID: request.ID, ChatID: request.ChatID, TelegramMessageID: 102, Kind: RequestMessageKindSummaryPart2})
	sender := &fakeSummaryEditor{}

	err := SummaryService{Repo: repo, Summarizer: &fakeSummaryLLM{}, Sender: sender, Model: "model"}.SwitchSummaryVariant(ctx, SwitchSummaryVariantCommand{
		RequestID: request.ID,
		ChatID:    request.ChatID,
		MessageID: 101,
		Variant:   summarize.VariantTraditional,
	})
	if err == nil {
		t.Fatal("SwitchSummaryVariant returned nil error")
	}
	if len(sender.edits) != 0 {
		t.Fatalf("edits = %#v", sender.edits)
	}
}

func TestSummaryServiceCustomPromptCacheMiss(t *testing.T) {
	ctx := context.Background()
	repo := newSummaryRepo()
	media := db.MediaItem{ID: 1, TranscriptText: "transcript"}
	defaultPrompt := summarize.ResolvePrompt("")
	repo.addCache(db.SummaryCache{MediaItemID: media.ID, PromptHash: summarize.PromptHash(defaultPrompt), PromptText: defaultPrompt, SummaryText: "cached summary", Model: "model"})
	customPrompt := "custom prompt"
	request := repo.addRequest(db.SummaryRequest{MediaItemID: media.ID, ChatID: 10, PromptHash: summarize.PromptHash(customPrompt), PromptText: customPrompt, Status: "pending_summary"})
	summarizer := &fakeSummaryLLM{responses: []string{"new summary"}}
	sender := &fakeSummarySender{}

	err := SummaryService{Repo: repo, Summarizer: summarizer, Sender: sender, Model: "model"}.ProcessRequests(ctx, media, []db.SummaryRequest{request})
	if err != nil {
		t.Fatalf("ProcessRequests returned error: %v", err)
	}
	if len(summarizer.calls) != 1 || summarizer.calls[0].prompt != customPrompt || summarizer.calls[0].transcript != "transcript" {
		t.Fatalf("summarizer calls = %#v", summarizer.calls)
	}
	if len(sender.messages) != 1 || sender.messages[0] != "new summary" {
		t.Fatalf("messages = %#v", sender.messages)
	}
	if repo.requests[request.ID].Status != "sent" {
		t.Fatalf("request = %#v", repo.requests[request.ID])
	}
}

func TestSummaryServiceFanoutUsesOneSummary(t *testing.T) {
	ctx := context.Background()
	repo := newSummaryRepo()
	media := db.MediaItem{ID: 1, TranscriptText: "transcript"}
	prompt := "prompt"
	first := repo.addRequest(db.SummaryRequest{MediaItemID: media.ID, ChatID: 10, PromptHash: summarize.PromptHash(prompt), PromptText: prompt, Status: "pending_summary"})
	second := repo.addRequest(db.SummaryRequest{MediaItemID: media.ID, ChatID: 11, PromptHash: summarize.PromptHash(prompt), PromptText: prompt, Status: "pending_summary"})
	summarizer := &fakeSummaryLLM{responses: []string{"shared summary"}}
	sender := &fakeSummarySender{}

	err := SummaryService{Repo: repo, Summarizer: summarizer, Sender: sender, Model: "model"}.ProcessRequests(ctx, media, []db.SummaryRequest{first, second})
	if err != nil {
		t.Fatalf("ProcessRequests returned error: %v", err)
	}
	if len(summarizer.calls) != 1 {
		t.Fatalf("summarizer calls = %#v", summarizer.calls)
	}
	if len(sender.messages) != 2 || sender.messages[0] != "shared summary" || sender.messages[1] != "shared summary" {
		t.Fatalf("messages = %#v", sender.messages)
	}
	if repo.requests[first.ID].SummaryCacheID != repo.requests[second.ID].SummaryCacheID {
		t.Fatalf("requests = %#v %#v", repo.requests[first.ID], repo.requests[second.ID])
	}
}

func TestSummaryServiceMarksPendingTranscriptBeforeSummary(t *testing.T) {
	ctx := context.Background()
	repo := newSummaryRepo()
	media := db.MediaItem{ID: 1, TranscriptText: "transcript"}
	prompt := "prompt"
	request := repo.addRequest(db.SummaryRequest{MediaItemID: media.ID, ChatID: 10, PromptHash: summarize.PromptHash(prompt), PromptText: prompt, Status: "pending_transcript"})
	summarizer := &fakeSummaryLLM{responses: []string{"summary"}}
	sender := &fakeSummarySender{}

	err := SummaryService{Repo: repo, Summarizer: summarizer, Sender: sender, Model: "model"}.ProcessRequests(ctx, media, []db.SummaryRequest{request})
	if err != nil {
		t.Fatalf("ProcessRequests returned error: %v", err)
	}
	wantTransitions := []string{"pending_summary", summaryRequestSummarizing, summaryRequestSending, "sent"}
	if !slices.Equal(repo.transitions[request.ID], wantTransitions) {
		t.Fatalf("transitions = %#v, want %#v", repo.transitions[request.ID], wantTransitions)
	}
}

func TestSummaryServiceContinuesFanoutAfterSendFailure(t *testing.T) {
	ctx := context.Background()
	repo := newSummaryRepo()
	media := db.MediaItem{ID: 1, TranscriptText: "transcript"}
	prompt := "prompt"
	first := repo.addRequest(db.SummaryRequest{MediaItemID: media.ID, ChatID: 10, PromptHash: summarize.PromptHash(prompt), PromptText: prompt, Status: "pending_summary"})
	second := repo.addRequest(db.SummaryRequest{MediaItemID: media.ID, ChatID: 11, PromptHash: summarize.PromptHash(prompt), PromptText: prompt, Status: "pending_summary"})
	summarizer := &fakeSummaryLLM{responses: []string{"shared summary"}}
	sender := &fakeSummarySender{errors: map[int64]error{10: errors.New("telegram send failed")}}

	err := SummaryService{Repo: repo, Summarizer: summarizer, Sender: sender, Model: "model"}.ProcessRequests(ctx, media, []db.SummaryRequest{first, second})
	if err == nil {
		t.Fatal("ProcessRequests returned nil error")
	}
	if repo.requests[first.ID].Status != summaryRequestPendingSummary {
		t.Fatalf("first request = %#v", repo.requests[first.ID])
	}
	if repo.requests[second.ID].Status != "sent" {
		t.Fatalf("second request = %#v", repo.requests[second.ID])
	}
}

func TestSummaryServiceMarksAmbiguousSendFailureDeliveryUnknown(t *testing.T) {
	ctx := context.Background()
	repo := newSummaryRepo()
	media := db.MediaItem{ID: 1, TranscriptText: "transcript"}
	prompt := "prompt"
	request := repo.addRequest(db.SummaryRequest{MediaItemID: media.ID, ChatID: 10, PromptHash: summarize.PromptHash(prompt), PromptText: prompt, Status: summaryRequestPendingSummary})
	summarizer := &fakeSummaryLLM{responses: []string{"summary"}}
	sender := &fakeSummarySender{errors: map[int64]error{10: errors.New("telegram send failed")}, ambiguous: true}

	err := SummaryService{Repo: repo, Summarizer: summarizer, Sender: sender, Model: "model"}.ProcessRequests(ctx, media, []db.SummaryRequest{request})
	if err == nil {
		t.Fatal("ProcessRequests returned nil error")
	}
	if repo.requests[request.ID].Status != summaryRequestDeliveryUnknown {
		t.Fatalf("request = %#v", repo.requests[request.ID])
	}
}

func TestSummaryServiceTreatsPostSendPersistenceFailureAsDurableDelivery(t *testing.T) {
	ctx := context.Background()
	repo := newSummaryRepo()
	media := db.MediaItem{ID: 1, TranscriptText: "transcript"}
	prompt := "prompt"
	request := repo.addRequest(db.SummaryRequest{MediaItemID: media.ID, ChatID: 10, PromptHash: summarize.PromptHash(prompt), PromptText: prompt, Status: "pending_summary"})
	repo.markSentErr = errors.New("database unavailable")
	sender := &fakeSummarySender{}

	err := SummaryService{Repo: repo, Summarizer: &fakeSummaryLLM{responses: []string{"summary"}}, Sender: sender, Model: "model"}.ProcessRequests(ctx, media, []db.SummaryRequest{request})
	if !IsDurableSummaryDeliveryError(err) {
		t.Fatalf("ProcessRequests error = %v", err)
	}
	if repo.requests[request.ID].Status != summaryRequestDeliveryUnknown {
		t.Fatalf("request = %#v", repo.requests[request.ID])
	}
	if !slices.Equal(sender.messages, []string{"summary"}) {
		t.Fatalf("messages = %#v", sender.messages)
	}
}

func TestSummaryServiceContinuesAfterPromptGroupFailure(t *testing.T) {
	ctx := context.Background()
	repo := newSummaryRepo()
	media := db.MediaItem{ID: 1, TranscriptText: "transcript"}
	badPrompt := "bad prompt"
	goodPrompt := "good prompt"
	failed := repo.addRequest(db.SummaryRequest{MediaItemID: media.ID, ChatID: 10, PromptHash: summarize.PromptHash(badPrompt), PromptText: badPrompt, Status: "pending_summary"})
	sent := repo.addRequest(db.SummaryRequest{MediaItemID: media.ID, ChatID: 11, PromptHash: summarize.PromptHash(goodPrompt), PromptText: goodPrompt, Status: "pending_summary"})
	summarizer := &fakeSummaryLLM{
		errors:            map[string]error{badPrompt: errors.New("summarization failed")},
		responsesByPrompt: map[string]string{goodPrompt: "good summary"},
	}
	sender := &fakeSummarySender{}

	err := SummaryService{Repo: repo, Summarizer: summarizer, Sender: sender, Model: "model"}.ProcessRequests(ctx, media, []db.SummaryRequest{failed, sent})
	if err == nil {
		t.Fatal("ProcessRequests returned nil error")
	}
	if repo.requests[failed.ID].Status != summaryRequestPendingSummary {
		t.Fatalf("failed request = %#v", repo.requests[failed.ID])
	}
	if repo.requests[sent.ID].Status != "sent" {
		t.Fatalf("sent request = %#v", repo.requests[sent.ID])
	}
	if !slices.Equal(sender.messages, []string{"good summary"}) {
		t.Fatalf("messages = %#v", sender.messages)
	}
}

func TestSummaryServiceSkipsAlreadyClaimedRequests(t *testing.T) {
	ctx := context.Background()
	repo := newSummaryRepo()
	media := db.MediaItem{ID: 1, TranscriptText: "transcript"}
	prompt := "prompt"
	request := repo.addRequest(db.SummaryRequest{MediaItemID: media.ID, ChatID: 10, PromptHash: summarize.PromptHash(prompt), PromptText: prompt, Status: "pending_summary"})
	summarizer := &fakeSummaryLLM{responses: []string{"summary"}}
	sender := &fakeSummarySender{}
	service := SummaryService{Repo: repo, Summarizer: summarizer, Sender: sender, Model: "model"}

	if err := service.ProcessRequests(ctx, media, []db.SummaryRequest{request}); err != nil {
		t.Fatalf("first ProcessRequests returned error: %v", err)
	}
	if err := service.ProcessRequests(ctx, media, []db.SummaryRequest{request}); err != nil {
		t.Fatalf("second ProcessRequests returned error: %v", err)
	}
	if !slices.Equal(sender.messages, []string{"summary"}) {
		t.Fatalf("messages = %#v", sender.messages)
	}
}

func TestSummaryServiceKeepsOperationalCacheErrorsRetryable(t *testing.T) {
	ctx := context.Background()
	repo := newSummaryRepo()
	media := db.MediaItem{ID: 1, TranscriptText: "transcript"}
	prompt := "prompt"
	request := repo.addRequest(db.SummaryRequest{MediaItemID: media.ID, ChatID: 10, PromptHash: summarize.PromptHash(prompt), PromptText: prompt, Status: "pending_summary"})
	repo.cacheErr = errors.New("database unavailable")

	err := SummaryService{Repo: repo, Summarizer: &fakeSummaryLLM{}, Sender: &fakeSummarySender{}, Model: "model"}.ProcessRequests(ctx, media, []db.SummaryRequest{request})
	if err == nil {
		t.Fatal("ProcessRequests returned nil error")
	}
	if repo.requests[request.ID].Status != summaryRequestPendingSummary {
		t.Fatalf("request = %#v", repo.requests[request.ID])
	}
}

func TestSummaryServiceKeepsInsertFallbackErrorsRetryable(t *testing.T) {
	ctx := context.Background()
	repo := newSummaryRepo()
	media := db.MediaItem{ID: 1, TranscriptText: "transcript"}
	prompt := "prompt"
	request := repo.addRequest(db.SummaryRequest{MediaItemID: media.ID, ChatID: 10, PromptHash: summarize.PromptHash(prompt), PromptText: prompt, Status: "pending_summary"})
	repo.insertErr = errors.New("insert failed")
	summarizer := &fakeSummaryLLM{responses: []string{"summary"}}

	err := SummaryService{Repo: repo, Summarizer: summarizer, Sender: &fakeSummarySender{}, Model: "model"}.ProcessRequests(ctx, media, []db.SummaryRequest{request})
	if err == nil {
		t.Fatal("ProcessRequests returned nil error")
	}
	if repo.requests[request.ID].Status != summaryRequestPendingSummary {
		t.Fatalf("request = %#v", repo.requests[request.ID])
	}
}

func TestSummaryServiceReusesCacheAfterConcurrentInsert(t *testing.T) {
	ctx := context.Background()
	repo := newSummaryRepo()
	media := db.MediaItem{ID: 1, TranscriptText: "transcript"}
	prompt := "prompt"
	promptHash := summarize.PromptHash(prompt)
	request := repo.addRequest(db.SummaryRequest{MediaItemID: media.ID, ChatID: 10, PromptHash: promptHash, PromptText: prompt, Status: "pending_summary"})
	repo.insertErr = errors.New("unique cache conflict")
	repo.cacheOnInsertErr = db.SummaryCache{MediaItemID: media.ID, PromptHash: promptHash, PromptText: prompt, SummaryText: "concurrent summary", Model: "model"}
	summarizer := &fakeSummaryLLM{responses: []string{"new summary"}}
	sender := &fakeSummarySender{}

	err := SummaryService{Repo: repo, Summarizer: summarizer, Sender: sender, Model: "model"}.ProcessRequests(ctx, media, []db.SummaryRequest{request})
	if err != nil {
		t.Fatalf("ProcessRequests returned error: %v", err)
	}
	if len(sender.messages) != 1 || sender.messages[0] != "concurrent summary" {
		t.Fatalf("messages = %#v", sender.messages)
	}
	if repo.requests[request.ID].Status != "sent" {
		t.Fatalf("request = %#v", repo.requests[request.ID])
	}
}

func TestSummaryServiceRequestUsesTranscriptCache(t *testing.T) {
	repo := newSummaryRepo()
	prompt := summarize.ResolvePrompt("")
	media := repo.addMedia(db.MediaItem{Provider: "youtube", ProviderMediaID: "abc12345678", CanonicalURL: "https://www.youtube.com/watch?v=abc12345678", Status: mediaStatusTranscriptReady, TranscriptText: "transcript"})
	repo.addCache(db.SummaryCache{MediaItemID: media.ID, PromptHash: summarize.PromptHash(prompt), PromptText: prompt, SummaryText: "cached summary", Model: "model"})
	summarizer := &fakeSummaryLLM{}
	sender := &fakeSummarySender{}
	downloader := &fakeSubtitleDownloader{}
	service := newSummaryService(repo, summarizer, sender, downloader)

	result, err := service.RequestSummary(context.Background(), summaryCommand(""))
	if err != nil {
		t.Fatalf("RequestSummary returned error: %v", err)
	}
	if !result.Summarized || result.Request.Status != summaryRequestPendingSummary {
		t.Fatalf("result = %#v", result)
	}
	if len(summarizer.calls) != 0 || len(downloader.urls) != 0 {
		t.Fatalf("summarizer calls = %#v, downloader urls = %#v", summarizer.calls, downloader.urls)
	}
	if len(sender.messages) != 1 || sender.messages[0] != "cached summary" {
		t.Fatalf("messages = %#v", sender.messages)
	}
}

func TestSummaryServiceRequestStoresSubtitleAndSummarizes(t *testing.T) {
	repo := newSummaryRepo()
	summarizer := &fakeSummaryLLM{responses: []string{"subtitle summary"}}
	sender := &fakeSummarySender{}
	downloader := &fakeSubtitleDownloader{result: transcript.DownloadResult{Source: "manual_subtitle", Text: "subtitle transcript"}}
	service := newSummaryService(repo, summarizer, sender, downloader)

	result, err := service.RequestSummary(context.Background(), summaryCommand("custom prompt"))
	if err != nil {
		t.Fatalf("RequestSummary returned error: %v", err)
	}
	if !result.CreatedMedia || !result.UsedSubtitle || !result.Summarized {
		t.Fatalf("result = %#v", result)
	}
	if repo.media[summaryMediaKey("youtube", "abc12345678")].TranscriptSource != "manual_subtitle" {
		t.Fatalf("media = %#v", repo.media)
	}
	if len(summarizer.calls) != 1 || summarizer.calls[0].prompt != "custom prompt" || summarizer.calls[0].transcript != "subtitle transcript" {
		t.Fatalf("summarizer calls = %#v", summarizer.calls)
	}
	if len(sender.messages) != 1 || sender.messages[0] != "subtitle summary" {
		t.Fatalf("messages = %#v", sender.messages)
	}
}

func TestSummaryServiceUsesPersistedTranscriptAfterSubtitleRace(t *testing.T) {
	repo := newSummaryRepo()
	repo.addMedia(db.MediaItem{Provider: "youtube", ProviderMediaID: "abc12345678", CanonicalURL: "https://www.youtube.com/watch?v=abc12345678", Status: "new"})
	repo.updateRaceSource = "whisper"
	repo.updateRaceText = "persisted transcript"
	summarizer := &fakeSummaryLLM{responses: []string{"persisted summary"}}
	sender := &fakeSummarySender{}
	downloader := &fakeSubtitleDownloader{result: transcript.DownloadResult{Source: "manual_subtitle", Text: "downloaded subtitle"}}
	service := newSummaryService(repo, summarizer, sender, downloader)

	result, err := service.RequestSummary(context.Background(), summaryCommand("custom prompt"))
	if err != nil {
		t.Fatalf("RequestSummary returned error: %v", err)
	}
	if !result.UsedSubtitle || !result.Summarized {
		t.Fatalf("result = %#v", result)
	}
	if len(summarizer.calls) != 1 || summarizer.calls[0].transcript != "persisted transcript" {
		t.Fatalf("summarizer calls = %#v", summarizer.calls)
	}
}

func TestSummaryServiceRequestQueuesTranscriptionJob(t *testing.T) {
	repo := newSummaryRepo()
	summarizer := &fakeSummaryLLM{}
	sender := &fakeSummarySender{}
	downloader := &fakeSubtitleDownloader{err: errors.New("no subtitles")}
	service := newSummaryService(repo, summarizer, sender, downloader)

	result, err := service.RequestSummary(context.Background(), summaryCommand(""))
	if err != nil {
		t.Fatalf("RequestSummary returned error: %v", err)
	}
	if !result.CreatedTranscriptionJob || result.Request.Status != summaryRequestPendingTranscript || result.Job.Status != mediaStatusQueued {
		t.Fatalf("result = %#v", result)
	}
	if len(summarizer.calls) != 0 || len(sender.messages) != 0 {
		t.Fatalf("summarizer calls = %#v, messages = %#v", summarizer.calls, sender.messages)
	}
}

func TestSummaryServiceRequestUsesConfiguredDefaultVariant(t *testing.T) {
	repo := newSummaryRepo()
	summarizer := &fakeSummaryLLM{}
	sender := &fakeSummarySender{}
	downloader := &fakeSubtitleDownloader{err: errors.New("no subtitles")}
	service := newSummaryService(repo, summarizer, sender, downloader)
	service.DefaultSummaryVariant = summarize.VariantTraditional

	result, err := service.RequestSummary(context.Background(), summaryCommand(""))
	if err != nil {
		t.Fatalf("RequestSummary returned error: %v", err)
	}
	wantPrompt := summarize.VariantTraditional.Prompt()
	if result.Request.PromptText != wantPrompt || result.Request.PromptHash != summarize.PromptHash(wantPrompt) {
		t.Fatalf("request prompt = %#v", result.Request)
	}
}

func TestSummaryServiceRequestSkipsSubtitleForPodcastProviders(t *testing.T) {
	for _, tc := range []provider.MediaRef{
		{Provider: "xiaoyuzhou", ProviderMediaID: "69ebf1d71d989496e7729801", CanonicalURL: "https://www.xiaoyuzhoufm.com/episode/69ebf1d71d989496e7729801"},
		{Provider: "soundon", ProviderMediaID: "954689a5-3096-43a4-a80b-7810b219cef3/181f554e-900e-4261-bc3c-1a3fe53a902a", CanonicalURL: "https://player.soundon.fm/p/954689a5-3096-43a4-a80b-7810b219cef3/episodes/181f554e-900e-4261-bc3c-1a3fe53a902a"},
	} {
		t.Run(tc.Provider, func(t *testing.T) {
			repo := newSummaryRepo()
			downloader := &fakeSubtitleDownloader{err: errors.New("should not be called")}
			service := newSummaryService(repo, &fakeSummaryLLM{}, &fakeSummarySender{}, downloader)
			service.Registry = fakeSummaryRegistry{ref: tc}

			result, err := service.RequestSummary(context.Background(), summaryCommand(""))
			if err != nil {
				t.Fatalf("RequestSummary returned error: %v", err)
			}
			if !result.CreatedTranscriptionJob || result.Request.Status != summaryRequestPendingTranscript {
				t.Fatalf("result = %#v", result)
			}
			if len(downloader.urls) != 0 {
				t.Fatalf("downloader urls = %#v", downloader.urls)
			}
		})
	}
}

func TestSummaryServiceRequestSubscribesToActiveJob(t *testing.T) {
	repo := newSummaryRepo()
	media := repo.addMedia(db.MediaItem{Provider: "youtube", ProviderMediaID: "abc12345678", CanonicalURL: "https://www.youtube.com/watch?v=abc12345678", Status: mediaStatusQueued})
	repo.addJob(db.TranscriptionJob{MediaItemID: media.ID, Status: mediaStatusQueued})
	downloader := &fakeSubtitleDownloader{err: errors.New("should not be called")}
	service := newSummaryService(repo, &fakeSummaryLLM{}, &fakeSummarySender{}, downloader)

	result, err := service.RequestSummary(context.Background(), summaryCommand(""))
	if err != nil {
		t.Fatalf("RequestSummary returned error: %v", err)
	}
	if result.CreatedTranscriptionJob || result.Request.Status != summaryRequestPendingTranscript || result.Job.ID == 0 {
		t.Fatalf("result = %#v", result)
	}
	if len(downloader.urls) != 0 {
		t.Fatalf("downloader urls = %#v", downloader.urls)
	}
}

func TestSummaryServiceRecoversInterruptedSummaryRequests(t *testing.T) {
	repo := newSummaryRepo()
	media := repo.addMedia(db.MediaItem{Provider: "youtube", ProviderMediaID: "abc12345678", CanonicalURL: "https://www.youtube.com/watch?v=abc12345678", Status: mediaStatusTranscriptReady, TranscriptText: "transcript"})
	prompt := "prompt"
	interrupted := repo.addRequest(db.SummaryRequest{MediaItemID: media.ID, ChatID: 10, PromptHash: summarize.PromptHash(prompt), PromptText: prompt, Status: summaryRequestSummarizing})
	pendingTranscript := repo.addRequest(db.SummaryRequest{MediaItemID: media.ID, ChatID: 11, PromptHash: summarize.PromptHash(prompt), PromptText: prompt, Status: summaryRequestPendingTranscript})
	summarizer := &fakeSummaryLLM{responses: []string{"summary"}}
	sender := &fakeSummarySender{}
	service := newSummaryService(repo, summarizer, sender, &fakeSubtitleDownloader{})

	requeued, err := service.RequeueInterruptedSummaryRequests(context.Background())
	if err != nil {
		t.Fatalf("RequeueInterruptedSummaryRequests returned error: %v", err)
	}
	if requeued != 1 {
		t.Fatalf("requeued = %d", requeued)
	}
	if err := service.ProcessRecoverableSummaryRequests(context.Background()); err != nil {
		t.Fatalf("ProcessRecoverableSummaryRequests returned error: %v", err)
	}
	if repo.requests[interrupted.ID].Status != "sent" || repo.requests[pendingTranscript.ID].Status != "sent" {
		t.Fatalf("requests = %#v %#v", repo.requests[interrupted.ID], repo.requests[pendingTranscript.ID])
	}
	if !slices.Equal(sender.messages, []string{"summary", "summary"}) {
		t.Fatalf("messages = %#v", sender.messages)
	}
}

func TestSummaryServiceRecoversStaleSummaryRequests(t *testing.T) {
	repo := newSummaryRepo()
	media := repo.addMedia(db.MediaItem{Provider: "youtube", ProviderMediaID: "abc12345678", CanonicalURL: "https://www.youtube.com/watch?v=abc12345678", Status: mediaStatusTranscriptReady, TranscriptText: "transcript"})
	prompt := "prompt"
	stale := repo.addRequest(db.SummaryRequest{MediaItemID: media.ID, ChatID: 10, PromptHash: summarize.PromptHash(prompt), PromptText: prompt, Status: summaryRequestSummarizing, UpdatedAt: time.Now().Add(-summaryRequestLeaseTimeout - time.Minute).Format(time.DateTime)})
	fresh := repo.addRequest(db.SummaryRequest{MediaItemID: media.ID, ChatID: 11, PromptHash: summarize.PromptHash(prompt), PromptText: prompt, Status: summaryRequestSummarizing, UpdatedAt: time.Now().Format(time.DateTime)})
	sender := &fakeSummarySender{}
	service := newSummaryService(repo, &fakeSummaryLLM{responses: []string{"summary"}}, sender, &fakeSubtitleDownloader{})

	if err := service.ProcessRecoverableSummaryRequests(context.Background()); err != nil {
		t.Fatalf("ProcessRecoverableSummaryRequests returned error: %v", err)
	}
	if repo.requests[stale.ID].Status != "sent" || repo.requests[fresh.ID].Status != summaryRequestSummarizing {
		t.Fatalf("requests = %#v %#v", repo.requests[stale.ID], repo.requests[fresh.ID])
	}
	if !slices.Equal(sender.messages, []string{"summary"}) {
		t.Fatalf("messages = %#v", sender.messages)
	}
}

func TestSummaryServiceRecoversPendingTranscriptRequestsWithoutJob(t *testing.T) {
	repo := newSummaryRepo()
	media := repo.addMedia(db.MediaItem{Provider: "youtube", ProviderMediaID: "abc12345678", CanonicalURL: "https://www.youtube.com/watch?v=abc12345678", Status: "new"})
	repo.addRequest(db.SummaryRequest{MediaItemID: media.ID, ChatID: 10, PromptHash: summarize.PromptHash("prompt"), PromptText: "prompt", Status: summaryRequestPendingTranscript})
	service := newSummaryService(repo, &fakeSummaryLLM{}, &fakeSummarySender{}, &fakeSubtitleDownloader{})

	if err := service.ProcessRecoverableSummaryRequests(context.Background()); err != nil {
		t.Fatalf("ProcessRecoverableSummaryRequests returned error: %v", err)
	}
	job := repo.jobs[media.ID]
	if job.Status != mediaStatusQueued {
		t.Fatalf("job = %#v", job)
	}
	storedMedia, err := repo.GetMedia(context.Background(), media.ID)
	if err != nil {
		t.Fatalf("GetMedia returned error: %v", err)
	}
	if storedMedia.Status != mediaStatusQueued {
		t.Fatalf("media = %#v", storedMedia)
	}
}

func TestSummaryServiceProcessesPendingRequestsForMedia(t *testing.T) {
	repo := newSummaryRepo()
	media := repo.addMedia(db.MediaItem{Provider: "youtube", ProviderMediaID: "abc12345678", CanonicalURL: "https://www.youtube.com/watch?v=abc12345678", Status: mediaStatusTranscriptReady, TranscriptText: "whisper transcript"})
	prompt := "prompt"
	request := repo.addRequest(db.SummaryRequest{MediaItemID: media.ID, ChatID: 10, PromptHash: summarize.PromptHash(prompt), PromptText: prompt, Status: summaryRequestPendingTranscript})
	summarizer := &fakeSummaryLLM{responses: []string{"summary"}}
	sender := &fakeSummarySender{}
	service := newSummaryService(repo, summarizer, sender, &fakeSubtitleDownloader{})

	err := service.ProcessPendingRequests(context.Background(), media.ID)
	if err != nil {
		t.Fatalf("ProcessPendingRequests returned error: %v", err)
	}
	if repo.requests[request.ID].Status != "sent" {
		t.Fatalf("request = %#v", repo.requests[request.ID])
	}
	if len(summarizer.calls) != 1 || summarizer.calls[0].transcript != "whisper transcript" {
		t.Fatalf("summarizer calls = %#v", summarizer.calls)
	}
	if len(sender.messages) != 1 || sender.messages[0] != "summary" {
		t.Fatalf("messages = %#v", sender.messages)
	}
}

type summaryRepo struct {
	nextMediaID      int64
	nextJobID        int64
	nextRequestID    int64
	nextCacheID      int64
	nextMessageID    int64
	media            map[string]db.MediaItem
	jobs             map[int64]db.TranscriptionJob
	requests         map[int64]db.SummaryRequest
	caches           map[string]db.SummaryCache
	messages         []db.SummaryRequestMessage
	transitions      map[int64][]string
	insertErr        error
	cacheErr         error
	markSentErr      error
	cacheOnInsertErr db.SummaryCache
	updateRaceSource string
	updateRaceText   string
}

func newSummaryRepo() *summaryRepo {
	return &summaryRepo{
		nextMediaID:   1,
		nextJobID:     1,
		nextRequestID: 1,
		nextCacheID:   1,
		nextMessageID: 1,
		media:         map[string]db.MediaItem{},
		jobs:          map[int64]db.TranscriptionJob{},
		requests:      map[int64]db.SummaryRequest{},
		caches:        map[string]db.SummaryCache{},
		messages:      []db.SummaryRequestMessage{},
		transitions:   map[int64][]string{},
	}
}

func (r *summaryRepo) addMedia(media db.MediaItem) db.MediaItem {
	media.ID = r.nextMediaID
	r.nextMediaID++
	if media.Status == "" {
		media.Status = "new"
	}
	r.media[summaryMediaKey(media.Provider, media.ProviderMediaID)] = media
	return media
}

func (r *summaryRepo) addJob(job db.TranscriptionJob) db.TranscriptionJob {
	job.ID = r.nextJobID
	r.nextJobID++
	r.jobs[job.MediaItemID] = job
	return job
}

func (r *summaryRepo) addRequest(request db.SummaryRequest) db.SummaryRequest {
	request.ID = r.nextRequestID
	r.nextRequestID++
	if request.UpdatedAt == "" {
		request.UpdatedAt = time.Now().Format(time.DateTime)
	}
	r.requests[request.ID] = request
	return request
}

func (r *summaryRepo) addCache(cache db.SummaryCache) db.SummaryCache {
	cache.ID = r.nextCacheID
	r.nextCacheID++
	r.caches[cacheKey(cache.MediaItemID, cache.PromptHash, cache.Model)] = cache
	return cache
}

func (r *summaryRepo) CreateOrLoadMedia(_ context.Context, providerName, providerMediaID, canonicalURL string) (db.MediaItem, bool, error) {
	key := summaryMediaKey(providerName, providerMediaID)
	if media, ok := r.media[key]; ok {
		return media, false, nil
	}
	return r.addMedia(db.MediaItem{Provider: providerName, ProviderMediaID: providerMediaID, CanonicalURL: canonicalURL, Status: "new"}), true, nil
}

func (r *summaryRepo) BackfillMediaTitle(_ context.Context, mediaItemID int64, title string) (db.MediaItem, error) {
	for key, media := range r.media {
		if media.ID == mediaItemID {
			if strings.TrimSpace(media.Title) == "" {
				media.Title = strings.TrimSpace(title)
				r.media[key] = media
			}
			return media, nil
		}
	}
	return db.MediaItem{}, db.ErrNotFound
}

func (r *summaryRepo) FindSummaryDisplayMetadata(_ context.Context, mediaItemID int64) (display.SummaryMetadata, bool, error) {
	return display.SummaryMetadata{}, false, nil
}

func (r *summaryRepo) UpdateMediaTranscript(_ context.Context, mediaItemID int64, source, transcriptText string) error {
	for key, media := range r.media {
		if media.ID == mediaItemID {
			if r.updateRaceText != "" {
				media.TranscriptSource = r.updateRaceSource
				media.TranscriptText = r.updateRaceText
				r.updateRaceSource = ""
				r.updateRaceText = ""
			}
			if strings.TrimSpace(media.TranscriptText) == "" {
				media.TranscriptSource = source
				media.TranscriptText = transcriptText
			}
			media.Status = mediaStatusTranscriptReady
			r.media[key] = media
			return nil
		}
	}
	return nil
}

func (r *summaryRepo) CreateOrLoadActiveTranscriptionJob(_ context.Context, mediaItemID int64) (db.TranscriptionJob, bool, error) {
	if job, ok := r.jobs[mediaItemID]; ok && hasActiveTranscriptionStatus(job.Status) {
		return job, false, nil
	}
	for key, media := range r.media {
		if media.ID != mediaItemID {
			continue
		}
		if strings.TrimSpace(media.TranscriptText) != "" {
			return db.TranscriptionJob{}, false, nil
		}
		media.Status = mediaStatusQueued
		r.media[key] = media
		return r.addJob(db.TranscriptionJob{MediaItemID: mediaItemID, Status: mediaStatusQueued}), true, nil
	}
	return db.TranscriptionJob{}, false, db.ErrNotFound
}

func (r *summaryRepo) CreateSummaryRequest(_ context.Context, request db.SummaryRequest) (db.SummaryRequest, error) {
	for _, existingRequest := range r.requests {
		if request.MessageID != 0 && existingRequest.ChatID == request.ChatID && existingRequest.MessageID == request.MessageID {
			return existingRequest, nil
		}
		if request.MessageID == 0 && existingRequest.MessageID == 0 && existingRequest.Status != "failed" && existingRequest.MediaItemID == request.MediaItemID && existingRequest.ChatID == request.ChatID && existingRequest.PromptHash == request.PromptHash {
			return existingRequest, nil
		}
	}
	return r.addRequest(request), nil
}

func (r *summaryRepo) GetSummaryRequest(_ context.Context, requestID int64) (db.SummaryRequest, error) {
	request, ok := r.requests[requestID]
	if !ok {
		return db.SummaryRequest{}, db.ErrNotFound
	}
	return request, nil
}

func (r *summaryRepo) CreateSummaryRequestMessage(_ context.Context, message db.SummaryRequestMessage) (db.SummaryRequestMessage, error) {
	message.ID = r.nextMessageID
	r.nextMessageID++
	r.messages = append(r.messages, message)
	return message, nil
}

func (r *summaryRepo) ListActiveSummaryRequestMessagesByKind(_ context.Context, requestID int64, kinds []string) ([]db.SummaryRequestMessage, error) {
	allowed := map[string]bool{}
	for _, kind := range kinds {
		allowed[kind] = true
	}
	var messages []db.SummaryRequestMessage
	for _, message := range r.messages {
		if message.SummaryRequestID == requestID && message.DeletedAt == "" && allowed[message.Kind] {
			messages = append(messages, message)
		}
	}
	return messages, nil
}

func (r *summaryRepo) FindSummaryCache(_ context.Context, mediaItemID int64, promptHash, model string) (db.SummaryCache, bool, error) {
	if r.cacheErr != nil {
		return db.SummaryCache{}, false, r.cacheErr
	}
	cache, ok := r.caches[cacheKey(mediaItemID, promptHash, model)]
	return cache, ok, nil
}

func (r *summaryRepo) InsertSummaryCache(_ context.Context, cache db.SummaryCache) (db.SummaryCache, error) {
	if r.insertErr != nil {
		if r.cacheOnInsertErr.MediaItemID != 0 {
			r.addCache(r.cacheOnInsertErr)
		}
		return db.SummaryCache{}, r.insertErr
	}
	return r.addCache(cache), nil
}

func (r *summaryRepo) MarkSummaryRequestPendingSummary(_ context.Context, requestID int64) error {
	return r.updateSummaryRequestStatus(requestID, summaryRequestPendingTranscript, summaryRequestPendingSummary, nil)
}

func (r *summaryRepo) MarkSummaryRequestSummarizing(_ context.Context, requestID int64) error {
	return r.updateSummaryRequestStatus(requestID, summaryRequestPendingSummary, summaryRequestSummarizing, nil)
}

func (r *summaryRepo) MarkSummaryRequestSending(_ context.Context, requestID int64) error {
	return r.updateSummaryRequestStatus(requestID, summaryRequestSummarizing, summaryRequestSending, nil)
}

func (r *summaryRepo) MarkSummaryRequestSent(_ context.Context, requestID, summaryCacheID int64) error {
	if r.markSentErr != nil {
		return r.markSentErr
	}
	return r.updateSummaryRequestStatus(requestID, summaryRequestSending, "sent", func(request *db.SummaryRequest) {
		request.SummaryCacheID = summaryCacheID
	})
}

func (r *summaryRepo) UpdateSummaryRequestCache(_ context.Context, requestID, summaryCacheID int64) error {
	request, ok := r.requests[requestID]
	if !ok || request.Status != "sent" {
		return db.ErrNotFound
	}
	request.SummaryCacheID = summaryCacheID
	request.Error = ""
	r.requests[requestID] = request
	return nil
}

func (r *summaryRepo) MarkSummaryRequestRetryPending(_ context.Context, requestID int64, message string) error {
	request := r.requests[requestID]
	if request.Status != summaryRequestSummarizing && request.Status != summaryRequestSending {
		return db.ErrNotFound
	}
	request.Status = summaryRequestPendingSummary
	request.Error = message
	r.requests[requestID] = request
	r.transitions[requestID] = append(r.transitions[requestID], request.Status)
	return nil
}

func (r *summaryRepo) MarkSummaryRequestDeliveryUnknown(_ context.Context, requestID int64, message string) error {
	return r.updateSummaryRequestStatus(requestID, summaryRequestSending, summaryRequestDeliveryUnknown, func(request *db.SummaryRequest) {
		request.Error = message
	})
}

func (r *summaryRepo) MarkSummaryRequestFailed(_ context.Context, requestID int64, message string) error {
	return r.updateSummaryRequestStatus(requestID, summaryRequestSending, "failed", func(request *db.SummaryRequest) {
		request.Error = message
	})
}

func (r *summaryRepo) updateSummaryRequestStatus(requestID int64, from, to string, update func(*db.SummaryRequest)) error {
	request := r.requests[requestID]
	if request.Status != from {
		return db.ErrNotFound
	}
	request.Status = to
	if update != nil {
		update(&request)
	}
	r.requests[requestID] = request
	r.transitions[requestID] = append(r.transitions[requestID], request.Status)
	return nil
}

func (r *summaryRepo) GetMedia(_ context.Context, mediaItemID int64) (db.MediaItem, error) {
	for _, media := range r.media {
		if media.ID == mediaItemID {
			return media, nil
		}
	}
	return db.MediaItem{}, db.ErrNotFound
}

func (r *summaryRepo) ListPendingRequestsForMedia(_ context.Context, mediaItemID int64) ([]db.SummaryRequest, error) {
	var requests []db.SummaryRequest
	for _, request := range r.requests {
		if request.MediaItemID == mediaItemID && (request.Status == summaryRequestPendingTranscript || request.Status == summaryRequestPendingSummary) {
			requests = append(requests, request)
		}
	}
	return requests, nil
}

func (r *summaryRepo) RequeueInterruptedSummaryRequests(ctx context.Context) (int64, error) {
	return r.requeueSummaryRequests(ctx, time.Time{})
}

func (r *summaryRepo) RequeueStaleSummaryRequests(ctx context.Context, before time.Time) (int64, error) {
	return r.requeueSummaryRequests(ctx, before)
}

func (r *summaryRepo) EnqueuePendingTranscriptionRequests(_ context.Context) (int64, error) {
	activeJobs := map[int64]bool{}
	for mediaID, job := range r.jobs {
		if hasActiveTranscriptionStatus(job.Status) {
			activeJobs[mediaID] = true
		}
	}
	var count int64
	for _, request := range r.requests {
		if request.Status != summaryRequestPendingTranscript || activeJobs[request.MediaItemID] {
			continue
		}
		for key, media := range r.media {
			if media.ID != request.MediaItemID || strings.TrimSpace(media.TranscriptText) != "" {
				continue
			}
			media.Status = mediaStatusQueued
			r.media[key] = media
			r.addJob(db.TranscriptionJob{MediaItemID: media.ID, Status: mediaStatusQueued})
			activeJobs[media.ID] = true
			count++
		}
	}
	return count, nil
}

func (r *summaryRepo) requeueSummaryRequests(_ context.Context, before time.Time) (int64, error) {
	updatedBefore := before.Format(time.DateTime)
	var count int64
	for id, request := range r.requests {
		if !before.IsZero() && request.UpdatedAt >= updatedBefore {
			continue
		}
		switch request.Status {
		case summaryRequestSummarizing:
			request.Status = summaryRequestPendingSummary
			r.requests[id] = request
			r.transitions[id] = append(r.transitions[id], request.Status)
			count++
		case summaryRequestSending:
			request.Status = summaryRequestDeliveryUnknown
			request.Error = "delivery status unknown after interruption"
			r.requests[id] = request
			r.transitions[id] = append(r.transitions[id], request.Status)
			count++
		}
	}
	return count, nil
}

func (r *summaryRepo) ListMediaIDsWithPendingSummaryRequests(_ context.Context) ([]int64, error) {
	readyMedia := map[int64]bool{}
	for _, media := range r.media {
		if media.Status == mediaStatusTranscriptReady {
			readyMedia[media.ID] = true
		}
	}

	seen := map[int64]bool{}
	var mediaIDs []int64
	for _, request := range r.requests {
		if seen[request.MediaItemID] || !readyMedia[request.MediaItemID] || !isPendingSummaryRequest(request.Status) {
			continue
		}
		seen[request.MediaItemID] = true
		mediaIDs = append(mediaIDs, request.MediaItemID)
	}
	return mediaIDs, nil
}

func cacheKey(mediaItemID int64, promptHash, model string) string {
	return fmt.Sprintf("%d:%s:%s", mediaItemID, promptHash, model)
}

func summaryMediaKey(providerName, providerMediaID string) string {
	return providerName + ":" + providerMediaID
}

func newSummaryService(repo *summaryRepo, summarizer *fakeSummaryLLM, sender *fakeSummarySender, downloader *fakeSubtitleDownloader) SummaryService {
	return SummaryService{Repo: repo, Registry: fakeSummaryRegistry{}, SubtitleDownloader: downloader, Summarizer: summarizer, Sender: sender, Model: "model"}
}

func summaryCommand(prompt string) SummaryCommand {
	return SummaryCommand{ChatID: 10, UserID: 20, MessageID: 30, RawURL: "https://youtu.be/abc12345678", Prompt: prompt}
}

type fakeSummaryProgress struct {
	mediaProgress  []string
	finalSummaries []string
}

func (f *fakeSummaryProgress) MediaProgress(_ context.Context, _ int64, text string) error {
	f.mediaProgress = append(f.mediaProgress, text)
	return nil
}

func (f *fakeSummaryProgress) FinalSummary(_ context.Context, _ db.SummaryRequest, text string, _ ...any) error {
	f.finalSummaries = append(f.finalSummaries, text)
	return nil
}

type fakeSummaryRegistry struct {
	ref provider.MediaRef
}

func (r fakeSummaryRegistry) Find(_ string) (provider.Provider, provider.MediaRef, error) {
	if r.ref.Provider != "" {
		return nil, r.ref, nil
	}
	return nil, provider.MediaRef{Provider: "youtube", ProviderMediaID: "abc12345678", CanonicalURL: "https://www.youtube.com/watch?v=abc12345678"}, nil
}

type fakeSubtitleDownloader struct {
	result transcript.DownloadResult
	err    error
	urls   []string
}

func (f *fakeSubtitleDownloader) Download(_ context.Context, canonicalURL string) (transcript.DownloadResult, error) {
	f.urls = append(f.urls, canonicalURL)
	if f.err != nil {
		return transcript.DownloadResult{}, f.err
	}
	return f.result, nil
}

type fakeSummaryLLM struct {
	calls             []summaryCall
	responses         []string
	responsesByPrompt map[string]string
	errors            map[string]error
}

type summaryCall struct {
	prompt     string
	transcript string
}

func (f *fakeSummaryLLM) Summarize(_ context.Context, prompt, transcript string) (string, error) {
	f.calls = append(f.calls, summaryCall{prompt: prompt, transcript: transcript})
	if err := f.errors[prompt]; err != nil {
		return "", err
	}
	if response, ok := f.responsesByPrompt[prompt]; ok {
		return response, nil
	}
	if len(f.responses) == 0 {
		return "summary", nil
	}
	response := f.responses[0]
	f.responses = f.responses[1:]
	return response, nil
}

type fakeSummarySender struct {
	errors    map[int64]error
	ambiguous bool
	messages  []string
}

type fakeSummarySendError struct {
	ambiguous bool
}

func (e fakeSummarySendError) Error() string {
	return "telegram send failed"
}

func (e fakeSummarySendError) AmbiguousDelivery() bool {
	return e.ambiguous
}

func (f *fakeSummarySender) SendText(_ context.Context, chatID int64, text string) error {
	if err := f.errors[chatID]; err != nil {
		if f.ambiguous {
			return fakeSummarySendError{ambiguous: true}
		}
		return err
	}
	f.messages = append(f.messages, text)
	return nil
}

type fakeFinalSummarySender struct {
	fakeSummarySender
	finalErrors       map[int64]error
	finalChatIDs      []int64
	finalSummaries    []string
	finalReplyTargets []int64
	metadata          []display.SummaryMetadata
}

func (f *fakeFinalSummarySender) SendFinalSummary(_ context.Context, chatID int64, replyToMessageID int64, text string, _ ...any) (int64, error) {
	if err := f.finalErrors[chatID]; err != nil {
		return 0, err
	}
	f.finalChatIDs = append(f.finalChatIDs, chatID)
	f.finalSummaries = append(f.finalSummaries, text)
	f.finalReplyTargets = append(f.finalReplyTargets, replyToMessageID)
	return int64(len(f.finalSummaries)), nil
}

func (f *fakeFinalSummarySender) SendFinalSummaryPartsWithMetadata(_ context.Context, chatID int64, replyToMessageID int64, text string, _ int64, metadata display.SummaryMetadata, _ ...any) ([]int64, error) {
	if err := f.finalErrors[chatID]; err != nil {
		return nil, err
	}
	f.finalChatIDs = append(f.finalChatIDs, chatID)
	f.finalSummaries = append(f.finalSummaries, text)
	f.finalReplyTargets = append(f.finalReplyTargets, replyToMessageID)
	f.metadata = append(f.metadata, metadata)
	return []int64{int64(len(f.finalSummaries)), int64(len(f.finalSummaries) + 100)}, nil
}

type broadcastCall struct {
	chatID   int64
	text     string
	metadata display.SummaryMetadata
}

type fakeSummaryBroadcaster struct {
	calls []broadcastCall
	err   error
}

func (f *fakeSummaryBroadcaster) BroadcastFinalSummary(_ context.Context, chatID int64, text string, metadata display.SummaryMetadata, _ ...any) error {
	f.calls = append(f.calls, broadcastCall{chatID: chatID, text: text, metadata: metadata})
	return f.err
}

type fakeSummaryEditor struct {
	fakeSummarySender
	edits []fakeSummaryEdit
}

type fakeSummaryEdit struct {
	messageIDs []int64
	text       string
	requestID  int64
}

func (f *fakeSummaryEditor) EditFinalSummaryParts(_ context.Context, _ int64, messageIDs []int64, text string, requestID int64, _ ...any) error {
	f.edits = append(f.edits, fakeSummaryEdit{messageIDs: append([]int64(nil), messageIDs...), text: text, requestID: requestID})
	return nil
}
