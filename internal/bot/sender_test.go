package bot

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSenderSendsShortText(t *testing.T) {
	client := &fakeSenderClient{}
	sender := Sender{Client: client, TempDir: t.TempDir()}
	if err := sender.SendText(context.Background(), 123, "short summary"); err != nil {
		t.Fatalf("SendText returned error: %v", err)
	}
	if len(client.messages) != 1 || client.messages[0] != "short summary" {
		t.Fatalf("messages = %#v", client.messages)
	}
	if len(client.htmlMessages) != 0 {
		t.Fatalf("html messages = %#v", client.htmlMessages)
	}
	if len(client.documents) != 0 {
		t.Fatalf("documents = %#v", client.documents)
	}
}

func TestSenderSendsForceReplyText(t *testing.T) {
	client := &fakeSenderClient{}
	sender := Sender{Client: client, TempDir: t.TempDir()}

	messageID, err := sender.SendForceReplyText(context.Background(), 123, 456, "send URL", "Paste URL")
	if err != nil {
		t.Fatalf("SendForceReplyText returned error: %v", err)
	}
	if messageID != 1 {
		t.Fatalf("message ID = %d", messageID)
	}
	if len(client.forceReplies) != 1 || client.forceReplies[0] != 456 || client.forcePlaceholders[0] != "Paste URL" {
		t.Fatalf("force replies = %#v placeholders = %#v", client.forceReplies, client.forcePlaceholders)
	}
}

func TestSenderSendsFinalSummaryAsHTML(t *testing.T) {
	client := &fakeSenderClient{}
	sender := Sender{Client: client, TempDir: t.TempDir()}
	summary := simplifiedInvestmentSummary("summary", "missed", "explicit", "implicit", "stocks")

	messageID, err := sender.SendFinalSummary(context.Background(), 123, 456, summary)
	if err != nil {
		t.Fatalf("SendFinalSummary returned error: %v", err)
	}
	if messageID != 1 {
		t.Fatalf("message ID = %d", messageID)
	}
	if len(client.htmlMessages) != 2 || client.htmlReplies[0] != 456 || client.htmlReplies[1] != 456 {
		t.Fatalf("html messages = %#v replies = %#v", client.htmlMessages, client.htmlReplies)
	}
	if !strings.Contains(client.htmlMessages[0], "<blockquote expandable>") {
		t.Fatalf("html message missing expandable blockquote: %s", client.htmlMessages[0])
	}
	if !strings.Contains(client.htmlMessages[1], simplifiedPlaceholderSummary) {
		t.Fatalf("second html message missing placeholder: %s", client.htmlMessages[1])
	}
	if len(client.htmlMarkups) != 2 || client.htmlMarkups[0] != nil || client.htmlMarkups[1] == nil {
		t.Fatalf("html markups = %#v", client.htmlMarkups)
	}
	if len(client.messages) != 0 {
		t.Fatalf("plain messages = %#v", client.messages)
	}
}

func TestSenderFinalSummaryFallsBackToPlainMessageWhenReplyTargetIsMissing(t *testing.T) {
	client := &fakeSenderClient{htmlReplyErr: errors.New("Bad Request: reply message not found")}
	sender := Sender{Client: client, TempDir: t.TempDir()}

	messageID, err := sender.SendFinalSummary(context.Background(), 123, 456, "plain fallback summary")
	if err != nil {
		t.Fatalf("SendFinalSummary returned error: %v", err)
	}
	if messageID != 1 {
		t.Fatalf("message ID = %d", messageID)
	}
	if len(client.htmlMessages) != 2 || client.htmlReplies[0] != 0 || client.htmlReplies[1] != 0 {
		t.Fatalf("html messages = %#v replies = %#v", client.htmlMessages, client.htmlReplies)
	}
}

func TestSenderEditsFinalSummaryPartsWithKeyboardOnSecondMessage(t *testing.T) {
	client := &fakeSenderClient{}
	sender := Sender{Client: client, TempDir: t.TempDir()}
	summary := simplifiedInvestmentSummary("summary", "missed", "explicit", "implicit", "stocks")

	if err := sender.EditFinalSummaryParts(context.Background(), 123, []int64{10, 11}, summary, 42); err != nil {
		t.Fatalf("EditFinalSummaryParts returned error: %v", err)
	}
	if len(client.edits) != 2 {
		t.Fatalf("edits = %#v", client.edits)
	}
	if client.edits[0].messageID != 10 || client.edits[0].markup != nil {
		t.Fatalf("first edit = %#v", client.edits[0])
	}
	if client.edits[1].messageID != 11 || client.edits[1].markup == nil {
		t.Fatalf("second edit = %#v", client.edits[1])
	}
}

func TestSenderSplitsLongText(t *testing.T) {
	client := &fakeSenderClient{}
	sender := Sender{Client: client, TempDir: t.TempDir()}
	text := strings.Repeat("a", 2000) + "\n\n" + strings.Repeat("b", 2000)
	if err := sender.SendText(context.Background(), 123, text); err != nil {
		t.Fatalf("SendText returned error: %v", err)
	}
	if len(client.messages) != 2 {
		t.Fatalf("messages = %#v", client.messages)
	}
	for _, message := range client.messages {
		if runeLen(message) > maxTelegramTextChars {
			t.Fatalf("oversized message length %d", runeLen(message))
		}
		if !strings.HasPrefix(message, "Summary ") {
			t.Fatalf("message missing prefix: %q", message[:20])
		}
	}
}

func TestSenderHardSplitsOversizedChunk(t *testing.T) {
	client := &fakeSenderClient{}
	sender := Sender{Client: client, TempDir: t.TempDir()}
	text := strings.Repeat("x", maxTelegramTextChars*2+10)
	if err := sender.SendText(context.Background(), 123, text); err != nil {
		t.Fatalf("SendText returned error: %v", err)
	}
	if len(client.messages) != 3 {
		t.Fatalf("messages = %#v", client.messages)
	}
	for _, message := range client.messages {
		if runeLen(message) > maxTelegramTextChars {
			t.Fatalf("unexpected oversized message length %d", runeLen(message))
		}
	}
}

func TestSenderPrefixReserveConvergesAcrossDigitBoundary(t *testing.T) {
	client := &fakeSenderClient{}
	sender := Sender{Client: client, TempDir: t.TempDir()}
	text := strings.Repeat("x", maxTelegramTextChars*9)
	if err := sender.SendText(context.Background(), 123, text); err != nil {
		t.Fatalf("SendText returned error: %v", err)
	}
	if len(client.messages) < 10 {
		t.Fatalf("expected digit-boundary split, got %d messages", len(client.messages))
	}
	for _, message := range client.messages {
		if runeLen(message) > maxTelegramTextChars {
			t.Fatalf("oversized message length %d", runeLen(message))
		}
	}
}

func TestSenderHardSplitsNonASCIIWithoutInvalidUTF8(t *testing.T) {
	client := &fakeSenderClient{}
	sender := Sender{Client: client, TempDir: t.TempDir()}
	text := strings.Repeat("界", maxTelegramTextChars+10)
	if err := sender.SendText(context.Background(), 123, text); err != nil {
		t.Fatalf("SendText returned error: %v", err)
	}
	if len(client.messages) != 2 {
		t.Fatalf("messages = %#v", client.messages)
	}
	for _, message := range client.messages {
		if !utf8.ValidString(message) {
			t.Fatalf("invalid UTF-8 message: %q", message)
		}
		if runeLen(message) > maxTelegramTextChars {
			t.Fatalf("oversized message length %d", runeLen(message))
		}
	}
}

func TestSenderDocumentFallback(t *testing.T) {
	client := &fakeSenderClient{messageErr: errors.New("Bad Request: message is too long")}
	tempDir := t.TempDir()
	sender := Sender{Client: client, TempDir: tempDir}
	if err := sender.SendText(context.Background(), 123, "summary text"); err != nil {
		t.Fatalf("SendText returned error: %v", err)
	}
	if len(client.documents) != 1 {
		t.Fatalf("documents = %#v", client.documents)
	}
	if client.documentContents[0] != "summary text" {
		t.Fatalf("document content = %q", client.documentContents[0])
	}
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("ReadDir returned error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("temp files not deleted: %#v", entries)
	}
}

func TestSenderDoesNotFallbackAfterAmbiguousFirstMessageFailure(t *testing.T) {
	client := &fakeSenderClient{messageErr: errors.New("network failed")}
	sender := Sender{Client: client, TempDir: t.TempDir()}

	err := sender.SendText(context.Background(), 123, "summary text")
	if err == nil {
		t.Fatal("SendText returned nil error")
	}
	if !isAmbiguousDelivery(t, err) {
		t.Fatalf("SendText error is not ambiguous: %v", err)
	}
	if len(client.documents) != 0 {
		t.Fatalf("documents = %#v", client.documents)
	}
}

func TestSenderTreatsTelegramAPIRejectionAsTerminal(t *testing.T) {
	client := &fakeSenderClient{messageErr: errors.New("Bad Request: chat not found")}
	sender := Sender{Client: client, TempDir: t.TempDir()}

	err := sender.SendText(context.Background(), 123, "summary text")
	if err == nil {
		t.Fatal("SendText returned nil error")
	}
	if isAmbiguousDelivery(t, err) {
		t.Fatalf("SendText error is ambiguous: %v", err)
	}
	if !isTerminalDeliveryFailure(t, err) {
		t.Fatalf("SendText error is not terminal: %v", err)
	}
	if len(client.documents) != 0 {
		t.Fatalf("documents = %#v", client.documents)
	}
}

func TestSenderTreatsRateLimitAsNonAmbiguous(t *testing.T) {
	client := &fakeSenderClient{messageErr: errors.New("Too Many Requests: retry after 3")}
	sender := Sender{Client: client, TempDir: t.TempDir()}

	err := sender.SendText(context.Background(), 123, "summary text")
	if err == nil {
		t.Fatal("SendText returned nil error")
	}
	if isAmbiguousDelivery(t, err) {
		t.Fatalf("SendText error is ambiguous: %v", err)
	}
	if isTerminalDeliveryFailure(t, err) {
		t.Fatalf("SendText error is terminal: %v", err)
	}
}

func TestSenderFallsBackToPlainMessageWhenReplyTargetIsMissing(t *testing.T) {
	client := &fakeSenderClient{replyErr: errors.New("Bad Request: reply message not found")}
	sender := Sender{Client: client, TempDir: t.TempDir()}

	messageID, err := sender.SendReplyText(context.Background(), 123, 456, "summary text")
	if err != nil {
		t.Fatalf("SendReplyText returned error: %v", err)
	}
	if messageID != 1 {
		t.Fatalf("message ID = %d", messageID)
	}
	if len(client.messages) != 1 || client.messages[0] != "summary text" || client.replies[0] != 0 {
		t.Fatalf("messages = %#v replies = %#v", client.messages, client.replies)
	}
}

func TestSenderFallsBackToPlainDocumentWhenReplyTargetIsMissing(t *testing.T) {
	client := &fakeSenderClient{messageErr: errors.New("Bad Request: message is too long"), documentReplyErr: errors.New("Bad Request: message to be replied not found")}
	sender := Sender{Client: client, TempDir: t.TempDir()}

	messageID, err := sender.SendReplyText(context.Background(), 123, 456, "summary text")
	if err != nil {
		t.Fatalf("SendReplyText returned error: %v", err)
	}
	if messageID != 1 {
		t.Fatalf("message ID = %d", messageID)
	}
	if len(client.documents) != 1 || client.documentContents[0] != "summary text" || client.documentReplies[0] != 0 {
		t.Fatalf("documents = %#v replies = %#v contents = %#v", client.documents, client.documentReplies, client.documentContents)
	}
}

func TestSenderClassifiesDocumentFallbackFailure(t *testing.T) {
	for _, tc := range []struct {
		name      string
		docErr    error
		ambiguous bool
	}{
		{name: "api rejection", docErr: errors.New("Forbidden: bot was blocked by the user"), ambiguous: false},
		{name: "transport failure", docErr: errors.New("network failed"), ambiguous: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeSenderClient{messageErr: errors.New("Bad Request: message is too long"), documentErr: tc.docErr}
			sender := Sender{Client: client, TempDir: t.TempDir()}

			err := sender.SendText(context.Background(), 123, "summary text")
			if err == nil {
				t.Fatal("SendText returned nil error")
			}
			if isAmbiguousDelivery(t, err) != tc.ambiguous {
				t.Fatalf("ambiguity = %v, want %v: %v", isAmbiguousDelivery(t, err), tc.ambiguous, err)
			}
			if isTerminalDeliveryFailure(t, err) == tc.ambiguous {
				t.Fatalf("terminal = %v, ambiguous = %v: %v", isTerminalDeliveryFailure(t, err), tc.ambiguous, err)
			}
		})
	}
}

func TestSenderDoesNotFallbackAfterPartialChunkDelivery(t *testing.T) {
	client := &fakeSenderClient{messageErrAfter: 1, messageErr: errors.New("network failed")}
	sender := Sender{Client: client, TempDir: t.TempDir()}
	text := strings.Repeat("a", 2000) + "\n\n" + strings.Repeat("b", 2000)

	err := sender.SendText(context.Background(), 123, text)
	if err == nil {
		t.Fatal("SendText returned nil error")
	}
	if !isAmbiguousDelivery(t, err) {
		t.Fatalf("SendText error is not ambiguous: %v", err)
	}
	if len(client.messages) != 1 {
		t.Fatalf("messages = %#v", client.messages)
	}
	if len(client.documents) != 0 {
		t.Fatalf("documents = %#v", client.documents)
	}
}

func isAmbiguousDelivery(t *testing.T, err error) bool {
	t.Helper()
	type ambiguousDelivery interface {
		AmbiguousDelivery() bool
	}
	var ambiguous ambiguousDelivery
	if !errors.As(err, &ambiguous) {
		t.Fatalf("error does not expose ambiguity: %v", err)
	}
	return ambiguous.AmbiguousDelivery()
}

func isTerminalDeliveryFailure(t *testing.T, err error) bool {
	t.Helper()
	type terminalDeliveryFailure interface {
		TerminalDeliveryFailure() bool
	}
	var terminal terminalDeliveryFailure
	if !errors.As(err, &terminal) {
		t.Fatalf("error does not expose terminal delivery failure: %v", err)
	}
	return terminal.TerminalDeliveryFailure()
}

type fakeSenderClient struct {
	messageErr        error
	replyErr          error
	htmlErr           error
	htmlReplyErr      error
	messageErrAfter   int
	documentErr       error
	documentReplyErr  error
	messages          []string
	replies           []int64
	htmlMessages      []string
	htmlReplies       []int64
	documents         []string
	documentReplies   []int64
	documentContents  []string
	forceReplies      []int64
	forcePlaceholders []string
	deleted           []int64
	htmlMarkups       []any
	edits             []editedHTMLMessage
}

type editedHTMLMessage struct {
	messageID int64
	text      string
	markup    any
}

func (f *fakeSenderClient) SendMessage(_ context.Context, _ int64, text string) (int64, error) {
	return f.sendMessage(0, text)
}

func (f *fakeSenderClient) SendReply(_ context.Context, _ int64, replyToMessageID int64, text string) (int64, error) {
	return f.sendMessage(replyToMessageID, text)
}

func (f *fakeSenderClient) SendForceReply(_ context.Context, _ int64, replyToMessageID int64, text, placeholder string) (int64, error) {
	messageID, err := f.sendMessage(replyToMessageID, text)
	if err != nil {
		return 0, err
	}
	f.forceReplies = append(f.forceReplies, replyToMessageID)
	f.forcePlaceholders = append(f.forcePlaceholders, placeholder)
	return messageID, nil
}

func (f *fakeSenderClient) SendHTMLMessage(_ context.Context, _ int64, text string) (int64, error) {
	return f.sendHTMLMessage(0, text)
}

func (f *fakeSenderClient) SendHTMLReply(_ context.Context, _ int64, replyToMessageID int64, text string) (int64, error) {
	return f.sendHTMLMessage(replyToMessageID, text)
}

func (f *fakeSenderClient) SendHTMLMessageWithMarkup(_ context.Context, _ int64, text string, markup any) (int64, error) {
	return f.sendHTMLMessageWithMarkup(0, text, markup)
}

func (f *fakeSenderClient) SendHTMLReplyWithMarkup(_ context.Context, _ int64, replyToMessageID int64, text string, markup any) (int64, error) {
	return f.sendHTMLMessageWithMarkup(replyToMessageID, text, markup)
}

func (f *fakeSenderClient) EditHTMLMessage(_ context.Context, _ int64, messageID int64, text string, markup any) error {
	f.edits = append(f.edits, editedHTMLMessage{messageID: messageID, text: text, markup: markup})
	return nil
}

func (f *fakeSenderClient) SendDocument(_ context.Context, _ int64, path string) (int64, error) {
	return f.sendDocument(0, path)
}

func (f *fakeSenderClient) SendReplyDocument(_ context.Context, _ int64, replyToMessageID int64, path string) (int64, error) {
	return f.sendDocument(replyToMessageID, path)
}

func (f *fakeSenderClient) DeleteMessage(_ context.Context, _ int64, messageID int64) error {
	f.deleted = append(f.deleted, messageID)
	return nil
}

func (f *fakeSenderClient) sendMessage(replyToMessageID int64, text string) (int64, error) {
	if replyToMessageID != 0 && f.replyErr != nil {
		return 0, f.replyErr
	}
	if f.messageErr != nil && (f.messageErrAfter == 0 || len(f.messages) >= f.messageErrAfter) {
		return 0, f.messageErr
	}
	f.messages = append(f.messages, text)
	f.replies = append(f.replies, replyToMessageID)
	return int64(len(f.messages)), nil
}

func (f *fakeSenderClient) sendHTMLMessage(replyToMessageID int64, text string) (int64, error) {
	return f.sendHTMLMessageWithMarkup(replyToMessageID, text, nil)
}

func (f *fakeSenderClient) sendHTMLMessageWithMarkup(replyToMessageID int64, text string, markup any) (int64, error) {
	if replyToMessageID != 0 && f.htmlReplyErr != nil {
		return 0, f.htmlReplyErr
	}
	if f.htmlErr != nil && (f.messageErrAfter == 0 || len(f.htmlMessages) >= f.messageErrAfter) {
		return 0, f.htmlErr
	}
	f.htmlMessages = append(f.htmlMessages, text)
	f.htmlReplies = append(f.htmlReplies, replyToMessageID)
	f.htmlMarkups = append(f.htmlMarkups, markup)
	return int64(len(f.htmlMessages)), nil
}

func (f *fakeSenderClient) sendDocument(replyToMessageID int64, path string) (int64, error) {
	if replyToMessageID != 0 && f.documentReplyErr != nil {
		return 0, f.documentReplyErr
	}
	if f.documentErr != nil {
		return 0, f.documentErr
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	f.documents = append(f.documents, path)
	f.documentReplies = append(f.documentReplies, replyToMessageID)
	f.documentContents = append(f.documentContents, string(contents))
	return int64(len(f.documents)), nil
}
