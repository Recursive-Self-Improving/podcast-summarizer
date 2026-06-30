package bot

import (
	"testing"

	"github.com/go-telegram/bot/models"
)

func TestTelegramMessageIncludesReplyToMessageID(t *testing.T) {
	message, ok := telegramMessage(&models.Update{Message: &models.Message{
		ID:             31,
		Text:           "https://youtu.be/abc12345678",
		Chat:           models.Chat{ID: 10, Type: models.ChatTypePrivate},
		From:           &models.User{ID: 20, Username: "alice", FirstName: "Alice"},
		ReplyToMessage: &models.Message{ID: 1},
	}})
	if !ok {
		t.Fatal("telegramMessage returned false")
	}
	if message.MessageID != 31 || message.ReplyToMessageID != 1 || message.ChatID != 10 || message.UserID != 20 {
		t.Fatalf("message = %#v", message)
	}
}

func TestTelegramMessageWithoutReplyHasZeroReplyToMessageID(t *testing.T) {
	message, ok := telegramMessage(&models.Update{Message: &models.Message{
		ID:   31,
		Text: "/help",
		Chat: models.Chat{ID: 10, Type: models.ChatTypePrivate},
		From: &models.User{ID: 20},
	}})
	if !ok {
		t.Fatal("telegramMessage returned false")
	}
	if message.ReplyToMessageID != 0 {
		t.Fatalf("message = %#v", message)
	}
}
