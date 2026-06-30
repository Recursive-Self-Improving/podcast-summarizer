package bot

import (
	"context"
	"errors"
	"fmt"
	"os"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/auth"
)

type Registrar interface {
	RegisterHandler(handlerType telegram.HandlerType, pattern string, matchType telegram.MatchType, f telegram.HandlerFunc, m ...telegram.Middleware) string
}

type TelegramSenderClient struct {
	Bot *telegram.Bot
}

func (c TelegramSenderClient) SendMessage(ctx context.Context, chatID int64, text string) (int64, error) {
	return c.sendMessage(ctx, chatID, 0, text, "", nil)
}

func (c TelegramSenderClient) SendReply(ctx context.Context, chatID, replyToMessageID int64, text string) (int64, error) {
	return c.sendMessage(ctx, chatID, replyToMessageID, text, "", nil)
}

func (c TelegramSenderClient) SendForceReply(ctx context.Context, chatID, replyToMessageID int64, text, placeholder string) (int64, error) {
	return c.sendMessage(ctx, chatID, replyToMessageID, text, "", &models.ForceReply{ForceReply: true, InputFieldPlaceholder: placeholder, Selective: true})
}

func (c TelegramSenderClient) SendHTMLMessage(ctx context.Context, chatID int64, text string) (int64, error) {
	return c.sendMessage(ctx, chatID, 0, text, models.ParseModeHTML, nil)
}

func (c TelegramSenderClient) SendHTMLReply(ctx context.Context, chatID, replyToMessageID int64, text string) (int64, error) {
	return c.sendMessage(ctx, chatID, replyToMessageID, text, models.ParseModeHTML, nil)
}

func (c TelegramSenderClient) SendHTMLMessageWithMarkup(ctx context.Context, chatID int64, text string, markup any) (int64, error) {
	return c.sendMessage(ctx, chatID, 0, text, models.ParseModeHTML, markup)
}

func (c TelegramSenderClient) SendHTMLReplyWithMarkup(ctx context.Context, chatID, replyToMessageID int64, text string, markup any) (int64, error) {
	return c.sendMessage(ctx, chatID, replyToMessageID, text, models.ParseModeHTML, markup)
}

func (c TelegramSenderClient) EditHTMLMessage(ctx context.Context, chatID, messageID int64, text string, markup any) error {
	if c.Bot == nil {
		return errors.New("telegram bot is required")
	}
	_, err := c.Bot.EditMessageText(ctx, &telegram.EditMessageTextParams{ChatID: chatID, MessageID: int(messageID), Text: text, ParseMode: models.ParseModeHTML, ReplyMarkup: markup})
	return err
}

func (c TelegramSenderClient) SendDocument(ctx context.Context, chatID int64, path string) (int64, error) {
	return c.sendDocument(ctx, chatID, 0, path)
}

func (c TelegramSenderClient) SendReplyDocument(ctx context.Context, chatID, replyToMessageID int64, path string) (int64, error) {
	return c.sendDocument(ctx, chatID, replyToMessageID, path)
}

func (c TelegramSenderClient) DeleteMessage(ctx context.Context, chatID, messageID int64) error {
	if c.Bot == nil {
		return errors.New("telegram bot is required")
	}
	_, err := c.Bot.DeleteMessage(ctx, &telegram.DeleteMessageParams{ChatID: chatID, MessageID: int(messageID)})
	return err
}

func (c TelegramSenderClient) sendMessage(ctx context.Context, chatID, replyToMessageID int64, text string, parseMode models.ParseMode, replyMarkup models.ReplyMarkup) (int64, error) {
	if c.Bot == nil {
		return 0, errors.New("telegram bot is required")
	}
	message, err := c.Bot.SendMessage(ctx, &telegram.SendMessageParams{ChatID: chatID, Text: text, ParseMode: parseMode, ReplyParameters: replyParameters(replyToMessageID), ReplyMarkup: replyMarkup})
	if err != nil {
		return 0, err
	}
	return int64(message.ID), nil
}

func (c TelegramSenderClient) sendDocument(ctx context.Context, chatID, replyToMessageID int64, path string) (int64, error) {
	if c.Bot == nil {
		return 0, errors.New("telegram bot is required")
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open telegram document: %w", err)
	}
	defer file.Close()
	message, err := c.Bot.SendDocument(ctx, &telegram.SendDocumentParams{
		ChatID:          chatID,
		Document:        &models.InputFileUpload{Filename: "summary.txt", Data: file},
		ReplyParameters: replyParameters(replyToMessageID),
	})
	if err != nil {
		return 0, err
	}
	return int64(message.ID), nil
}

func replyParameters(messageID int64) *models.ReplyParameters {
	if messageID == 0 {
		return nil
	}
	return &models.ReplyParameters{MessageID: int(messageID)}
}

func NewTelegramBot(token string, handler Handler, skipOldUpdates bool) (*telegram.Bot, error) {
	if handler.ReplyPrompts == nil {
		handler.ReplyPrompts = NewMemoryReplyPromptStore()
	}
	options := []telegram.Option{telegram.WithDefaultHandler(DefaultHandler(&handler))}
	if skipOldUpdates {
		options = append(options, telegram.WithInitialOffset(-1))
	}
	bot, err := telegram.New(token, options...)
	if err != nil {
		return nil, err
	}
	RegisterHandlers(bot, handler)
	return bot, nil
}

func DefaultHandler(handler *Handler) telegram.HandlerFunc {
	return func(ctx context.Context, _ *telegram.Bot, update *models.Update) {
		if handler == nil {
			return
		}
		if message, ok := telegramMessage(update); ok {
			handler.handleTelegramError(ctx, message, handler.HandleDefaultMessage(ctx, message))
		}
	}
}

func RegisterHandlers(registrar Registrar, handler Handler) {
	registrar.RegisterHandler(telegram.HandlerTypeMessageText, "start", telegram.MatchTypeCommandStartOnly, handler.telegramHandleStart)
	registrar.RegisterHandler(telegram.HandlerTypeMessageText, "help", telegram.MatchTypeCommandStartOnly, handler.telegramHandleHelp)

	commands := []CommandName{
		CommandSummarize,
		CommandSubscribePodcast,
		CommandUnsubscribePodcast,
		CommandSubscriptions,
		CommandAllowGroup,
		CommandRemoveGroup,
		CommandAllowUser,
		CommandRemoveUser,
		CommandWhitelist,
		CommandStatus,
	}
	for _, command := range commands {
		registrar.RegisterHandler(telegram.HandlerTypeMessageText, string(command), telegram.MatchTypeCommandStartOnly, handler.telegramHandleMessage)
	}
	registrar.RegisterHandler(telegram.HandlerTypeCallbackQueryData, summaryVariantCallbackPrefix, telegram.MatchTypePrefix, handler.telegramHandleSummaryVariantCallback)
}

func (h Handler) telegramHandleStart(ctx context.Context, _ *telegram.Bot, update *models.Update) {
	if message, ok := telegramMessage(update); ok {
		h.handleTelegramError(ctx, message, h.HandleStart(ctx, message))
	}
}

func (h Handler) telegramHandleHelp(ctx context.Context, _ *telegram.Bot, update *models.Update) {
	if message, ok := telegramMessage(update); ok {
		h.handleTelegramError(ctx, message, h.HandleHelp(ctx, message))
	}
}

func (h Handler) telegramHandleMessage(ctx context.Context, _ *telegram.Bot, update *models.Update) {
	if message, ok := telegramMessage(update); ok {
		h.handleTelegramError(ctx, message, h.HandleMessage(ctx, message))
	}
}

func (h Handler) telegramHandleSummaryVariantCallback(ctx context.Context, b *telegram.Bot, update *models.Update) {
	if update == nil || update.CallbackQuery == nil {
		return
	}
	if b != nil {
		_, _ = b.AnswerCallbackQuery(ctx, &telegram.AnswerCallbackQueryParams{CallbackQueryID: update.CallbackQuery.ID, Text: "处理中..."})
	}
	if err := h.HandleSummaryVariantCallback(ctx, callbackQuery(update.CallbackQuery)); err != nil {
		h.logger().Warn("summary variant callback failed", "callback_id", update.CallbackQuery.ID, "error", err)
		if b != nil {
			_, _ = b.AnswerCallbackQuery(ctx, &telegram.AnswerCallbackQueryParams{CallbackQueryID: update.CallbackQuery.ID, Text: "切换失败"})
		}
	}
}

func (h Handler) handleTelegramError(ctx context.Context, message Message, err error) {
	if err == nil {
		return
	}
	h.logger().Error("telegram handler failed", "chat_id", message.ChatID, "user_id", message.UserID, "message_id", message.MessageID, "error", err)
	if h.Sender == nil || ctx.Err() != nil || isDeliveryClassifiedError(err) {
		return
	}
	if _, sendErr := h.Sender.SendReplyText(ctx, message.ChatID, message.MessageID, "Sorry, something went wrong. Please try again later."); sendErr != nil {
		h.logger().Error("telegram handler failure reply failed", "chat_id", message.ChatID, "error", sendErr)
	}
}

func isDeliveryClassifiedError(err error) bool {
	type ambiguousDelivery interface {
		AmbiguousDelivery() bool
	}
	type terminalDeliveryFailure interface {
		TerminalDeliveryFailure() bool
	}
	var ambiguous ambiguousDelivery
	if errors.As(err, &ambiguous) && ambiguous.AmbiguousDelivery() {
		return true
	}
	var terminal terminalDeliveryFailure
	return errors.As(err, &terminal) && terminal.TerminalDeliveryFailure()
}

func telegramMessage(update *models.Update) (Message, bool) {
	if update == nil || update.Message == nil || update.Message.From == nil {
		return Message{}, false
	}

	message := update.Message
	chat := message.Chat
	from := message.From
	replyToMessageID := int64(0)
	if message.ReplyToMessage != nil {
		replyToMessageID = int64(message.ReplyToMessage.ID)
	}
	return Message{
		Text:             message.Text,
		ChatID:           chat.ID,
		ChatType:         auth.ChatType(chat.Type),
		ChatTitle:        chat.Title,
		UserID:           from.ID,
		Username:         from.Username,
		FirstName:        from.FirstName,
		MessageID:        int64(message.ID),
		ReplyToMessageID: replyToMessageID,
	}, true
}

func callbackQuery(query *models.CallbackQuery) SummaryVariantCallback {
	callback := SummaryVariantCallback{ID: query.ID, Data: query.Data, UserID: query.From.ID}
	if query.Message.Message != nil {
		message := query.Message.Message
		callback.ChatID = message.Chat.ID
		callback.ChatType = auth.ChatType(message.Chat.Type)
		callback.MessageID = int64(message.ID)
	}
	return callback
}
