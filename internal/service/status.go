package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/auth"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/db"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/provider"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/summarize"
)

type StatusAuthorizer interface {
	CanUse(ctx context.Context, chatID, userID int64, chatType auth.ChatType) (bool, error)
}

type StatusProviderRegistry interface {
	Find(rawURL string) (provider.Provider, provider.MediaRef, error)
}

type StatusRepository interface {
	FindMedia(ctx context.Context, provider, providerMediaID string) (db.MediaItem, bool, error)
	FindLatestTranscriptionJob(ctx context.Context, mediaItemID int64) (db.TranscriptionJob, bool, error)
	FindSummaryCache(ctx context.Context, mediaItemID int64, promptHash, model string) (db.SummaryCache, bool, error)
}

type StatusService struct {
	Auth     StatusAuthorizer
	Registry StatusProviderRegistry
	Repo     StatusRepository
	Model    string
	Logger   *slog.Logger
}

type StatusQuery struct {
	ChatID   int64
	UserID   int64
	ChatType auth.ChatType
	RawURL   string
	Prompt   string
}

type StatusReport struct {
	Authorized      bool
	Found           bool
	InvalidURL      bool
	Media           db.MediaItem
	Job             db.TranscriptionJob
	HasJob          bool
	HasTranscript   bool
	HasSummaryCache bool
	Text            string
}

func (s StatusService) Status(ctx context.Context, query StatusQuery) (StatusReport, error) {
	if s.Auth == nil {
		return StatusReport{}, errors.New("status authorizer is required")
	}
	if s.Registry == nil {
		return StatusReport{}, errors.New("status provider registry is required")
	}
	if s.Repo == nil {
		return StatusReport{}, errors.New("status repository is required")
	}

	allowed, err := s.Auth.CanUse(ctx, query.ChatID, query.UserID, query.ChatType)
	if err != nil {
		return StatusReport{}, err
	}
	if !allowed {
		s.logger().Warn("auth rejected", "command", "status", "chat_id", query.ChatID, "chat_type", query.ChatType, "user_id", query.UserID)
		return StatusReport{Text: UnauthorizedNotification().Text}, nil
	}

	_, ref, err := s.Registry.Find(query.RawURL)
	if err != nil {
		return StatusReport{Authorized: true, InvalidURL: true, Text: InvalidURLNotification().Text}, nil
	}
	s.logger().Info("provider normalized", "command", "status", "provider", ref.Provider, "provider_media_id", ref.ProviderMediaID)

	media, found, err := s.Repo.FindMedia(ctx, ref.Provider, ref.ProviderMediaID)
	if err != nil {
		return StatusReport{}, err
	}
	if !found {
		return StatusReport{Authorized: true, Text: fmt.Sprintf("No status found for %s.", ref.CanonicalURL)}, nil
	}

	job, hasJob, err := s.Repo.FindLatestTranscriptionJob(ctx, media.ID)
	if err != nil {
		return StatusReport{}, err
	}
	prompt := summarize.ResolvePrompt(query.Prompt)
	_, hasSummaryCache, err := s.Repo.FindSummaryCache(ctx, media.ID, summarize.PromptHash(prompt), s.Model)
	if err != nil {
		return StatusReport{}, err
	}
	report := StatusReport{
		Authorized:      true,
		Found:           true,
		Media:           media,
		Job:             job,
		HasJob:          hasJob,
		HasTranscript:   strings.TrimSpace(media.TranscriptText) != "",
		HasSummaryCache: hasSummaryCache,
	}
	report.Text = formatStatusReport(report)
	return report, nil
}

func formatStatusReport(report StatusReport) string {
	jobStatus := "none"
	if report.HasJob {
		jobStatus = report.Job.Status
	}
	return fmt.Sprintf("Provider: %s\nMedia ID: %s\nMedia status: %s\nJob status: %s\nTranscript available: %s\nSummary cached: %s", report.Media.Provider, report.Media.ProviderMediaID, report.Media.Status, jobStatus, yesNo(report.HasTranscript), yesNo(report.HasSummaryCache))
}

func (s StatusService) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
