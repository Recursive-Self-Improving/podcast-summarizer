package service

import (
	"context"
	"strings"
	"testing"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/db"
)

func TestNotificationCategories(t *testing.T) {
	media := db.MediaItem{CanonicalURL: "https://www.youtube.com/watch?v=abc12345678", TranscriptSource: "manual_subtitle"}
	tests := []struct {
		name     string
		notify   Notification
		category NotificationCategory
		contains string
	}{
		{name: "unauthorized", notify: UnauthorizedNotification(), category: NotificationUnauthorized, contains: "not authorized"},
		{name: "invalid URL", notify: InvalidURLNotification(), category: NotificationInvalidURL, contains: "valid YouTube, Xiaoyuzhou, or SoundOn episode URL"},
		{name: "transcript processing", notify: TranscriptProcessingNotification(media), category: NotificationTranscriptProcessing, contains: "already being transcribed"},
		{name: "transcription queued", notify: TranscriptionQueuedNotification(media), category: NotificationTranscriptionQueued, contains: "Transcription queued"},
		{name: "subtitle summarizing", notify: SubtitleSummarizingNotification(media), category: NotificationSubtitleSummarizing, contains: "manual_subtitle"},
		{name: "summary complete", notify: SummaryCompleteNotification("summary text"), category: NotificationSummaryComplete, contains: "summary text"},
		{name: "transcription failed", notify: TranscriptionFailedNotification(), category: NotificationTranscriptionFailed, contains: "Transcription failed"},
		{name: "summarization failed", notify: SummarizationFailedNotification(), category: NotificationSummarizationFailed, contains: "Summarization failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.notify.Category != tt.category {
				t.Fatalf("category = %q, want %q", tt.notify.Category, tt.category)
			}
			if !strings.Contains(tt.notify.Text, tt.contains) {
				t.Fatalf("text = %q, want it to contain %q", tt.notify.Text, tt.contains)
			}
			if len(tt.notify.Text) > 250 {
				t.Fatalf("text too long: %d", len(tt.notify.Text))
			}
		})
	}
}

func TestSummaryRequestNotificationCategories(t *testing.T) {
	media := db.MediaItem{CanonicalURL: "https://www.youtube.com/watch?v=abc12345678", TranscriptSource: "auto_subtitle"}
	cache := db.SummaryCache{ID: 1, SummaryText: "cached summary"}
	tests := []struct {
		name    string
		media   db.MediaItem
		request db.SummaryRequest
		cache   db.SummaryCache
		want    NotificationCategory
	}{
		{name: "pending transcript", media: media, request: db.SummaryRequest{Status: "pending_transcript"}, cache: cache, want: NotificationTranscriptProcessing},
		{name: "pending summary", media: media, request: db.SummaryRequest{Status: "pending_summary"}, cache: cache, want: NotificationSubtitleSummarizing},
		{name: "summarizing", media: media, request: db.SummaryRequest{Status: "summarizing"}, cache: cache, want: NotificationSubtitleSummarizing},
		{name: "sent", media: media, request: db.SummaryRequest{Status: "sent"}, cache: cache, want: NotificationSummaryComplete},
		{name: "summarization failed", media: media, request: db.SummaryRequest{Status: "failed", Error: "model failed"}, cache: cache, want: NotificationSummarizationFailed},
		{name: "transcription failed", media: db.MediaItem{Status: "failed", StatusDetail: "whisper failed"}, request: db.SummaryRequest{Status: "failed"}, want: NotificationTranscriptionFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notification := SummaryRequestNotification(tt.media, tt.request, tt.cache)
			if notification.Category != tt.want {
				t.Fatalf("category = %q, want %q", notification.Category, tt.want)
			}
		})
	}
}

func TestNotifierSendsNotificationText(t *testing.T) {
	sender := &fakeSummarySender{}
	notifier := Notifier{Sender: sender}

	err := notifier.Send(context.Background(), 10, SummaryCompleteNotification("summary text"))
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if len(sender.messages) != 1 || sender.messages[0] != "summary text" {
		t.Fatalf("messages = %#v", sender.messages)
	}
}
