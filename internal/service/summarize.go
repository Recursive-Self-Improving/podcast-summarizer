package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/commandrunner"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/db"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/display"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/provider"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/summarize"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/transcript"
)

type SummaryRepository interface {
	CreateOrLoadMedia(ctx context.Context, provider, providerMediaID, canonicalURL string) (db.MediaItem, bool, error)
	BackfillMediaTitle(ctx context.Context, mediaItemID int64, title string) (db.MediaItem, error)
	FindSummaryDisplayMetadata(ctx context.Context, mediaItemID int64) (display.SummaryMetadata, bool, error)
	UpdateMediaTranscript(ctx context.Context, mediaItemID int64, source, transcriptText string) error
	CreateOrLoadActiveTranscriptionJob(ctx context.Context, mediaItemID int64) (db.TranscriptionJob, bool, error)
	CreateSummaryRequest(ctx context.Context, request db.SummaryRequest) (db.SummaryRequest, error)
	GetSummaryRequest(ctx context.Context, requestID int64) (db.SummaryRequest, error)
	FindSummaryCache(ctx context.Context, mediaItemID int64, promptHash, model string) (db.SummaryCache, bool, error)
	InsertSummaryCache(ctx context.Context, cache db.SummaryCache) (db.SummaryCache, error)
	CreateSummaryRequestMessage(ctx context.Context, message db.SummaryRequestMessage) (db.SummaryRequestMessage, error)
	ListActiveSummaryRequestMessagesByKind(ctx context.Context, requestID int64, kinds []string) ([]db.SummaryRequestMessage, error)
	UpdateSummaryRequestCache(ctx context.Context, requestID, summaryCacheID int64) error
	MarkSummaryRequestPendingSummary(ctx context.Context, requestID int64) error
	MarkSummaryRequestSummarizing(ctx context.Context, requestID int64) error
	MarkSummaryRequestSending(ctx context.Context, requestID int64) error
	MarkSummaryRequestSent(ctx context.Context, requestID, summaryCacheID int64) error
	MarkSummaryRequestRetryPending(ctx context.Context, requestID int64, message string) error
	MarkSummaryRequestDeliveryUnknown(ctx context.Context, requestID int64, message string) error
	MarkSummaryRequestFailed(ctx context.Context, requestID int64, message string) error
	GetMedia(ctx context.Context, mediaItemID int64) (db.MediaItem, error)
	ListPendingRequestsForMedia(ctx context.Context, mediaItemID int64) ([]db.SummaryRequest, error)
	RequeueInterruptedSummaryRequests(ctx context.Context) (int64, error)
	RequeueStaleSummaryRequests(ctx context.Context, before time.Time) (int64, error)
	EnqueuePendingTranscriptionRequests(ctx context.Context) (int64, error)
	ListMediaIDsWithPendingSummaryRequests(ctx context.Context) ([]int64, error)
}

type SummaryProviderRegistry interface {
	Find(rawURL string) (provider.Provider, provider.MediaRef, error)
}

type SubtitleDownloader interface {
	Download(ctx context.Context, canonicalURL string) (transcript.DownloadResult, error)
}

type SummarySender interface {
	SendText(ctx context.Context, chatID int64, text string) error
}

type ContextualSummarySender interface {
	SendTextWithAttrs(ctx context.Context, chatID int64, text string, attrs ...any) error
}

type ReplySummarySender interface {
	SendReplyText(ctx context.Context, chatID, replyToMessageID int64, text string) (int64, error)
}

type ContextualReplySummarySender interface {
	SendReplyTextWithAttrs(ctx context.Context, chatID, replyToMessageID int64, text string, attrs ...any) (int64, error)
}

type FinalSummarySender interface {
	SendFinalSummary(ctx context.Context, chatID, replyToMessageID int64, text string, attrs ...any) (int64, error)
}

type FinalSummaryPartsSender interface {
	SendFinalSummaryParts(ctx context.Context, chatID, replyToMessageID int64, text string, requestID int64, attrs ...any) ([]int64, error)
}

type FinalSummaryPartsWithMetadataSender interface {
	SendFinalSummaryPartsWithMetadata(ctx context.Context, chatID, replyToMessageID int64, text string, requestID int64, metadata display.SummaryMetadata, attrs ...any) ([]int64, error)
}

type SummaryBroadcaster interface {
	BroadcastFinalSummary(ctx context.Context, chatID int64, text string, metadata display.SummaryMetadata, sourceURL string, attrs ...any) error
}

type FinalSummaryEditor interface {
	EditFinalSummaryParts(ctx context.Context, chatID int64, messageIDs []int64, text string, requestID int64, attrs ...any) error
}

type FinalSummaryWithMetadataEditor interface {
	EditFinalSummaryPartsWithMetadata(ctx context.Context, chatID int64, messageIDs []int64, text string, requestID int64, metadata display.SummaryMetadata, attrs ...any) error
}

type SummaryProgressNotifier interface {
	MediaProgress(ctx context.Context, mediaItemID int64, text string) error
	FinalSummary(ctx context.Context, request db.SummaryRequest, summaryText string, attrs ...any) error
}

type SummaryProgressMetadataNotifier interface {
	FinalSummaryWithMetadata(ctx context.Context, request db.SummaryRequest, summaryText string, metadata display.SummaryMetadata, attrs ...any) error
}

type SummaryService struct {
	Repo                      SummaryRepository
	Registry                  SummaryProviderRegistry
	SubtitleDownloader        SubtitleDownloader
	Summarizer                summarize.Summarizer
	Sender                    SummarySender
	Progress                  SummaryProgressNotifier
	SummaryBroadcastChannelID int64
	SummaryBroadcaster        SummaryBroadcaster
	Model                     string
	DefaultSummaryVariant     summarize.SummaryVariant
	Logger                    *slog.Logger
}

type SummaryCommand struct {
	ChatID    int64
	UserID    int64
	MessageID int64
	RawURL    string
	Prompt    string
}

type SummaryCommandResult struct {
	Media                   db.MediaItem
	Request                 db.SummaryRequest
	Job                     db.TranscriptionJob
	CreatedMedia            bool
	CreatedTranscriptionJob bool
	UsedSubtitle            bool
	Summarized              bool
}

type WatchSummarySubscriber struct {
	ChatID int64
	UserID int64
}

type WatchSummaryCommand struct {
	Provider        string
	ProviderMediaID string
	CanonicalURL    string
	Subscribers     []WatchSummarySubscriber
	Prompt          string
	Metadata        display.SummaryMetadata
}

type WatchSummaryResult struct {
	Media                   db.MediaItem
	Requests                []db.SummaryRequest
	Job                     db.TranscriptionJob
	CreatedMedia            bool
	CreatedTranscriptionJob bool
	Summarized              bool
}

type SwitchSummaryVariantCommand struct {
	RequestID int64
	ChatID    int64
	UserID    int64
	MessageID int64
	Variant   summarize.SummaryVariant
}

type summaryRequestGroup struct {
	promptHash string
	promptText string
}

type summaryCacheResult struct {
	cache     db.SummaryCache
	generated bool
}

type durableSummaryDeliveryError struct {
	requestID int64
	cause     error
}

func (e durableSummaryDeliveryError) Error() string {
	return fmt.Sprintf("summary request %d delivery deferred: %v", e.requestID, e.cause)
}

func (e durableSummaryDeliveryError) Unwrap() error {
	return e.cause
}

const (
	mediaStatusQueued           = "queued"
	mediaStatusDownloadingAudio = "downloading_audio"
	mediaStatusConvertingAudio  = "converting_audio"
	mediaStatusSplittingAudio   = "splitting_audio"
	mediaStatusTranscribing     = "transcribing"
	mediaStatusTranscriptReady  = "transcript_ready"

	summaryRequestPendingTranscript = "pending_transcript"
	summaryRequestPendingSummary    = "pending_summary"
	summaryRequestSummarizing       = "summarizing"
	summaryRequestSending           = "sending"
	summaryRequestDeliveryUnknown   = "delivery_unknown"

	summaryRequestLeaseTimeout = 10 * time.Minute
)

func (s SummaryService) RequestSummary(ctx context.Context, command SummaryCommand) (SummaryCommandResult, error) {
	if s.Repo == nil {
		return SummaryCommandResult{}, errors.New("summary repository is required")
	}
	if s.Registry == nil {
		return SummaryCommandResult{}, errors.New("summary provider registry is required")
	}
	if s.SubtitleDownloader == nil {
		return SummaryCommandResult{}, errors.New("subtitle downloader is required")
	}

	_, ref, err := s.Registry.Find(command.RawURL)
	if err != nil {
		return SummaryCommandResult{}, err
	}
	s.logger().Info("provider normalized", "provider", ref.Provider, "provider_media_id", ref.ProviderMediaID)
	media, createdMedia, err := s.Repo.CreateOrLoadMedia(ctx, ref.Provider, ref.ProviderMediaID, ref.CanonicalURL)
	if err != nil {
		return SummaryCommandResult{}, err
	}

	request := s.newSummaryRequest(media.ID, command)
	if strings.TrimSpace(media.TranscriptText) != "" {
		request.Status = summaryRequestPendingSummary
		request, err = s.Repo.CreateSummaryRequest(ctx, request)
		if err != nil {
			return SummaryCommandResult{}, err
		}
		return s.summarizeTranscriptReadyMedia(ctx, media, request, createdMedia, false)
	}

	request.Status = summaryRequestPendingTranscript
	request, err = s.Repo.CreateSummaryRequest(ctx, request)
	if err != nil {
		return SummaryCommandResult{}, err
	}
	if !isPendingSummaryRequest(request.Status) {
		return SummaryCommandResult{Media: media, Request: request, CreatedMedia: createdMedia}, nil
	}

	if !hasActiveTranscriptionStatus(media.Status) && provider.SupportsSubtitleLookup(ref.Provider) {
		s.logger().Info("subtitle download started", "media_id", media.ID)
		downloader := s.SubtitleDownloader
		if withLogger, ok := downloader.(interface {
			WithLogger(*slog.Logger) transcript.Downloader
		}); ok {
			downloader = withLogger.WithLogger(s.logger().With("media_id", media.ID))
		}
		result, err := downloader.Download(ctx, media.CanonicalURL)
		if err == nil {
			s.logger().Info("subtitle download succeeded", "media_id", media.ID, "source", result.Source)
			if err := s.Repo.UpdateMediaTranscript(ctx, media.ID, result.Source, result.Text); err != nil {
				return SummaryCommandResult{}, err
			}
			media, err = s.Repo.GetMedia(ctx, media.ID)
			if err != nil {
				return SummaryCommandResult{}, err
			}
			return s.summarizeTranscriptReadyMedia(ctx, media, request, createdMedia, true)
		}
		if commandrunner.HasCleanupFailure(err) {
			return SummaryCommandResult{}, err
		}
		s.logger().Info("subtitle download failed", "media_id", media.ID, "error", err)
	}

	job, createdJob, err := s.Repo.CreateOrLoadActiveTranscriptionJob(ctx, media.ID)
	if err != nil {
		return SummaryCommandResult{}, err
	}
	if job.ID == 0 {
		media, err = s.Repo.GetMedia(ctx, media.ID)
		if err != nil {
			return SummaryCommandResult{}, err
		}
		return s.summarizeTranscriptReadyMedia(ctx, media, request, createdMedia, false)
	}
	s.logger().Info("transcription job queued", "media_id", media.ID, "job_id", job.ID, "created", createdJob)
	return SummaryCommandResult{Media: media, Request: request, Job: job, CreatedMedia: createdMedia, CreatedTranscriptionJob: createdJob}, nil
}

func (s SummaryService) RequestWatchSummary(ctx context.Context, command WatchSummaryCommand) (WatchSummaryResult, error) {
	if s.Repo == nil {
		return WatchSummaryResult{}, errors.New("summary repository is required")
	}
	if command.Provider == "" || command.ProviderMediaID == "" || command.CanonicalURL == "" {
		return WatchSummaryResult{}, errors.New("watch summary media ref is required")
	}
	if len(command.Subscribers) == 0 {
		return WatchSummaryResult{}, errors.New("watch summary subscribers are required")
	}

	media, createdMedia, err := s.Repo.CreateOrLoadMedia(ctx, command.Provider, command.ProviderMediaID, command.CanonicalURL)
	if err != nil {
		return WatchSummaryResult{}, err
	}
	if strings.TrimSpace(command.Metadata.EpisodeTitle) != "" && strings.TrimSpace(media.Title) == "" {
		if updated, err := s.Repo.BackfillMediaTitle(ctx, media.ID, command.Metadata.EpisodeTitle); err != nil {
			s.logger().Warn("media title backfill failed", "media_id", media.ID, "error", err)
		} else {
			media = updated
		}
	}
	result := WatchSummaryResult{Media: media, CreatedMedia: createdMedia}
	requests := make([]db.SummaryRequest, 0, len(command.Subscribers))
	status := summaryRequestPendingTranscript
	if strings.TrimSpace(media.TranscriptText) != "" {
		status = summaryRequestPendingSummary
	}
	for _, subscriber := range command.Subscribers {
		request := s.newSummaryRequest(media.ID, SummaryCommand{ChatID: subscriber.ChatID, UserID: subscriber.UserID, Prompt: command.Prompt})
		request.Status = status
		request, err = s.Repo.CreateSummaryRequest(ctx, request)
		if err != nil {
			result.Requests = requests
			return result, err
		}
		requests = append(requests, request)
	}
	result.Requests = requests
	if status == summaryRequestPendingSummary {
		if err := s.processRequestsWithMetadata(ctx, media, requests, command.Metadata); err != nil {
			return result, err
		}
		result.Summarized = true
		return result, nil
	}
	job, createdJob, err := s.Repo.CreateOrLoadActiveTranscriptionJob(ctx, media.ID)
	if err != nil {
		return result, err
	}
	result.Job = job
	result.CreatedTranscriptionJob = createdJob
	if job.ID == 0 {
		media, err = s.Repo.GetMedia(ctx, media.ID)
		if err != nil {
			return result, err
		}
		result.Media = media
		if err := s.processRequestsWithMetadata(ctx, media, requests, command.Metadata); err != nil {
			return result, err
		}
		result.Summarized = true
		return result, nil
	}
	return result, nil
}

func (s SummaryService) newSummaryRequest(mediaID int64, command SummaryCommand) db.SummaryRequest {
	prompt := s.defaultSummaryVariant().Prompt()
	if strings.TrimSpace(command.Prompt) != "" {
		prompt = summarize.ResolvePrompt(command.Prompt)
	}
	return db.SummaryRequest{
		MediaItemID: mediaID,
		ChatID:      command.ChatID,
		UserID:      command.UserID,
		MessageID:   command.MessageID,
		PromptHash:  summarize.PromptHash(prompt),
		PromptText:  prompt,
	}
}

func (s SummaryService) defaultSummaryVariant() summarize.SummaryVariant {
	if s.DefaultSummaryVariant.Code != "" {
		return s.DefaultSummaryVariant
	}
	return summarize.DefaultSummaryVariant()
}

func (s SummaryService) summarizeTranscriptReadyMedia(ctx context.Context, media db.MediaItem, request db.SummaryRequest, createdMedia, usedSubtitle bool) (SummaryCommandResult, error) {
	if isPendingSummaryRequest(request.Status) {
		if err := s.ProcessRequests(ctx, media, []db.SummaryRequest{request}); err != nil {
			return SummaryCommandResult{}, err
		}
	}
	return SummaryCommandResult{Media: media, Request: request, CreatedMedia: createdMedia, UsedSubtitle: usedSubtitle, Summarized: true}, nil
}

func IsDurableSummaryDeliveryError(err error) bool {
	var deliveryErr durableSummaryDeliveryError
	return errors.As(err, &deliveryErr)
}

func isPendingSummaryRequest(status string) bool {
	return status == summaryRequestPendingTranscript || status == summaryRequestPendingSummary
}

func hasActiveTranscriptionStatus(status string) bool {
	switch status {
	case mediaStatusQueued,
		mediaStatusDownloadingAudio,
		mediaStatusConvertingAudio,
		mediaStatusSplittingAudio,
		mediaStatusTranscribing:
		return true
	default:
		return false
	}
}

func (s SummaryService) ProcessPendingRequests(ctx context.Context, mediaItemID int64) error {
	if s.Repo == nil {
		return errors.New("summary repository is required")
	}
	media, err := s.Repo.GetMedia(ctx, mediaItemID)
	if err != nil {
		return err
	}
	requests, err := s.Repo.ListPendingRequestsForMedia(ctx, mediaItemID)
	if err != nil {
		return err
	}
	if len(requests) == 0 {
		return nil
	}
	return s.ProcessRequests(ctx, media, requests)
}

func (s SummaryService) RequeueInterruptedSummaryRequests(ctx context.Context) (int64, error) {
	if s.Repo == nil {
		return 0, errors.New("summary repository is required")
	}
	return s.Repo.RequeueInterruptedSummaryRequests(ctx)
}

func (s SummaryService) ProcessRecoverableSummaryRequests(ctx context.Context) error {
	if s.Repo == nil {
		return errors.New("summary repository is required")
	}
	if _, err := s.Repo.RequeueStaleSummaryRequests(ctx, time.Now().Add(-summaryRequestLeaseTimeout)); err != nil {
		return err
	}
	if _, err := s.Repo.EnqueuePendingTranscriptionRequests(ctx); err != nil {
		return err
	}
	mediaIDs, err := s.Repo.ListMediaIDsWithPendingSummaryRequests(ctx)
	if err != nil {
		return err
	}
	var processErr error
	for _, mediaID := range mediaIDs {
		processErr = errors.Join(processErr, s.ProcessPendingRequests(ctx, mediaID))
	}
	return processErr
}

func (s SummaryService) ProcessRequests(ctx context.Context, media db.MediaItem, requests []db.SummaryRequest) error {
	return s.processRequestsWithMetadata(ctx, media, requests, display.SummaryMetadata{})
}

func (s SummaryService) processRequestsWithMetadata(ctx context.Context, media db.MediaItem, requests []db.SummaryRequest, metadata display.SummaryMetadata) error {
	if s.Repo == nil {
		return errors.New("summary repository is required")
	}
	if s.Summarizer == nil {
		return errors.New("summarizer is required")
	}
	if s.Sender == nil {
		return errors.New("summary sender is required")
	}
	metadata = s.displayMetadata(ctx, media, metadata)

	groups, err := s.groupSummaryRequests(ctx, requests)
	if err != nil {
		return err
	}

	if s.Progress != nil && hasReplyRequests(requests) {
		if err := s.Progress.MediaProgress(ctx, media.ID, SubtitleSummarizingNotification(media).Text); err != nil {
			s.logger().Warn("summary progress failed", "media_id", media.ID, "error", err)
		}
	}

	var processErr error
	for group, groupedRequests := range groups {
		cacheResult, err := s.summaryCache(ctx, media, group, groupedRequests)
		if err != nil {
			processErr = errors.Join(processErr, err, s.markRequestsRetryPending(ctx, groupedRequests, err))
			continue
		}
		processErr = errors.Join(processErr, s.sendSummary(ctx, media, groupedRequests, cacheResult, metadata))
	}
	return processErr
}

func (s SummaryService) displayMetadata(ctx context.Context, media db.MediaItem, metadata display.SummaryMetadata) display.SummaryMetadata {
	stored, found, err := s.Repo.FindSummaryDisplayMetadata(ctx, media.ID)
	if err != nil {
		s.logger().Warn("summary metadata lookup failed", "media_id", media.ID, "error", err)
	} else if found {
		metadata = display.MergeSummaryMetadata(metadata, stored)
	}
	if strings.TrimSpace(metadata.EpisodeTitle) == "" {
		metadata.EpisodeTitle = media.Title
	}
	return metadata
}

func (s SummaryService) groupSummaryRequests(ctx context.Context, requests []db.SummaryRequest) (map[summaryRequestGroup][]db.SummaryRequest, error) {
	groups := make(map[summaryRequestGroup][]db.SummaryRequest)
	for _, request := range requests {
		claimed, err := s.claimSummaryRequest(ctx, request)
		if err != nil {
			return nil, err
		}
		if !claimed {
			continue
		}

		group := summaryRequestGroup{promptHash: request.PromptHash, promptText: request.PromptText}
		groups[group] = append(groups[group], request)
	}
	return groups, nil
}

func (s SummaryService) claimSummaryRequest(ctx context.Context, request db.SummaryRequest) (bool, error) {
	if request.Status == summaryRequestPendingTranscript {
		if err := s.Repo.MarkSummaryRequestPendingSummary(ctx, request.ID); err != nil {
			if errors.Is(err, db.ErrNotFound) {
				return false, nil
			}
			return false, err
		}
	}
	if err := s.Repo.MarkSummaryRequestSummarizing(ctx, request.ID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s SummaryService) summaryCache(ctx context.Context, media db.MediaItem, group summaryRequestGroup, requests []db.SummaryRequest) (summaryCacheResult, error) {
	cache, found, err := s.Repo.FindSummaryCache(ctx, media.ID, group.promptHash, s.Model)
	if err != nil {
		return summaryCacheResult{}, err
	}
	if found {
		s.logger().Info("summary cache hit", "media_id", media.ID, "summary_cache_id", cache.ID, "model", s.Model, "request_ids", requestIDs(requests))
		return summaryCacheResult{cache: cache}, nil
	}
	s.logger().Info("summary cache miss", "media_id", media.ID, "model", s.Model, "request_ids", requestIDs(requests))
	return s.createSummaryCache(ctx, media, group, requests)
}

func (s SummaryService) createSummaryCache(ctx context.Context, media db.MediaItem, group summaryRequestGroup, requests []db.SummaryRequest) (summaryCacheResult, error) {
	summaryText, err := s.Summarizer.Summarize(ctx, group.promptText, media.TranscriptText)
	if err != nil {
		s.logger().Error("summarization failed", "media_id", media.ID, "request_ids", requestIDs(requests), "model", s.Model, "error", err)
		return summaryCacheResult{}, fmt.Errorf("summarize media %d: %w", media.ID, err)
	}

	cache := db.SummaryCache{MediaItemID: media.ID, PromptHash: group.promptHash, PromptText: group.promptText, SummaryText: summaryText, Model: s.Model}
	insertedCache, err := s.Repo.InsertSummaryCache(ctx, cache)
	if err == nil {
		return summaryCacheResult{cache: insertedCache, generated: true}, nil
	}

	cachedSummary, found, retryErr := s.Repo.FindSummaryCache(ctx, media.ID, group.promptHash, s.Model)
	if retryErr != nil {
		return summaryCacheResult{}, errors.Join(err, retryErr)
	}
	if !found {
		return summaryCacheResult{}, err
	}
	return summaryCacheResult{cache: cachedSummary}, nil
}

func (s SummaryService) sendSummary(ctx context.Context, media db.MediaItem, requests []db.SummaryRequest, cacheResult summaryCacheResult, metadata display.SummaryMetadata) error {
	if s.Progress != nil && hasReplyRequests(requests) {
		if err := s.Progress.MediaProgress(ctx, media.ID, SummarySendingNotification().Text); err != nil {
			s.logger().Warn("summary sending progress failed", "media_id", media.ID, "error", err)
		}
	}
	cache := cacheResult.cache
	sent := false
	var sendErr error
	for _, request := range requests {
		attrs := []any{"request_id", request.ID, "media_id", request.MediaItemID, "summary_cache_id", cache.ID}
		if err := s.Repo.MarkSummaryRequestSending(ctx, request.ID); err != nil {
			if !errors.Is(err, db.ErrNotFound) {
				sendErr = errors.Join(sendErr, err)
			}
			continue
		}
		if err := s.sendFinalSummary(ctx, request, cache.SummaryText, metadata, attrs...); err != nil {
			s.logger().Error("telegram send failed", append([]any{"chat_id", request.ChatID, "error", err}, attrs...)...)
			sendErr = errors.Join(sendErr, s.handleSummarySendFailure(ctx, request.ID, err))
			continue
		}
		if err := s.markSummarySent(ctx, request.ID, cache.ID); err != nil {
			sendErr = errors.Join(sendErr, err)
			continue
		}
		sent = true
	}
	if cacheResult.generated && sent {
		s.broadcastGeneratedSummary(ctx, media, cache, metadata)
	}
	return sendErr
}

func (s SummaryService) broadcastGeneratedSummary(ctx context.Context, media db.MediaItem, cache db.SummaryCache, metadata display.SummaryMetadata) {
	if s.SummaryBroadcastChannelID == 0 || s.SummaryBroadcaster == nil {
		return
	}
	if err := s.SummaryBroadcaster.BroadcastFinalSummary(context.WithoutCancel(ctx), s.SummaryBroadcastChannelID, cache.SummaryText, metadata, media.CanonicalURL, "summary_cache_id", cache.ID); err != nil {
		s.logger().Warn("summary channel broadcast failed", "channel_id", s.SummaryBroadcastChannelID, "summary_cache_id", cache.ID, "error", err)
	}
}

func (s SummaryService) sendFinalSummary(ctx context.Context, request db.SummaryRequest, text string, metadata display.SummaryMetadata, attrs ...any) error {
	if s.Progress != nil {
		if notifier, ok := s.Progress.(SummaryProgressMetadataNotifier); ok {
			return notifier.FinalSummaryWithMetadata(ctx, request, text, metadata, attrs...)
		}
		return s.Progress.FinalSummary(ctx, request, text, attrs...)
	}
	if sender, ok := s.Sender.(FinalSummaryPartsWithMetadataSender); ok {
		messageIDs, err := sender.SendFinalSummaryPartsWithMetadata(ctx, request.ChatID, request.MessageID, text, request.ID, metadata, attrs...)
		if err != nil {
			return err
		}
		return s.storeFinalSummaryMessages(context.WithoutCancel(ctx), request, messageIDs)
	}
	if sender, ok := s.Sender.(FinalSummaryPartsSender); ok {
		messageIDs, err := sender.SendFinalSummaryParts(ctx, request.ChatID, request.MessageID, text, request.ID, attrs...)
		if err != nil {
			return err
		}
		return s.storeFinalSummaryMessages(context.WithoutCancel(ctx), request, messageIDs)
	}
	if sender, ok := s.Sender.(FinalSummarySender); ok {
		_, err := sender.SendFinalSummary(ctx, request.ChatID, request.MessageID, text, attrs...)
		return err
	}
	if sender, ok := s.Sender.(ContextualReplySummarySender); ok {
		_, err := sender.SendReplyTextWithAttrs(ctx, request.ChatID, request.MessageID, text, attrs...)
		return err
	}
	if sender, ok := s.Sender.(ReplySummarySender); ok {
		_, err := sender.SendReplyText(ctx, request.ChatID, request.MessageID, text)
		return err
	}
	if sender, ok := s.Sender.(ContextualSummarySender); ok {
		return sender.SendTextWithAttrs(ctx, request.ChatID, text, attrs...)
	}
	return s.Sender.SendText(ctx, request.ChatID, text)
}

func (s SummaryService) storeFinalSummaryMessages(ctx context.Context, request db.SummaryRequest, messageIDs []int64) error {
	return storeSummaryRequestMessages(ctx, s.Repo, request, messageIDs)
}

func (s SummaryService) SwitchSummaryVariant(ctx context.Context, command SwitchSummaryVariantCommand) error {
	if s.Repo == nil {
		return errors.New("summary repository is required")
	}
	if s.Summarizer == nil {
		return errors.New("summarizer is required")
	}
	editor, ok := s.Sender.(FinalSummaryEditor)
	if !ok {
		return errors.New("summary editor is required")
	}
	request, err := s.Repo.GetSummaryRequest(ctx, command.RequestID)
	if err != nil {
		return err
	}
	if request.ChatID != command.ChatID {
		return errors.New("summary variant callback chat mismatch")
	}
	messages, err := s.Repo.ListActiveSummaryRequestMessagesByKind(ctx, request.ID, []string{RequestMessageKindSummary, RequestMessageKindSummaryPart1, RequestMessageKindSummaryPart2})
	if err != nil {
		return err
	}
	messageIDs, ok := summaryPartMessageIDs(messages)
	if !ok {
		return errors.New("stored summary messages are incomplete")
	}
	// The variant buttons live on the final summary message: the single
	// rich message (messageIDs[0] when one message) or the second legacy
	// part (messageIDs[1] when two messages).
	buttonMessageID := messageIDs[len(messageIDs)-1]
	if command.MessageID != buttonMessageID {
		return errors.New("summary variant callback message mismatch")
	}
	media, err := s.Repo.GetMedia(ctx, request.MediaItemID)
	if err != nil {
		return err
	}
	prompt := command.Variant.Prompt()
	promptHash := summarize.PromptHash(prompt)
	cache, found, err := s.Repo.FindSummaryCache(ctx, media.ID, promptHash, s.Model)
	if err != nil {
		return err
	}
	if !found {
		cacheResult, err := s.createSummaryCache(ctx, media, summaryRequestGroup{promptHash: promptHash, promptText: prompt}, []db.SummaryRequest{request})
		cache = cacheResult.cache
		if err != nil {
			return err
		}
	}
	metadata := s.displayMetadata(ctx, media, display.SummaryMetadata{})
	attrs := []any{"request_id", request.ID, "media_id", request.MediaItemID, "summary_cache_id", cache.ID}
	if metadataEditor, ok := s.Sender.(FinalSummaryWithMetadataEditor); ok {
		if err := metadataEditor.EditFinalSummaryPartsWithMetadata(ctx, request.ChatID, messageIDs, cache.SummaryText, request.ID, metadata, attrs...); err != nil {
			return err
		}
	} else if err := editor.EditFinalSummaryParts(ctx, request.ChatID, messageIDs, cache.SummaryText, request.ID, attrs...); err != nil {
		return err
	}
	return s.Repo.UpdateSummaryRequestCache(context.WithoutCancel(ctx), request.ID, cache.ID)
}

func summaryPartMessageIDs(messages []db.SummaryRequestMessage) ([]int64, bool) {
	var single int64
	ids := make([]int64, 2)
	for _, message := range messages {
		switch message.Kind {
		case RequestMessageKindSummary:
			single = message.TelegramMessageID
		case RequestMessageKindSummaryPart1:
			ids[0] = message.TelegramMessageID
		case RequestMessageKindSummaryPart2:
			ids[1] = message.TelegramMessageID
		}
	}
	if single != 0 {
		return []int64{single}, true
	}
	return ids, ids[0] != 0 && ids[1] != 0
}

func (s SummaryService) markSummarySent(ctx context.Context, requestID, summaryCacheID int64) error {
	ctx = context.WithoutCancel(ctx)
	if err := s.Repo.MarkSummaryRequestSent(ctx, requestID, summaryCacheID); err != nil {
		markErr := s.Repo.MarkSummaryRequestDeliveryUnknown(ctx, requestID, shortError(err))
		return errors.Join(durableSummaryDeliveryError{requestID: requestID, cause: err}, markErr)
	}
	return nil
}

func (s SummaryService) handleSummarySendFailure(ctx context.Context, requestID int64, cause error) error {
	if markErr := s.markSendFailure(ctx, requestID, cause); markErr != nil {
		return errors.Join(fmt.Errorf("send summary request %d: %w", requestID, cause), markErr)
	}
	return durableSummaryDeliveryError{requestID: requestID, cause: cause}
}

func (s SummaryService) markSendFailure(ctx context.Context, requestID int64, cause error) error {
	message := shortError(cause)
	switch {
	case isAmbiguousDelivery(cause):
		return s.Repo.MarkSummaryRequestDeliveryUnknown(ctx, requestID, message)
	case isTerminalDeliveryFailure(cause):
		return s.Repo.MarkSummaryRequestFailed(ctx, requestID, message)
	default:
		return s.Repo.MarkSummaryRequestRetryPending(ctx, requestID, message)
	}
}

func isAmbiguousDelivery(err error) bool {
	type ambiguousDelivery interface {
		AmbiguousDelivery() bool
	}
	var ambiguous ambiguousDelivery
	return errors.As(err, &ambiguous) && ambiguous.AmbiguousDelivery()
}

func isTerminalDeliveryFailure(err error) bool {
	type terminalDeliveryFailure interface {
		TerminalDeliveryFailure() bool
	}
	var terminal terminalDeliveryFailure
	return errors.As(err, &terminal) && terminal.TerminalDeliveryFailure()
}

func (s SummaryService) markRequestsRetryPending(ctx context.Context, requests []db.SummaryRequest, cause error) error {
	var markErr error
	for _, request := range requests {
		err := s.Repo.MarkSummaryRequestRetryPending(ctx, request.ID, shortError(cause))
		if errors.Is(err, db.ErrNotFound) {
			continue
		}
		markErr = errors.Join(markErr, err)
	}
	return markErr
}

func hasReplyRequests(requests []db.SummaryRequest) bool {
	for _, request := range requests {
		if request.MessageID != 0 {
			return true
		}
	}
	return false
}

func requestIDs(requests []db.SummaryRequest) []int64 {
	ids := make([]int64, 0, len(requests))
	for _, request := range requests {
		ids = append(ids, request.ID)
	}
	return ids
}

func (s SummaryService) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
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
