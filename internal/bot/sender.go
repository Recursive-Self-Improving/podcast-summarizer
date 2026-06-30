package bot

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/display"
)

const maxTelegramTextChars = 3500

type SenderClient interface {
	SendMessage(ctx context.Context, chatID int64, text string) (int64, error)
	SendReply(ctx context.Context, chatID, replyToMessageID int64, text string) (int64, error)
	SendDocument(ctx context.Context, chatID int64, path string) (int64, error)
	SendReplyDocument(ctx context.Context, chatID, replyToMessageID int64, path string) (int64, error)
	DeleteMessage(ctx context.Context, chatID, messageID int64) error
}

type htmlSenderClient interface {
	SendHTMLMessage(ctx context.Context, chatID int64, text string) (int64, error)
	SendHTMLReply(ctx context.Context, chatID, replyToMessageID int64, text string) (int64, error)
}

type htmlMarkupSenderClient interface {
	SendHTMLMessageWithMarkup(ctx context.Context, chatID int64, text string, markup any) (int64, error)
	SendHTMLReplyWithMarkup(ctx context.Context, chatID, replyToMessageID int64, text string, markup any) (int64, error)
}

type htmlEditorClient interface {
	EditHTMLMessage(ctx context.Context, chatID, messageID int64, text string, markup any) error
}

type forceReplySenderClient interface {
	SendForceReply(ctx context.Context, chatID, replyToMessageID int64, text, placeholder string) (int64, error)
}

type Sender struct {
	Client  SenderClient
	TempDir string
	Logger  *slog.Logger
}

func (s Sender) SendText(ctx context.Context, chatID int64, text string) error {
	_, err := s.sendText(ctx, chatID, 0, text, nil)
	return err
}

func (s Sender) SendTextWithAttrs(ctx context.Context, chatID int64, text string, attrs ...any) error {
	_, err := s.sendText(ctx, chatID, 0, text, attrs)
	return err
}

func (s Sender) SendReplyText(ctx context.Context, chatID, replyToMessageID int64, text string) (int64, error) {
	return s.sendText(ctx, chatID, replyToMessageID, text, nil)
}

func (s Sender) SendReplyTextWithAttrs(ctx context.Context, chatID, replyToMessageID int64, text string, attrs ...any) (int64, error) {
	return s.sendText(ctx, chatID, replyToMessageID, text, attrs)
}

func (s Sender) SendForceReplyText(ctx context.Context, chatID, replyToMessageID int64, text, placeholder string) (int64, error) {
	client, ok := s.Client.(forceReplySenderClient)
	if !ok {
		return s.SendReplyText(ctx, chatID, replyToMessageID, text)
	}
	return client.SendForceReply(ctx, chatID, replyToMessageID, text, placeholder)
}

func (s Sender) SendFinalSummary(ctx context.Context, chatID, replyToMessageID int64, text string, attrs ...any) (int64, error) {
	messageIDs, err := s.SendFinalSummaryParts(ctx, chatID, replyToMessageID, text, 0, attrs...)
	if err != nil {
		return 0, err
	}
	if len(messageIDs) == 0 {
		return 0, nil
	}
	return messageIDs[0], nil
}

func (s Sender) SendFinalSummaryParts(ctx context.Context, chatID, replyToMessageID int64, text string, requestID int64, attrs ...any) ([]int64, error) {
	return s.SendFinalSummaryPartsWithMetadata(ctx, chatID, replyToMessageID, text, requestID, display.SummaryMetadata{}, attrs...)
}

func (s Sender) SendFinalSummaryPartsWithMetadata(ctx context.Context, chatID, replyToMessageID int64, text string, requestID int64, metadata display.SummaryMetadata, attrs ...any) ([]int64, error) {
	client, ok := s.Client.(htmlSenderClient)
	plainFallback := summaryTextWithMetadata(text, metadata)
	if !ok {
		messageID, err := s.sendText(ctx, chatID, replyToMessageID, plainFallback, attrs)
		if err != nil {
			return nil, err
		}
		return []int64{messageID}, nil
	}
	messages, err := renderFinalSummaryHTMLPartsWithMetadata(text, metadata, maxTelegramTextChars)
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		messageID, err := s.sendText(ctx, chatID, replyToMessageID, plainFallback, attrs)
		if err != nil {
			return nil, err
		}
		return []int64{messageID}, nil
	}
	return s.sendHTMLMessages(ctx, client, chatID, replyToMessageID, plainFallback, messages, summaryVariantKeyboard(requestID), attrs)
}

func (s Sender) EditFinalSummaryParts(ctx context.Context, chatID int64, messageIDs []int64, text string, requestID int64, attrs ...any) error {
	return s.EditFinalSummaryPartsWithMetadata(ctx, chatID, messageIDs, text, requestID, display.SummaryMetadata{}, attrs...)
}

func (s Sender) EditFinalSummaryPartsWithMetadata(ctx context.Context, chatID int64, messageIDs []int64, text string, requestID int64, metadata display.SummaryMetadata, attrs ...any) error {
	client, ok := s.Client.(htmlEditorClient)
	if !ok {
		return fmt.Errorf("telegram html editor is required")
	}
	messages, err := renderFinalSummaryHTMLPartsWithMetadata(text, metadata, maxTelegramTextChars)
	if err != nil {
		return err
	}
	if len(messages) != 2 || len(messageIDs) != 2 {
		return fmt.Errorf("summary edit requires exactly two messages")
	}
	keyboard := summaryVariantKeyboard(requestID)
	for i, message := range messages {
		var markup any
		if i == 1 {
			markup = keyboard
		}
		if err := client.EditHTMLMessage(ctx, chatID, messageIDs[i], message, markup); err != nil && !isMessageNotModifiedError(err) {
			s.logger().Warn("telegram html edit failed", append([]any{"chat_id", chatID, "message_id", messageIDs[i], "error", err}, attrs...)...)
			return err
		}
	}
	return nil
}

func (s Sender) DeleteMessage(ctx context.Context, chatID, messageID int64) error {
	if s.Client == nil {
		return fmt.Errorf("sender client is required")
	}
	return s.Client.DeleteMessage(ctx, chatID, messageID)
}

type sendError struct {
	action    string
	cause     error
	ambiguous bool
	terminal  bool
}

func (e sendError) Error() string {
	return fmt.Sprintf("%s: %v", e.action, e.cause)
}

func (e sendError) Unwrap() error {
	return e.cause
}

func (e sendError) AmbiguousDelivery() bool {
	return e.ambiguous
}

func (e sendError) TerminalDeliveryFailure() bool {
	return e.terminal
}

func newSendError(action string, cause error, deliveredMessages int) sendError {
	return sendError{
		action:    action,
		cause:     cause,
		ambiguous: isAmbiguousSendError(cause, deliveredMessages),
		terminal:  isTerminalSendError(cause, deliveredMessages),
	}
}

func (s Sender) sendHTMLMessages(ctx context.Context, client htmlSenderClient, chatID, replyToMessageID int64, plainFallback string, messages []string, keyboard any, attrs []any) ([]int64, error) {
	messageIDs := make([]int64, 0, len(messages))
	for i, message := range messages {
		var markup any
		if i == 1 {
			markup = keyboard
		}
		messageID, err := s.sendHTMLMessage(ctx, client, chatID, replyToMessageID, message, markup)
		if err != nil {
			s.logger().Warn("telegram html send failed", append([]any{"chat_id", chatID, "error", err}, attrs...)...)
			if i == 0 && replyToMessageID != 0 && isMissingReplyTargetError(err) {
				return s.sendHTMLMessages(ctx, client, chatID, 0, plainFallback, messages, keyboard, attrs)
			}
			if i == 0 && isMessageTooLongError(err) {
				messageID, err := s.sendDocumentFallback(ctx, chatID, replyToMessageID, plainFallback, attrs)
				if err != nil {
					return nil, err
				}
				return []int64{messageID}, nil
			}
			return nil, newSendError(fmt.Sprintf("send final summary chunk %d", i+1), err, i)
		}
		messageIDs = append(messageIDs, messageID)
	}
	return messageIDs, nil
}

func (s Sender) sendHTMLMessage(ctx context.Context, client htmlSenderClient, chatID, replyToMessageID int64, text string, markup any) (int64, error) {
	if markup != nil {
		if markupClient, ok := client.(htmlMarkupSenderClient); ok {
			if replyToMessageID != 0 {
				return markupClient.SendHTMLReplyWithMarkup(ctx, chatID, replyToMessageID, text, markup)
			}
			return markupClient.SendHTMLMessageWithMarkup(ctx, chatID, text, markup)
		}
	}
	if replyToMessageID != 0 {
		return client.SendHTMLReply(ctx, chatID, replyToMessageID, text)
	}
	return client.SendHTMLMessage(ctx, chatID, text)
}

func (s Sender) sendText(ctx context.Context, chatID, replyToMessageID int64, text string, attrs []any) (int64, error) {
	if s.Client == nil {
		return 0, fmt.Errorf("sender client is required")
	}
	chunks := splitTelegramText(text, maxTelegramTextChars)
	if len(chunks) == 0 {
		chunks = []string{""}
	}
	messages := chunks
	if len(chunks) > 1 {
		for {
			prefixReserve := runeLen(fmt.Sprintf("Summary %d/%d\n\n", len(chunks), len(chunks)))
			next := splitTelegramText(text, maxTelegramTextChars-prefixReserve)
			if len(next) == len(chunks) {
				chunks = next
				break
			}
			chunks = next
		}
		messages = make([]string, len(chunks))
		for i, chunk := range chunks {
			messages[i] = fmt.Sprintf("Summary %d/%d\n\n%s", i+1, len(chunks), chunk)
		}
	}
	var firstMessageID int64
	for i, message := range messages {
		messageID, err := s.sendMessage(ctx, chatID, replyToMessageID, message)
		if err != nil {
			s.logger().Warn("telegram send failed", append([]any{"chat_id", chatID, "error", err}, attrs...)...)
			if i == 0 && replyToMessageID != 0 && isMissingReplyTargetError(err) {
				return s.sendText(ctx, chatID, 0, text, attrs)
			}
			if i == 0 && isMessageTooLongError(err) {
				return s.sendDocumentFallback(ctx, chatID, replyToMessageID, text, attrs)
			}
			return 0, newSendError(fmt.Sprintf("send summary chunk %d", i+1), err, i)
		}
		if firstMessageID == 0 {
			firstMessageID = messageID
		}
	}
	return firstMessageID, nil
}

func (s Sender) sendMessage(ctx context.Context, chatID, replyToMessageID int64, text string) (int64, error) {
	if replyToMessageID != 0 {
		return s.Client.SendReply(ctx, chatID, replyToMessageID, text)
	}
	return s.Client.SendMessage(ctx, chatID, text)
}

func isMessageTooLongError(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "message is too long")
}

func isMissingReplyTargetError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "reply message not found") || strings.Contains(message, "message to be replied not found")
}

func isMessageNotModifiedError(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "message is not modified")
}

func isAmbiguousSendError(err error, deliveredMessages int) bool {
	return deliveredMessages > 0 || !isTelegramAPIRejection(err)
}

func isTerminalSendError(err error, deliveredMessages int) bool {
	return deliveredMessages == 0 && isTerminalTelegramAPIRejection(err)
}

func isTelegramAPIRejection(err error) bool {
	message := strings.ToLower(err.Error())
	for _, prefix := range []string{"bad request:", "forbidden:", "too many requests:"} {
		if strings.Contains(message, prefix) {
			return true
		}
	}
	return false
}

func isTerminalTelegramAPIRejection(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "forbidden:") || strings.Contains(message, "bad request:")
}

func (s Sender) sendDocumentFallback(ctx context.Context, chatID, replyToMessageID int64, text string, attrs []any) (int64, error) {
	file, err := os.CreateTemp(s.TempDir, "summary-*.txt")
	if err != nil {
		return 0, fmt.Errorf("create summary document: %w", err)
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err := file.WriteString(text); err != nil {
		file.Close()
		return 0, fmt.Errorf("write summary document: %w", err)
	}
	if err := file.Close(); err != nil {
		return 0, fmt.Errorf("close summary document: %w", err)
	}
	messageID, err := s.sendDocument(ctx, chatID, replyToMessageID, path)
	if err != nil {
		s.logger().Error("telegram document send failed", append([]any{"chat_id", chatID, "error", err}, attrs...)...)
		if replyToMessageID != 0 && isMissingReplyTargetError(err) {
			return s.sendDocumentFallback(ctx, chatID, 0, text, attrs)
		}
		return 0, newSendError("send summary document", err, 0)
	}
	return messageID, nil
}

func (s Sender) sendDocument(ctx context.Context, chatID, replyToMessageID int64, path string) (int64, error) {
	if replyToMessageID != 0 {
		return s.Client.SendReplyDocument(ctx, chatID, replyToMessageID, path)
	}
	return s.Client.SendDocument(ctx, chatID, path)
}

func (s Sender) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

func splitTelegramText(text string, limit int) []string {
	if limit <= 0 {
		limit = maxTelegramTextChars
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	var chunks []string
	for runeLen(text) > limit {
		cut := boundaryCut(text, limit, "\n\n")
		if cut <= 0 {
			cut = boundaryCut(text, limit, "\n")
		}
		if cut <= 0 {
			cut = byteIndexAfterRunes(text, limit)
		}
		chunks = append(chunks, strings.TrimSpace(text[:cut]))
		text = strings.TrimSpace(text[cut:])
	}
	if text != "" {
		chunks = append(chunks, text)
	}
	return chunks
}

func boundaryCut(text string, limit int, separator string) int {
	limitByte := byteIndexAfterRunes(text, limit+1)
	if limitByte > len(text) {
		limitByte = len(text)
	}
	return strings.LastIndex(text[:limitByte], separator)
}

func byteIndexAfterRunes(text string, count int) int {
	if count <= 0 {
		return 0
	}
	seen := 0
	for i := range text {
		if seen == count {
			return i
		}
		seen++
	}
	return len(text)
}

func runeLen(text string) int {
	return len([]rune(text))
}
