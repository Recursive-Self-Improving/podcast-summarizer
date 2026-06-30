package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/db"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/display"
)

const (
	RequestMessageKindProgress     = "progress"
	RequestMessageKindNotice       = "notice"
	RequestMessageKindSummaryPart1 = "summary_part_1"
	RequestMessageKindSummaryPart2 = "summary_part_2"
)

type ProgressRepository interface {
	IsFirstRequestForMediaChat(ctx context.Context, requestID, mediaItemID, chatID int64) (bool, error)
	ListProgressOwnerRequestsForMedia(ctx context.Context, mediaItemID int64) ([]db.SummaryRequest, error)
	CreateSummaryRequestMessage(ctx context.Context, message db.SummaryRequestMessage) (db.SummaryRequestMessage, error)
	LatestActiveSummaryRequestMessage(ctx context.Context, requestID int64, kind string) (db.SummaryRequestMessage, bool, error)
	ListActiveSummaryRequestMessages(ctx context.Context, requestIDs []int64) ([]db.SummaryRequestMessage, error)
	MarkSummaryRequestMessageDeleted(ctx context.Context, id int64) error
	MarkSummaryRequestMessagesDeleted(ctx context.Context, ids []int64) error
}

type ReplyingSender interface {
	SendReplyText(ctx context.Context, chatID, replyToMessageID int64, text string) (int64, error)
	DeleteMessage(ctx context.Context, chatID, messageID int64) error
}

type ContextualReplyingSender interface {
	SendReplyTextWithAttrs(ctx context.Context, chatID, replyToMessageID int64, text string, attrs ...any) (int64, error)
}

type ProgressNotifier struct {
	Repo   ProgressRepository
	Sender ReplyingSender
	Logger *slog.Logger
}

func (n ProgressNotifier) InitialRequestStatus(ctx context.Context, result SummaryCommandResult) error {
	if result.Summarized || result.Request.MessageID == 0 {
		return nil
	}
	if err := n.validate(); err != nil {
		return err
	}
	isOwner, err := n.Repo.IsFirstRequestForMediaChat(ctx, result.Request.ID, result.Request.MediaItemID, result.Request.ChatID)
	if err != nil {
		return err
	}
	notification := TranscriptProcessingNotification(result.Media)
	if result.CreatedTranscriptionJob {
		notification = TranscriptionQueuedNotification(result.Media)
	}
	if isOwner {
		return n.sendProgress(ctx, result.Request, notification.Text)
	}
	return n.sendNotice(ctx, result.Request, notification.Text)
}

func (n ProgressNotifier) MediaProgress(ctx context.Context, mediaItemID int64, text string) error {
	if err := n.validate(); err != nil {
		return err
	}
	requests, err := n.Repo.ListProgressOwnerRequestsForMedia(ctx, mediaItemID)
	if err != nil {
		return err
	}
	for _, request := range requests {
		if request.MessageID == 0 {
			continue
		}
		if err := n.sendProgress(ctx, request, text); err != nil {
			n.logger().Warn("progress send failed", "request_id", request.ID, "media_id", request.MediaItemID, "chat_id", request.ChatID, "error", err)
		}
	}
	return nil
}

func (n ProgressNotifier) FinalSummary(ctx context.Context, request db.SummaryRequest, summaryText string, attrs ...any) error {
	return n.FinalSummaryWithMetadata(ctx, request, summaryText, display.SummaryMetadata{}, attrs...)
}

func (n ProgressNotifier) FinalSummaryWithMetadata(ctx context.Context, request db.SummaryRequest, summaryText string, metadata display.SummaryMetadata, attrs ...any) error {
	if err := n.validate(); err != nil {
		return err
	}
	messageIDs, err := n.sendFinalReply(ctx, request, summaryText, metadata, attrs...)
	if err != nil {
		return err
	}
	if err := n.storeFinalSummaryMessages(context.WithoutCancel(ctx), request, messageIDs); err != nil {
		return err
	}
	n.CleanupRequests(context.WithoutCancel(ctx), []db.SummaryRequest{request})
	return nil
}

func (n ProgressNotifier) FinalFailureText(ctx context.Context, request db.SummaryRequest, text string) error {
	if err := n.validate(); err != nil {
		return err
	}
	if _, err := n.Sender.SendReplyText(ctx, request.ChatID, request.MessageID, text); err != nil {
		return err
	}
	n.CleanupRequests(context.WithoutCancel(ctx), []db.SummaryRequest{request})
	return nil
}

func (n ProgressNotifier) CleanupRequests(ctx context.Context, requests []db.SummaryRequest) {
	if n.Repo == nil || n.Sender == nil || len(requests) == 0 {
		return
	}
	requestIDs := make([]int64, 0, len(requests))
	for _, request := range requests {
		requestIDs = append(requestIDs, request.ID)
	}
	messages, err := n.Repo.ListActiveSummaryRequestMessages(ctx, requestIDs)
	if err != nil {
		n.logger().Warn("list intermediate messages failed", "error", err)
		return
	}
	deletedIDs := make([]int64, 0, len(messages))
	for _, message := range messages {
		if message.Kind != RequestMessageKindProgress && message.Kind != RequestMessageKindNotice {
			continue
		}
		if err := n.Sender.DeleteMessage(ctx, message.ChatID, message.TelegramMessageID); err != nil {
			n.logger().Warn("delete intermediate message failed", "request_id", message.SummaryRequestID, "chat_id", message.ChatID, "message_id", message.TelegramMessageID, "error", err)
			if !isTelegramMessageAlreadyGone(err) {
				continue
			}
		}
		deletedIDs = append(deletedIDs, message.ID)
	}
	if err := n.Repo.MarkSummaryRequestMessagesDeleted(ctx, deletedIDs); err != nil {
		n.logger().Warn("mark intermediate messages deleted failed", "error", err)
	}
}

func (n ProgressNotifier) sendProgress(ctx context.Context, request db.SummaryRequest, text string) error {
	previous, hasPrevious, err := n.Repo.LatestActiveSummaryRequestMessage(ctx, request.ID, RequestMessageKindProgress)
	if err != nil {
		return err
	}
	messageID, err := n.Sender.SendReplyText(ctx, request.ChatID, request.MessageID, text)
	if err != nil {
		return err
	}
	if _, err := n.Repo.CreateSummaryRequestMessage(ctx, db.SummaryRequestMessage{
		SummaryRequestID:  request.ID,
		ChatID:            request.ChatID,
		TelegramMessageID: messageID,
		Kind:              RequestMessageKindProgress,
	}); err != nil {
		if deleteErr := n.Sender.DeleteMessage(context.WithoutCancel(ctx), request.ChatID, messageID); deleteErr != nil {
			n.logger().Warn("delete untracked progress message failed", "request_id", request.ID, "chat_id", request.ChatID, "message_id", messageID, "error", deleteErr)
		}
		return err
	}
	if !hasPrevious {
		return nil
	}
	if err := n.Sender.DeleteMessage(ctx, previous.ChatID, previous.TelegramMessageID); err != nil {
		n.logger().Warn("delete previous progress message failed", "request_id", request.ID, "chat_id", previous.ChatID, "message_id", previous.TelegramMessageID, "error", err)
		if !isTelegramMessageAlreadyGone(err) {
			return nil
		}
	}
	if err := n.Repo.MarkSummaryRequestMessageDeleted(ctx, previous.ID); err != nil {
		n.logger().Warn("mark previous progress message deleted failed", "request_id", request.ID, "message_row_id", previous.ID, "error", err)
	}
	return nil
}

func isTelegramMessageAlreadyGone(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "message to delete not found")
}

func (n ProgressNotifier) sendNotice(ctx context.Context, request db.SummaryRequest, text string) error {
	messageID, err := n.Sender.SendReplyText(ctx, request.ChatID, request.MessageID, text)
	if err != nil {
		return err
	}
	if _, err := n.Repo.CreateSummaryRequestMessage(ctx, db.SummaryRequestMessage{
		SummaryRequestID:  request.ID,
		ChatID:            request.ChatID,
		TelegramMessageID: messageID,
		Kind:              RequestMessageKindNotice,
	}); err != nil {
		if deleteErr := n.Sender.DeleteMessage(context.WithoutCancel(ctx), request.ChatID, messageID); deleteErr != nil {
			n.logger().Warn("delete untracked notice message failed", "request_id", request.ID, "chat_id", request.ChatID, "message_id", messageID, "error", deleteErr)
		}
		return err
	}
	return nil
}

func (n ProgressNotifier) sendFinalReply(ctx context.Context, request db.SummaryRequest, text string, metadata display.SummaryMetadata, attrs ...any) ([]int64, error) {
	if sender, ok := n.Sender.(FinalSummaryPartsWithMetadataSender); ok {
		return sender.SendFinalSummaryPartsWithMetadata(ctx, request.ChatID, request.MessageID, text, request.ID, metadata, attrs...)
	}
	if sender, ok := n.Sender.(FinalSummaryPartsSender); ok {
		return sender.SendFinalSummaryParts(ctx, request.ChatID, request.MessageID, text, request.ID, attrs...)
	}
	if sender, ok := n.Sender.(FinalSummarySender); ok {
		messageID, err := sender.SendFinalSummary(ctx, request.ChatID, request.MessageID, text, attrs...)
		return []int64{messageID}, err
	}
	if sender, ok := n.Sender.(ContextualReplyingSender); ok {
		messageID, err := sender.SendReplyTextWithAttrs(ctx, request.ChatID, request.MessageID, text, attrs...)
		return []int64{messageID}, err
	}
	messageID, err := n.Sender.SendReplyText(ctx, request.ChatID, request.MessageID, text)
	return []int64{messageID}, err
}

func (n ProgressNotifier) storeFinalSummaryMessages(ctx context.Context, request db.SummaryRequest, messageIDs []int64) error {
	kinds := []string{RequestMessageKindSummaryPart1, RequestMessageKindSummaryPart2}
	for i, messageID := range messageIDs {
		kind := ""
		if i < len(kinds) {
			kind = kinds[i]
		} else {
			kind = "summary_part_" + string(rune('1'+i))
		}
		if _, err := n.Repo.CreateSummaryRequestMessage(ctx, db.SummaryRequestMessage{
			SummaryRequestID:  request.ID,
			ChatID:            request.ChatID,
			TelegramMessageID: messageID,
			Kind:              kind,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (n ProgressNotifier) validate() error {
	if n.Repo == nil {
		return errors.New("progress repository is required")
	}
	if n.Sender == nil {
		return errors.New("progress sender is required")
	}
	return nil
}

func (n ProgressNotifier) logger() *slog.Logger {
	if n.Logger != nil {
		return n.Logger
	}
	return slog.Default()
}
