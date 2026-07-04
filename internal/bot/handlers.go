package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/auth"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/db"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/provider"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/service"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/summarize"
)

const (
	helpText                    = "Commands:\n/summarize_podcast [MEDIA_URL]\n/summary_status [MEDIA_URL]\n/help\n\nOwner commands:\n/subscribe_podcast <PODCAST_URL>\n/unsubscribe_podcast <PODCAST_URL>\n/subscriptions"
	customPromptUnsupportedText = "Custom prompts are currently unsupported. Please use /summarize_podcast <MEDIA_URL>."
	summarizeURLPromptText      = "Please reply to this message with a YouTube, Xiaoyuzhou, or SoundOn URL to summarize."
	statusURLPromptText         = "Please reply to this message with a YouTube, Xiaoyuzhou, or SoundOn URL to check summary status."
	urlPromptPlaceholderText    = "" // "Paste the media URL here"
	wrongPromptUserText         = "Please have the user who requested this prompt reply with the URL."
)

type Message struct {
	Text             string
	ChatID           int64
	ChatType         auth.ChatType
	ChatTitle        string
	UserID           int64
	Username         string
	FirstName        string
	MessageID        int64
	ReplyToMessageID int64
}

type MessageSender interface {
	SendText(ctx context.Context, chatID int64, text string) error
	SendReplyText(ctx context.Context, chatID, replyToMessageID int64, text string) (int64, error)
	SendForceReplyText(ctx context.Context, chatID, replyToMessageID int64, text, placeholder string) (int64, error)
}

type Authorizer interface {
	CanUse(ctx context.Context, chatID, userID int64, chatType auth.ChatType) (bool, error)
	CanManageWhitelist(userID int64) bool
	CanManageSubscriptions(userID int64) bool
}

type SummaryRequester interface {
	RequestSummary(ctx context.Context, command service.SummaryCommand) (service.SummaryCommandResult, error)
}

type StatusReporter interface {
	Status(ctx context.Context, query service.StatusQuery) (service.StatusReport, error)
}

type WatchManager interface {
	Subscribe(ctx context.Context, command service.WatchSubscribeCommand) (service.WatchResponse, error)
	Unsubscribe(ctx context.Context, command service.WatchUnsubscribeCommand) (service.WatchResponse, error)
	ListSubscriptions(ctx context.Context, chatID int64) (service.WatchResponse, error)
}

type SummaryProgressReporter interface {
	InitialRequestStatus(ctx context.Context, result service.SummaryCommandResult) error
}

type SummaryVariantCallback struct {
	ID        string
	Data      string
	ChatID    int64
	ChatType  auth.ChatType
	UserID    int64
	MessageID int64
}

type SummaryVariantSwitcher interface {
	SwitchSummaryVariant(ctx context.Context, command service.SwitchSummaryVariantCommand) error
}

type WhitelistManager interface {
	UpsertWhitelistedGroup(ctx context.Context, group db.WhitelistedGroup) error
	RemoveWhitelistedGroup(ctx context.Context, chatID int64) error
	UpsertWhitelistedDMUser(ctx context.Context, user db.WhitelistedDMUser) error
	RemoveWhitelistedDMUser(ctx context.Context, userID int64) error
	ListWhitelistedGroups(ctx context.Context) ([]db.WhitelistedGroup, error)
	ListWhitelistedDMUsers(ctx context.Context) ([]db.WhitelistedDMUser, error)
}

type Handler struct {
	Auth         Authorizer
	Summary      SummaryRequester
	Variants     SummaryVariantSwitcher
	Status       StatusReporter
	Watch        WatchManager
	Whitelist    WhitelistManager
	Sender       MessageSender
	Progress     SummaryProgressReporter
	ReplyPrompts ReplyPromptStore
	Logger       *slog.Logger
}

func (h Handler) HandleMessage(ctx context.Context, message Message) error {
	if h.Sender == nil {
		return errors.New("message sender is required")
	}
	text := strings.TrimSpace(message.Text)
	if !strings.HasPrefix(text, "/") {
		return h.handlePossiblePromptReply(ctx, message)
	}
	command, err := ParseCommand(text)
	if err != nil {
		return h.reply(ctx, message, err.Error())
	}

	h.logCommandReceived(command.Name, message)
	switch command.Name {
	case "":
		return h.reply(ctx, message, helpText)
	case CommandSummarize:
		return h.handleSummarize(ctx, message, command)
	case CommandStatus:
		return h.handleStatus(ctx, message, command)
	case CommandSubscribePodcast, CommandUnsubscribePodcast, CommandSubscriptions:
		return h.handleWatchCommand(ctx, message, command)
	case CommandAllowGroup, CommandRemoveGroup, CommandAllowUser, CommandRemoveUser, CommandWhitelist:
		return h.handleWhitelistCommand(ctx, message, command)
	default:
		return h.reply(ctx, message, helpText)
	}
}

func (h Handler) HandleDefaultMessage(ctx context.Context, message Message) error {
	if h.Sender == nil {
		return errors.New("message sender is required")
	}
	text := strings.TrimSpace(message.Text)
	if !strings.HasPrefix(text, "/") {
		return h.handlePossiblePromptReply(ctx, message)
	}
	if _, err := ParseCommand(text); err != nil {
		if strings.HasPrefix(err.Error(), "unknown command") || err.Error() == "command is required" {
			return nil
		}
	}
	return h.HandleMessage(ctx, message)
}

func (h Handler) HandleStart(ctx context.Context, message Message) error {
	h.logCommandReceived(CommandName("start"), message)
	return h.reply(ctx, message, "Send /summarize_podcast <MEDIA_URL> to summarize a YouTube, Xiaoyuzhou, or SoundOn episode. Bot owners can use /subscribe_podcast <PODCAST_URL> for SoundOn or xiaoyuzhou podcast subscriptions.")
}

func (h Handler) HandleHelp(ctx context.Context, message Message) error {
	h.logCommandReceived(CommandName("help"), message)
	return h.reply(ctx, message, helpText)
}

func (h Handler) HandleSummaryVariantCallback(ctx context.Context, callback SummaryVariantCallback) error {
	if h.Auth == nil {
		return errors.New("authorizer is required")
	}
	if h.Variants == nil {
		if switcher, ok := h.Summary.(SummaryVariantSwitcher); ok {
			h.Variants = switcher
		} else {
			return errors.New("summary variant switcher is required")
		}
	}
	requestID, variant, ok := parseSummaryVariantCallbackData(callback.Data)
	if !ok {
		return nil
	}
	allowed, err := h.Auth.CanUse(ctx, callback.ChatID, callback.UserID, callback.ChatType)
	if err != nil {
		return err
	}
	if !allowed {
		h.logger().Warn("auth rejected", "command", "summary_variant", "chat_id", callback.ChatID, "chat_type", callback.ChatType, "user_id", callback.UserID)
		return nil
	}
	return h.Variants.SwitchSummaryVariant(ctx, service.SwitchSummaryVariantCommand{
		RequestID: requestID,
		ChatID:    callback.ChatID,
		UserID:    callback.UserID,
		MessageID: callback.MessageID,
		Variant:   variant,
	})
}

func parseSummaryVariantCallbackData(data string) (int64, summarize.SummaryVariant, bool) {
	rest := strings.TrimPrefix(data, summaryVariantCallbackPrefix)
	if rest == data {
		return 0, summarize.SummaryVariant{}, false
	}
	parts := strings.Split(rest, ":")
	if len(parts) != 2 {
		return 0, summarize.SummaryVariant{}, false
	}
	requestID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || requestID <= 0 {
		return 0, summarize.SummaryVariant{}, false
	}
	variant, ok := summarize.SummaryVariantByCode(parts[1])
	if !ok {
		return 0, summarize.SummaryVariant{}, false
	}
	return requestID, variant, true
}

func (h Handler) handleSummarize(ctx context.Context, message Message, command Command) error {
	allowed, err := h.ensureCanUse(ctx, message, command.Name)
	if err != nil || !allowed {
		return err
	}
	if !command.HasURL {
		return h.promptForURL(ctx, message, ReplyActionSummarize)
	}
	if command.HasPrompt {
		return h.reply(ctx, message, customPromptUnsupportedText)
	}
	_, err = h.requestSummary(ctx, message, command.URL, command.Prompt)
	return err
}

func (h Handler) requestSummary(ctx context.Context, message Message, rawURL, prompt string) (invalidURL bool, err error) {
	if h.Summary == nil {
		return false, errors.New("summary service is required")
	}

	result, err := h.Summary.RequestSummary(ctx, service.SummaryCommand{
		ChatID:    message.ChatID,
		UserID:    message.UserID,
		MessageID: message.MessageID,
		RawURL:    rawURL,
		Prompt:    prompt,
	})
	if err != nil {
		if errors.Is(err, provider.ErrInvalidURL) {
			return true, h.reply(ctx, message, service.InvalidURLNotification().Text)
		}
		h.logger().Error("summary request failed", "chat_id", message.ChatID, "user_id", message.UserID, "error", err)
		if service.IsDurableSummaryDeliveryError(err) {
			return false, nil
		}
		return false, h.reply(ctx, message, service.SummarizationFailedNotification().Text)
	}
	if result.Summarized {
		return false, nil
	}
	if h.Progress == nil {
		return false, errors.New("summary progress reporter is required")
	}
	if err := h.Progress.InitialRequestStatus(ctx, result); err != nil {
		h.logger().Warn("initial summary progress failed", "request_id", result.Request.ID, "media_id", result.Request.MediaItemID, "chat_id", result.Request.ChatID, "error", err)
		if replyErr := h.reply(ctx, message, initialSummaryStatusText(result)); replyErr != nil {
			h.logger().Warn("fallback summary acknowledgement failed", "request_id", result.Request.ID, "media_id", result.Request.MediaItemID, "chat_id", result.Request.ChatID, "error", replyErr)
		}
	}
	return false, nil
}

func initialSummaryStatusText(result service.SummaryCommandResult) string {
	if result.CreatedTranscriptionJob {
		return service.TranscriptionQueuedNotification(result.Media).Text
	}
	return service.TranscriptProcessingNotification(result.Media).Text
}

func (h Handler) handleStatus(ctx context.Context, message Message, command Command) error {
	if !command.HasURL {
		allowed, err := h.ensureCanUse(ctx, message, command.Name)
		if err != nil || !allowed {
			return err
		}
		return h.promptForURL(ctx, message, ReplyActionStatus)
	}
	_, err := h.requestStatus(ctx, message, command.URL)
	return err
}

func (h Handler) requestStatus(ctx context.Context, message Message, rawURL string) (invalidURL bool, err error) {
	if h.Status == nil {
		return false, errors.New("status service is required")
	}
	report, err := h.Status.Status(ctx, service.StatusQuery{
		ChatID:   message.ChatID,
		UserID:   message.UserID,
		ChatType: message.ChatType,
		RawURL:   rawURL,
	})
	if err != nil {
		return false, err
	}
	return report.InvalidURL, h.reply(ctx, message, report.Text)
}

func (h Handler) handlePossiblePromptReply(ctx context.Context, message Message) error {
	if message.ReplyToMessageID == 0 || h.ReplyPrompts == nil {
		return nil
	}
	prompt, ok := h.ReplyPrompts.Get(message.ChatID, message.ReplyToMessageID)
	if !ok {
		return nil
	}
	if prompt.UserID != message.UserID {
		return h.reply(ctx, message, wrongPromptUserText)
	}
	rawURL := strings.TrimSpace(message.Text)
	if rawURL == "" {
		return h.reply(ctx, message, service.InvalidURLNotification().Text)
	}

	var invalidURL bool
	var err error
	switch prompt.Action {
	case ReplyActionSummarize:
		invalidURL, err = h.handlePromptSummarize(ctx, message, rawURL)
	case ReplyActionStatus:
		invalidURL, err = h.requestStatus(ctx, message, rawURL)
	default:
		return h.reply(ctx, message, helpText)
	}
	if err != nil {
		return err
	}
	if !invalidURL {
		h.ReplyPrompts.Delete(prompt.ChatID, prompt.PromptMessageID)
	}
	return nil
}

func (h Handler) handlePromptSummarize(ctx context.Context, message Message, rawURL string) (bool, error) {
	allowed, err := h.ensureCanUse(ctx, message, CommandSummarize)
	if err != nil || !allowed {
		return false, err
	}
	return h.requestSummary(ctx, message, rawURL, "")
}

func (h Handler) ensureCanUse(ctx context.Context, message Message, command CommandName) (bool, error) {
	if h.Auth == nil {
		return false, errors.New("authorizer is required")
	}
	allowed, err := h.Auth.CanUse(ctx, message.ChatID, message.UserID, message.ChatType)
	if err != nil {
		return false, err
	}
	if !allowed {
		h.logger().Warn("auth rejected", "command", command, "chat_id", message.ChatID, "chat_type", message.ChatType, "user_id", message.UserID)
		return false, h.reply(ctx, message, service.UnauthorizedNotification().Text)
	}
	return true, nil
}

func (h Handler) promptForURL(ctx context.Context, message Message, action ReplyAction) error {
	if h.ReplyPrompts == nil {
		return errors.New("reply prompt store is required")
	}
	promptText, ok := promptText(action)
	if !ok {
		return h.reply(ctx, message, helpText)
	}
	promptMessageID, err := h.Sender.SendForceReplyText(ctx, message.ChatID, message.MessageID, promptText, urlPromptPlaceholderText)
	if err != nil {
		return err
	}
	h.ReplyPrompts.Put(PendingReply{Action: action, ChatID: message.ChatID, UserID: message.UserID, PromptMessageID: promptMessageID})
	return nil
}

func promptText(action ReplyAction) (string, bool) {
	switch action {
	case ReplyActionSummarize:
		return summarizeURLPromptText, true
	case ReplyActionStatus:
		return statusURLPromptText, true
	default:
		return "", false
	}
}

func (h Handler) handleWatchCommand(ctx context.Context, message Message, command Command) error {
	if h.Auth == nil {
		return errors.New("authorizer is required")
	}
	if !h.Auth.CanManageSubscriptions(message.UserID) {
		h.logger().Warn("auth rejected", "command", command.Name, "chat_id", message.ChatID, "chat_type", message.ChatType, "user_id", message.UserID)
		return h.reply(ctx, message, service.UnauthorizedNotification().Text)
	}
	if h.Watch == nil {
		return errors.New("watch manager is required")
	}

	var response service.WatchResponse
	var err error
	switch command.Name {
	case CommandSubscribePodcast:
		response, err = h.Watch.Subscribe(ctx, service.WatchSubscribeCommand{
			RawURL:          command.URL,
			ChatID:          message.ChatID,
			ChatType:        string(message.ChatType),
			ChatTitle:       message.ChatTitle,
			CreatedByUserID: message.UserID,
		})
	case CommandUnsubscribePodcast:
		response, err = h.Watch.Unsubscribe(ctx, service.WatchUnsubscribeCommand{RawURL: command.URL, ChatID: message.ChatID})
	case CommandSubscriptions:
		response, err = h.Watch.ListSubscriptions(ctx, message.ChatID)
	default:
		return h.reply(ctx, message, helpText)
	}
	if err != nil {
		return err
	}
	return h.reply(ctx, message, response.Text)
}

func (h Handler) handleWhitelistCommand(ctx context.Context, message Message, command Command) error {
	if h.Auth == nil {
		return errors.New("authorizer is required")
	}
	if !h.Auth.CanManageWhitelist(message.UserID) {
		h.logger().Warn("auth rejected", "command", command.Name, "chat_id", message.ChatID, "chat_type", message.ChatType, "user_id", message.UserID)
		return h.reply(ctx, message, service.UnauthorizedNotification().Text)
	}
	if h.Whitelist == nil {
		return errors.New("whitelist manager is required")
	}

	switch command.Name {
	case CommandAllowGroup:
		chatIDs, err := groupChatIDs(message, command)
		if err != nil {
			return h.reply(ctx, message, err.Error())
		}
		for _, chatID := range chatIDs {
			group := db.WhitelistedGroup{
				ChatID:          chatID,
				Title:           message.ChatTitle,
				CreatedByUserID: message.UserID,
			}
			if err := h.Whitelist.UpsertWhitelistedGroup(ctx, group); err != nil {
				return err
			}
		}
		return h.reply(ctx, message, groupReply("Allowed", chatIDs, command.SkippedChatIDs))
	case CommandRemoveGroup:
		chatIDs, err := groupChatIDs(message, command)
		if err != nil {
			return h.reply(ctx, message, err.Error())
		}
		for _, chatID := range chatIDs {
			if err := h.Whitelist.RemoveWhitelistedGroup(ctx, chatID); err != nil {
				return err
			}
		}
		return h.reply(ctx, message, groupReply("Removed", chatIDs, command.SkippedChatIDs))
	case CommandAllowUser:
		user := db.WhitelistedDMUser{
			UserID:          command.UserID,
			CreatedByUserID: message.UserID,
		}
		if err := h.Whitelist.UpsertWhitelistedDMUser(ctx, user); err != nil {
			return err
		}
		return h.reply(ctx, message, fmt.Sprintf("Allowed user %d.", command.UserID))
	case CommandRemoveUser:
		if err := h.Whitelist.RemoveWhitelistedDMUser(ctx, command.UserID); err != nil {
			return err
		}
		return h.reply(ctx, message, fmt.Sprintf("Removed user %d.", command.UserID))
	case CommandWhitelist:
		text, err := h.whitelistText(ctx)
		if err != nil {
			return err
		}
		return h.reply(ctx, message, text)
	default:
		return h.reply(ctx, message, helpText)
	}
}

func groupChatIDs(message Message, command Command) ([]int64, error) {
	if len(command.ChatIDs) > 0 {
		return dedupeChatIDs(command.ChatIDs), nil
	}
	if message.ChatType != auth.ChatTypeGroup && message.ChatType != auth.ChatTypeSupergroup {
		return nil, fmt.Errorf("usage: /%s [chat_id]", command.Name)
	}
	return []int64{message.ChatID}, nil
}

func dedupeChatIDs(chatIDs []int64) []int64 {
	seen := make(map[int64]bool, len(chatIDs))
	unique := make([]int64, 0, len(chatIDs))
	for _, id := range chatIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		unique = append(unique, id)
	}
	return unique
}

func formatChatIDs(chatIDs []int64) string {
	parts := make([]string, len(chatIDs))
	for i, id := range chatIDs {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ", ")
}

func groupReply(verb string, chatIDs []int64, skipped []string) string {
	var prefix string
	if len(chatIDs) == 1 {
		prefix = fmt.Sprintf("%s group %d", verb, chatIDs[0])
	} else {
		prefix = verb + " groups: " + formatChatIDs(chatIDs)
	}
	text := prefix + "."
	if len(skipped) > 0 {
		text += " Skipped: " + strings.Join(skipped, ", ") + "."
	}
	return text
}

func (h Handler) whitelistText(ctx context.Context) (string, error) {
	groups, err := h.Whitelist.ListWhitelistedGroups(ctx)
	if err != nil {
		return "", err
	}
	users, err := h.Whitelist.ListWhitelistedDMUsers(ctx)
	if err != nil {
		return "", err
	}
	var builder strings.Builder
	builder.WriteString("Whitelisted groups:")
	if len(groups) == 0 {
		builder.WriteString(" none")
	}
	for _, group := range groups {
		fmt.Fprintf(&builder, "\n- %d", group.ChatID)
		if group.Title != "" {
			builder.WriteString(" ")
			builder.WriteString(group.Title)
		}
	}
	builder.WriteString("\nWhitelisted users:")
	if len(users) == 0 {
		builder.WriteString(" none")
	}
	for _, user := range users {
		fmt.Fprintf(&builder, "\n- %d", user.UserID)
		if user.Username != "" {
			builder.WriteString(" @")
			builder.WriteString(user.Username)
		}
	}
	return builder.String(), nil
}

func (h Handler) reply(ctx context.Context, message Message, text string) error {
	_, err := h.Sender.SendReplyText(ctx, message.ChatID, message.MessageID, strings.TrimSpace(text))
	return err
}

func (h Handler) logCommandReceived(command CommandName, message Message) {
	h.logger().Info("command received", "command", command, "chat_id", message.ChatID, "chat_type", message.ChatType, "user_id", message.UserID, "message_id", message.MessageID)
}

func (h Handler) logger() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}
