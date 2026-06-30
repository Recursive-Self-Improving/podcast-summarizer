package queue

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/commandrunner"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/db"
)

const DefaultMaxAttempts = 2

var processingStatuses = [...]string{"converting_audio", "splitting_audio", "transcribing"}

type Repository interface {
	ClaimOldestQueuedTranscriptionJob(ctx context.Context) (db.TranscriptionJob, bool, error)
	UpdateTranscriptionJobAndMediaStatus(ctx context.Context, jobID, mediaItemID int64, status string) error
	CompleteTranscriptionJobWithTranscript(ctx context.Context, jobID, mediaItemID int64, source, transcriptText string) error
	MarkTranscriptionJobRetryQueued(ctx context.Context, jobID, mediaItemID int64, lastError string) error
	MarkTranscriptionJobFailedAndMediaFailed(ctx context.Context, jobID, mediaItemID int64, lastError string) (bool, []db.SummaryRequest, error)
}

type Processor interface {
	Process(ctx context.Context, job db.TranscriptionJob) (Transcript, error)
}

type Notifier interface {
	SendText(ctx context.Context, chatID int64, text string) error
}

type ProgressReporter interface {
	MediaProgress(ctx context.Context, mediaItemID int64, text string) error
	FinalFailureText(ctx context.Context, request db.SummaryRequest, text string) error
}

type RequestProcessor interface {
	ProcessPendingRequests(ctx context.Context, mediaItemID int64) error
}

type Transcript struct {
	Source string
	Text   string
}

type Worker struct {
	Repo             Repository
	Processor        Processor
	Notifier         Notifier
	Progress         ProgressReporter
	RequestProcessor RequestProcessor
	IdleWait         time.Duration
	MaxAttempts      int
	Logger           *slog.Logger
}

func (w Worker) Run(ctx context.Context) error {
	if err := w.validate(); err != nil {
		return err
	}
	idleWait := w.IdleWait
	if idleWait <= 0 {
		idleWait = time.Second
	}
	for {
		didWork, err := w.RunOnce(ctx)
		if err != nil {
			return err
		}
		if didWork {
			continue
		}
		timer := time.NewTimer(idleWait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (w Worker) RunOnce(ctx context.Context) (bool, error) {
	if err := w.validate(); err != nil {
		return false, err
	}
	job, found, err := w.Repo.ClaimOldestQueuedTranscriptionJob(ctx)
	if err != nil || !found {
		return false, err
	}
	w.logger().Info("transcription job claimed", "job_id", job.ID, "media_id", job.MediaItemID, "attempt", job.AttemptCount+1)
	w.notifyProgress(ctx, job.MediaItemID, "Downloading audio...")
	for _, status := range processingStatuses {
		w.logger().Info("queue transition", "job_id", job.ID, "media_id", job.MediaItemID, "status", status)
		if err := w.Repo.UpdateTranscriptionJobAndMediaStatus(ctx, job.ID, job.MediaItemID, status); err != nil {
			if isWorkerCancellation(ctx, err) {
				return true, w.Repo.UpdateTranscriptionJobAndMediaStatus(context.WithoutCancel(ctx), job.ID, job.MediaItemID, "queued")
			}
			return true, err
		}
		w.notifyProgress(ctx, job.MediaItemID, transcriptionProgressText(status))
	}
	transcript, err := w.Processor.Process(ctx, job)
	if err != nil {
		if isWorkerCancellation(ctx, err) {
			return true, w.Repo.UpdateTranscriptionJobAndMediaStatus(context.WithoutCancel(ctx), job.ID, job.MediaItemID, "queued")
		}
		w.logger().Error("transcription failed", "job_id", job.ID, "media_id", job.MediaItemID, "attempt", job.AttemptCount+1, "error", err)
		failureCtx := ctx
		if ctx.Err() != nil {
			failureCtx = context.WithoutCancel(ctx)
		}
		failureErr := w.handleFailure(failureCtx, job, err)
		if commandrunner.HasCleanupFailure(err) {
			return true, errors.Join(err, failureErr)
		}
		return true, failureErr
	}
	if err := w.completeTranscript(ctx, job, transcript); err != nil {
		return true, err
	}
	if err := ctx.Err(); err != nil {
		return true, err
	}
	w.processPendingRequests(ctx, job.MediaItemID)
	return true, nil
}

func (w Worker) completeTranscript(ctx context.Context, job db.TranscriptionJob, transcript Transcript) error {
	err := w.Repo.CompleteTranscriptionJobWithTranscript(ctx, job.ID, job.MediaItemID, transcript.Source, transcript.Text)
	if err != nil {
		if !isWorkerCancellation(ctx, err) {
			return err
		}
		if err := w.Repo.CompleteTranscriptionJobWithTranscript(context.WithoutCancel(ctx), job.ID, job.MediaItemID, transcript.Source, transcript.Text); err != nil {
			return err
		}
	}
	w.logger().Info("transcription completed", "job_id", job.ID, "media_id", job.MediaItemID, "source", transcript.Source)
	w.notifyProgress(ctx, job.MediaItemID, "Transcript ready. Preparing summary...")
	return nil
}

func (w Worker) processPendingRequests(ctx context.Context, mediaItemID int64) {
	w.logger().Info("processing pending summary requests", "media_id", mediaItemID)
	if err := w.RequestProcessor.ProcessPendingRequests(ctx, mediaItemID); err != nil {
		w.logger().Error("pending summary request processing failed", "media_id", mediaItemID, "error", err)
	}
}

func (w Worker) handleFailure(ctx context.Context, job db.TranscriptionJob, cause error) error {
	message := shortError(cause)
	attempt := job.AttemptCount + 1
	maxAttempts := w.maxAttempts()
	if attempt >= maxAttempts {
		w.logger().Error("transcription job failed", "job_id", job.ID, "media_id", job.MediaItemID, "attempt", attempt, "max_attempts", maxAttempts, "error", cause)
		failed, requests, err := w.Repo.MarkTranscriptionJobFailedAndMediaFailed(ctx, job.ID, job.MediaItemID, message)
		if err != nil {
			return err
		}
		if !failed {
			w.processPendingRequests(ctx, job.MediaItemID)
			return nil
		}
		w.notifyTranscriptionFailed(ctx, requests)
		return nil
	}
	w.logger().Warn("transcription job retry queued", "job_id", job.ID, "media_id", job.MediaItemID, "attempt", attempt, "max_attempts", maxAttempts, "error", cause)
	return w.Repo.MarkTranscriptionJobRetryQueued(ctx, job.ID, job.MediaItemID, message)
}

func (w Worker) notifyTranscriptionFailed(ctx context.Context, requests []db.SummaryRequest) {
	text := "Transcription failed. Please try again later."
	for _, request := range requests {
		var err error
		if w.Progress != nil {
			err = w.Progress.FinalFailureText(ctx, request, text)
		} else if w.Notifier != nil {
			err = w.Notifier.SendText(ctx, request.ChatID, text)
		}
		if err != nil {
			w.logger().Warn("telegram send failed", "request_id", request.ID, "media_id", request.MediaItemID, "chat_id", request.ChatID, "error", err)
		}
	}
}

func (w Worker) notifyProgress(ctx context.Context, mediaItemID int64, text string) {
	if w.Progress == nil {
		return
	}
	if err := w.Progress.MediaProgress(ctx, mediaItemID, text); err != nil {
		w.logger().Warn("progress send failed", "media_id", mediaItemID, "error", err)
	}
}

func transcriptionProgressText(status string) string {
	switch status {
	case "converting_audio":
		return "Preparing audio for transcription..."
	case "splitting_audio":
		return "Splitting audio for transcription..."
	case "transcribing":
		return "Transcribing audio..."
	default:
		return "Processing transcript..."
	}
}

func isWorkerCancellation(ctx context.Context, err error) bool {
	return ctx.Err() != nil && !commandrunner.HasCleanupFailure(err)
}

func (w Worker) validate() error {
	if w.Repo == nil {
		return errors.New("queue repository is required")
	}
	if w.Processor == nil {
		return errors.New("queue processor is required")
	}
	if w.RequestProcessor == nil {
		return errors.New("request processor is required")
	}
	return nil
}

func (w Worker) maxAttempts() int {
	if w.MaxAttempts > 0 {
		return w.MaxAttempts
	}
	return DefaultMaxAttempts
}

func (w Worker) logger() *slog.Logger {
	if w.Logger != nil {
		return w.Logger
	}
	return slog.Default()
}

func shortError(err error) string {
	message := err.Error()
	if len(message) > 200 {
		return message[:200]
	}
	return message
}
