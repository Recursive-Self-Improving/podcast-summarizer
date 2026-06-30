package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/db"
)

type NotificationCategory string

const (
	NotificationUnauthorized             NotificationCategory = "unauthorized"
	NotificationInvalidURL               NotificationCategory = "invalid_url"
	NotificationTranscriptProcessing     NotificationCategory = "transcript_processing"
	NotificationTranscriptionQueued      NotificationCategory = "transcription_queued"
	NotificationTranscriptionDownloading NotificationCategory = "transcription_downloading"
	NotificationTranscriptionConverting  NotificationCategory = "transcription_converting"
	NotificationTranscriptionSplitting   NotificationCategory = "transcription_splitting"
	NotificationTranscribing             NotificationCategory = "transcribing"
	NotificationSubtitleSummarizing      NotificationCategory = "subtitle_summarizing"
	NotificationSummarySending           NotificationCategory = "summary_sending"
	NotificationSummaryComplete          NotificationCategory = "summary_complete"
	NotificationTranscriptionFailed      NotificationCategory = "transcription_failed"
	NotificationSummarizationFailed      NotificationCategory = "summarization_failed"
)

type Notification struct {
	Category NotificationCategory
	Text     string
}

type Notifier struct {
	Sender SummarySender
}

func (n Notifier) Send(ctx context.Context, chatID int64, notification Notification) error {
	if n.Sender == nil {
		return errors.New("notification sender is required")
	}
	return n.Sender.SendText(ctx, chatID, notification.Text)
}

func UnauthorizedNotification() Notification {
	return Notification{Category: NotificationUnauthorized, Text: "You are not authorized to use this bot."}
}

func InvalidURLNotification() Notification {
	return Notification{Category: NotificationInvalidURL, Text: "Please send a valid YouTube, Xiaoyuzhou, or SoundOn episode URL."}
}

func TranscriptProcessingNotification(media db.MediaItem) Notification {
	return Notification{Category: NotificationTranscriptProcessing, Text: fmt.Sprintf("This episode is already being transcribed: %s", media.CanonicalURL)}
}

func TranscriptionQueuedNotification(media db.MediaItem) Notification {
	return Notification{Category: NotificationTranscriptionQueued, Text: fmt.Sprintf("Transcription queued for: %s", media.CanonicalURL)}
}

func TranscriptionDownloadingNotification(media db.MediaItem) Notification {
	return Notification{Category: NotificationTranscriptionDownloading, Text: fmt.Sprintf("Downloading audio for: %s", media.CanonicalURL)}
}

func TranscriptionConvertingNotification(media db.MediaItem) Notification {
	return Notification{Category: NotificationTranscriptionConverting, Text: fmt.Sprintf("Preparing audio for transcription: %s", media.CanonicalURL)}
}

func TranscriptionSplittingNotification(media db.MediaItem) Notification {
	return Notification{Category: NotificationTranscriptionSplitting, Text: fmt.Sprintf("Splitting audio for transcription: %s", media.CanonicalURL)}
}

func TranscribingNotification(media db.MediaItem) Notification {
	return Notification{Category: NotificationTranscribing, Text: fmt.Sprintf("Transcribing audio for: %s", media.CanonicalURL)}
}

func SubtitleSummarizingNotification(media db.MediaItem) Notification {
	source := media.TranscriptSource
	if source == "" {
		source = "transcript"
	}
	return Notification{Category: NotificationSubtitleSummarizing, Text: fmt.Sprintf("Found %s transcript. Starting summary for: %s", source, media.CanonicalURL)}
}

func SummarySendingNotification() Notification {
	return Notification{Category: NotificationSummarySending, Text: "Summary ready. Sending..."}
}

func SummaryCompleteNotification(summaryText string) Notification {
	return Notification{Category: NotificationSummaryComplete, Text: summaryText}
}

func TranscriptionFailedNotification() Notification {
	return Notification{Category: NotificationTranscriptionFailed, Text: "Transcription failed. Please try again later."}
}

func SummarizationFailedNotification() Notification {
	return Notification{Category: NotificationSummarizationFailed, Text: "Summarization failed. Please try again later."}
}

func SummaryRequestNotification(media db.MediaItem, request db.SummaryRequest, cache db.SummaryCache) Notification {
	switch request.Status {
	case "pending_transcript":
		return TranscriptProcessingNotification(media)
	case "pending_summary", "summarizing":
		return SubtitleSummarizingNotification(media)
	case "sent":
		return SummaryCompleteNotification(cache.SummaryText)
	case "failed":
		if media.Status == "failed" && cache.ID == 0 {
			return TranscriptionFailedNotification()
		}
		return SummarizationFailedNotification()
	default:
		return Notification{Category: NotificationTranscriptProcessing, Text: fmt.Sprintf("Request is %s for: %s", request.Status, media.CanonicalURL)}
	}
}
