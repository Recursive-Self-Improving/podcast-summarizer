package bot

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	telegram "github.com/go-telegram/bot"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/auth"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/db"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/provider"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/service"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/summarize"
)

func TestHandlerRoutesSummarize(t *testing.T) {
	sender := &fakeMessageSender{}
	summary := &fakeSummaryRequester{result: service.SummaryCommandResult{
		Media:                   db.MediaItem{CanonicalURL: "https://www.youtube.com/watch?v=abc12345678"},
		CreatedTranscriptionJob: true,
	}}
	progress := &fakeProgressReporter{}
	handler := Handler{Auth: &fakeAuthorizer{allowed: true}, Summary: summary, Sender: sender, Progress: progress}

	err := handler.HandleMessage(context.Background(), testMessage("/summarize_podcast https://youtu.be/abc12345678"))
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	if len(summary.commands) != 1 {
		t.Fatalf("commands = %#v", summary.commands)
	}
	command := summary.commands[0]
	if command.RawURL != "https://youtu.be/abc12345678" || command.Prompt != "" {
		t.Fatalf("command = %#v", command)
	}
	if len(progress.results) != 1 || !progress.results[0].CreatedTranscriptionJob {
		t.Fatalf("progress results = %#v", progress.results)
	}
}

func TestHandlerSuppressesInitialProgressFailure(t *testing.T) {
	sender := &fakeMessageSender{}
	summary := &fakeSummaryRequester{result: service.SummaryCommandResult{
		Media:   db.MediaItem{CanonicalURL: "https://www.youtube.com/watch?v=abc12345678"},
		Request: db.SummaryRequest{ID: 1, MediaItemID: 2, ChatID: 10},
	}}
	progress := &fakeProgressReporter{err: errors.New("telegram failed")}
	handler := Handler{Auth: &fakeAuthorizer{allowed: true}, Summary: summary, Sender: sender, Progress: progress}

	err := handler.HandleMessage(context.Background(), testMessage("/summarize_podcast https://youtu.be/abc12345678"))
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	if len(sender.messages) != 1 || !strings.Contains(sender.messages[0], "already being transcribed") {
		t.Fatalf("messages = %#v", sender.messages)
	}
	if len(progress.results) != 1 {
		t.Fatalf("progress results = %#v", progress.results)
	}
}

func TestHandlerRejectsCustomPrompt(t *testing.T) {
	sender := &fakeMessageSender{}
	summary := &fakeSummaryRequester{}
	handler := Handler{Auth: &fakeAuthorizer{allowed: true}, Summary: summary, Sender: sender}

	err := handler.HandleMessage(context.Background(), testMessage("/summarize_podcast https://youtu.be/abc12345678 custom prompt"))
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	if len(summary.commands) != 0 {
		t.Fatalf("commands = %#v", summary.commands)
	}
	if len(sender.messages) != 1 || sender.messages[0] != customPromptUnsupportedText {
		t.Fatalf("messages = %#v", sender.messages)
	}
}

func TestHandlerRejectsUnauthorizedSummarize(t *testing.T) {
	sender := &fakeMessageSender{}
	summary := &fakeSummaryRequester{}
	handler := Handler{Auth: &fakeAuthorizer{allowed: false}, Summary: summary, Sender: sender}

	err := handler.HandleMessage(context.Background(), testMessage("/summarize_podcast https://youtu.be/abc12345678"))
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	if len(summary.commands) != 0 {
		t.Fatalf("commands = %#v", summary.commands)
	}
	if len(sender.messages) != 1 || sender.messages[0] != service.UnauthorizedNotification().Text {
		t.Fatalf("messages = %#v", sender.messages)
	}
}

func TestHandlerRoutesAuthorizedSummaryVariantCallback(t *testing.T) {
	switcher := &fakeSummaryVariantSwitcher{}
	authz := &fakeAuthorizer{allowed: true}
	handler := Handler{Auth: authz, Variants: switcher}

	err := handler.HandleSummaryVariantCallback(context.Background(), SummaryVariantCallback{
		Data:      summaryVariantCallbackData(42, summarize.VariantTraditional),
		ChatID:    10,
		ChatType:  auth.ChatTypePrivate,
		UserID:    20,
		MessageID: 99,
	})
	if err != nil {
		t.Fatalf("HandleSummaryVariantCallback returned error: %v", err)
	}
	if len(switcher.commands) != 1 {
		t.Fatalf("commands = %#v", switcher.commands)
	}
	command := switcher.commands[0]
	if command.RequestID != 42 || command.MessageID != 99 || command.Variant != summarize.VariantTraditional {
		t.Fatalf("command = %#v", command)
	}
}

func TestHandlerRejectsUnauthorizedSummaryVariantCallback(t *testing.T) {
	switcher := &fakeSummaryVariantSwitcher{}
	handler := Handler{Auth: &fakeAuthorizer{allowed: false}, Variants: switcher}

	err := handler.HandleSummaryVariantCallback(context.Background(), SummaryVariantCallback{
		Data:      summaryVariantCallbackData(42, summarize.VariantTraditional),
		ChatID:    10,
		ChatType:  auth.ChatTypePrivate,
		UserID:    20,
		MessageID: 99,
	})
	if err != nil {
		t.Fatalf("HandleSummaryVariantCallback returned error: %v", err)
	}
	if len(switcher.commands) != 0 {
		t.Fatalf("commands = %#v", switcher.commands)
	}
}

func TestParseSummaryVariantCallbackDataAcceptsCanonicalCodesAndAliases(t *testing.T) {
	tests := []struct {
		data        string
		wantRequest int64
		wantVariant summarize.SummaryVariant
	}{
		{data: "summary_variant:42:zh-hans", wantRequest: 42, wantVariant: summarize.VariantSimplified},
		{data: "summary_variant:42:zh-hant", wantRequest: 42, wantVariant: summarize.VariantTraditional},
		{data: "summary_variant:42:js", wantRequest: 42, wantVariant: summarize.VariantSimplified},
		{data: "summary_variant:42:fs", wantRequest: 42, wantVariant: summarize.VariantTraditional},
	}
	for _, tt := range tests {
		t.Run(tt.data, func(t *testing.T) {
			requestID, variant, ok := parseSummaryVariantCallbackData(tt.data)
			if !ok || requestID != tt.wantRequest || variant != tt.wantVariant {
				t.Fatalf("parseSummaryVariantCallbackData(%q) = %d, %#v, %v", tt.data, requestID, variant, ok)
			}
		})
	}
}

func TestParseSummaryVariantCallbackDataRejectsRetiredLongCodes(t *testing.T) {
	for _, data := range []string{"summary_variant:42:jl", "summary_variant:42:fl"} {
		t.Run(data, func(t *testing.T) {
			_, _, ok := parseSummaryVariantCallbackData(data)
			if ok {
				t.Fatalf("parseSummaryVariantCallbackData(%q) accepted retired code", data)
			}
		})
	}
}

func TestHandlerPromptsForSummarizeURL(t *testing.T) {
	sender := &fakeMessageSender{}
	summary := &fakeSummaryRequester{}
	auth := &fakeAuthorizer{allowed: true}
	handler := Handler{Auth: auth, Summary: summary, Sender: sender, ReplyPrompts: NewMemoryReplyPromptStore()}

	err := handler.HandleMessage(context.Background(), testMessage("/summarize_podcast"))
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	if len(summary.commands) != 0 {
		t.Fatalf("commands = %#v", summary.commands)
	}
	if len(sender.messages) != 1 || sender.messages[0] != summarizeURLPromptText || sender.replies[0] != 30 {
		t.Fatalf("messages = %#v replies = %#v", sender.messages, sender.replies)
	}
	if len(sender.forceReplies) != 1 || sender.forceReplies[0] != 30 || sender.forcePlaceholders[0] != urlPromptPlaceholderText {
		t.Fatalf("force replies = %#v placeholders = %#v", sender.forceReplies, sender.forcePlaceholders)
	}
	if auth.canUseCalls != 1 {
		t.Fatalf("canUseCalls = %d", auth.canUseCalls)
	}
}

func TestHandlerRunsPendingSummarizeReply(t *testing.T) {
	sender := &fakeMessageSender{}
	summary := &fakeSummaryRequester{result: service.SummaryCommandResult{
		Media:                   db.MediaItem{CanonicalURL: "https://www.youtube.com/watch?v=abc12345678"},
		CreatedTranscriptionJob: true,
	}}
	progress := &fakeProgressReporter{}
	auth := &fakeAuthorizer{allowed: true}
	handler := Handler{Auth: auth, Summary: summary, Sender: sender, Progress: progress, ReplyPrompts: NewMemoryReplyPromptStore()}

	if err := handler.HandleMessage(context.Background(), testMessage("/summarize_podcast")); err != nil {
		t.Fatalf("prompt returned error: %v", err)
	}
	if err := handler.HandleMessage(context.Background(), testReplyMessage("https://youtu.be/abc12345678", 1)); err != nil {
		t.Fatalf("reply returned error: %v", err)
	}
	if len(summary.commands) != 1 {
		t.Fatalf("commands = %#v", summary.commands)
	}
	command := summary.commands[0]
	if command.RawURL != "https://youtu.be/abc12345678" || command.Prompt != "" || command.MessageID != 31 {
		t.Fatalf("command = %#v", command)
	}
	if len(progress.results) != 1 {
		t.Fatalf("progress results = %#v", progress.results)
	}
	if auth.canUseCalls != 2 {
		t.Fatalf("canUseCalls = %d", auth.canUseCalls)
	}
}

func TestHandlerKeepsPromptAfterInvalidSummarizeReply(t *testing.T) {
	sender := &fakeMessageSender{}
	summary := &fakeSummaryRequester{err: provider.ErrInvalidURL}
	handler := Handler{Auth: &fakeAuthorizer{allowed: true}, Summary: summary, Sender: sender, ReplyPrompts: NewMemoryReplyPromptStore()}

	if err := handler.HandleMessage(context.Background(), testMessage("/summarize_podcast")); err != nil {
		t.Fatalf("prompt returned error: %v", err)
	}
	if err := handler.HandleMessage(context.Background(), testReplyMessage("not a url", 1)); err != nil {
		t.Fatalf("first reply returned error: %v", err)
	}
	if err := handler.HandleMessage(context.Background(), testReplyMessage("also not a url", 1)); err != nil {
		t.Fatalf("second reply returned error: %v", err)
	}
	if len(summary.commands) != 2 {
		t.Fatalf("commands = %#v", summary.commands)
	}
}

func TestHandlerRejectsWrongUserForPromptReply(t *testing.T) {
	sender := &fakeMessageSender{}
	summary := &fakeSummaryRequester{}
	handler := Handler{Auth: &fakeAuthorizer{allowed: true}, Summary: summary, Sender: sender, ReplyPrompts: NewMemoryReplyPromptStore()}

	if err := handler.HandleMessage(context.Background(), testMessage("/summarize_podcast")); err != nil {
		t.Fatalf("prompt returned error: %v", err)
	}
	wrongUserReply := testReplyMessage("https://youtu.be/abc12345678", 1)
	wrongUserReply.UserID = 21
	if err := handler.HandleMessage(context.Background(), wrongUserReply); err != nil {
		t.Fatalf("wrong user reply returned error: %v", err)
	}
	if len(summary.commands) != 0 {
		t.Fatalf("commands = %#v", summary.commands)
	}
	if len(sender.messages) != 2 || sender.messages[1] != wrongPromptUserText {
		t.Fatalf("messages = %#v", sender.messages)
	}
}

func TestHandlerDistinguishesSummarizeErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{name: "invalid url", err: provider.ErrInvalidURL, want: service.InvalidURLNotification().Text},
		{name: "operational error", err: context.DeadlineExceeded, want: "Summarization failed. Please try again later."},
	} {
		t.Run(test.name, func(t *testing.T) {
			sender := &fakeMessageSender{}
			handler := Handler{Auth: &fakeAuthorizer{allowed: true}, Summary: &fakeSummaryRequester{err: test.err}, Sender: sender}

			err := handler.HandleMessage(context.Background(), testMessage("/summarize_podcast https://youtu.be/abc12345678"))
			if err != nil {
				t.Fatalf("HandleMessage returned error: %v", err)
			}
			if len(sender.messages) != 1 || sender.messages[0] != test.want {
				t.Fatalf("messages = %#v", sender.messages)
			}
		})
	}
}

func TestHandlerSuppressesFallbackForDeliveryClassifiedErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "ambiguous", err: deliveryClassifiedError{ambiguous: true}},
		{name: "terminal", err: deliveryClassifiedError{terminal: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			sender := &fakeMessageSender{}
			handler := Handler{Sender: sender}

			handler.handleTelegramError(context.Background(), testMessage("/summarize_podcast https://youtu.be/abc12345678"), test.err)

			if len(sender.messages) != 0 {
				t.Fatalf("messages = %#v", sender.messages)
			}
		})
	}
}

func TestHandlerReturnsUsageForInvalidCommand(t *testing.T) {
	sender := &fakeMessageSender{}
	handler := Handler{Sender: sender}

	err := handler.HandleMessage(context.Background(), testMessage("/unknown"))
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	if len(sender.messages) != 1 || !strings.Contains(sender.messages[0], "unknown command") {
		t.Fatalf("messages = %#v", sender.messages)
	}
}

func TestHandlerRoutesStatus(t *testing.T) {
	sender := &fakeMessageSender{}
	status := &fakeStatusReporter{report: service.StatusReport{Text: "status text"}}
	handler := Handler{Status: status, Sender: sender}

	err := handler.HandleMessage(context.Background(), testMessage("/summary_status https://youtu.be/abc12345678"))
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	if len(status.queries) != 1 {
		t.Fatalf("queries = %#v", status.queries)
	}
	if status.queries[0].RawURL != "https://youtu.be/abc12345678" {
		t.Fatalf("query = %#v", status.queries[0])
	}
	if len(sender.messages) != 1 || sender.messages[0] != "status text" {
		t.Fatalf("messages = %#v", sender.messages)
	}
}

func TestHandlerPromptsForStatusURL(t *testing.T) {
	sender := &fakeMessageSender{}
	status := &fakeStatusReporter{report: service.StatusReport{Text: "status text"}}
	handler := Handler{Auth: &fakeAuthorizer{allowed: true}, Status: status, Sender: sender, ReplyPrompts: NewMemoryReplyPromptStore()}

	err := handler.HandleMessage(context.Background(), testMessage("/summary_status"))
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	if len(status.queries) != 0 {
		t.Fatalf("queries = %#v", status.queries)
	}
	if len(sender.messages) != 1 || sender.messages[0] != statusURLPromptText || sender.replies[0] != 30 {
		t.Fatalf("messages = %#v replies = %#v", sender.messages, sender.replies)
	}
	if len(sender.forceReplies) != 1 || sender.forceReplies[0] != 30 || sender.forcePlaceholders[0] != urlPromptPlaceholderText {
		t.Fatalf("force replies = %#v placeholders = %#v", sender.forceReplies, sender.forcePlaceholders)
	}
}

func TestHandlerRejectsUnauthorizedStatusPrompt(t *testing.T) {
	sender := &fakeMessageSender{}
	status := &fakeStatusReporter{}
	handler := Handler{Auth: &fakeAuthorizer{allowed: false}, Status: status, Sender: sender, ReplyPrompts: NewMemoryReplyPromptStore()}

	err := handler.HandleMessage(context.Background(), testMessage("/summary_status"))
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	if len(status.queries) != 0 {
		t.Fatalf("queries = %#v", status.queries)
	}
	if len(sender.messages) != 1 || sender.messages[0] != service.UnauthorizedNotification().Text {
		t.Fatalf("messages = %#v", sender.messages)
	}
}

func TestHandlerRunsPendingStatusReply(t *testing.T) {
	sender := &fakeMessageSender{}
	status := &fakeStatusReporter{report: service.StatusReport{Text: "status text"}}
	handler := Handler{Auth: &fakeAuthorizer{allowed: true}, Status: status, Sender: sender, ReplyPrompts: NewMemoryReplyPromptStore()}

	if err := handler.HandleMessage(context.Background(), testMessage("/summary_status")); err != nil {
		t.Fatalf("prompt returned error: %v", err)
	}
	if err := handler.HandleMessage(context.Background(), testReplyMessage("https://youtu.be/abc12345678", 1)); err != nil {
		t.Fatalf("reply returned error: %v", err)
	}
	if len(status.queries) != 1 {
		t.Fatalf("queries = %#v", status.queries)
	}
	if status.queries[0].RawURL != "https://youtu.be/abc12345678" {
		t.Fatalf("query = %#v", status.queries[0])
	}
	if len(sender.messages) != 2 || sender.messages[1] != "status text" || sender.replies[1] != 31 {
		t.Fatalf("messages = %#v replies = %#v", sender.messages, sender.replies)
	}
}

func TestHandlerKeepsPromptAfterInvalidStatusReply(t *testing.T) {
	sender := &fakeMessageSender{}
	status := &fakeStatusReporter{report: service.StatusReport{Text: service.InvalidURLNotification().Text, InvalidURL: true}}
	handler := Handler{Auth: &fakeAuthorizer{allowed: true}, Status: status, Sender: sender, ReplyPrompts: NewMemoryReplyPromptStore()}

	if err := handler.HandleMessage(context.Background(), testMessage("/summary_status")); err != nil {
		t.Fatalf("prompt returned error: %v", err)
	}
	if err := handler.HandleMessage(context.Background(), testReplyMessage("not a url", 1)); err != nil {
		t.Fatalf("first reply returned error: %v", err)
	}
	if err := handler.HandleMessage(context.Background(), testReplyMessage("also not a url", 1)); err != nil {
		t.Fatalf("second reply returned error: %v", err)
	}
	if len(status.queries) != 2 {
		t.Fatalf("queries = %#v", status.queries)
	}
}

func TestHandlerRoutesCommandsWithBotSuffix(t *testing.T) {
	sender := &fakeMessageSender{}
	summary := &fakeSummaryRequester{result: service.SummaryCommandResult{Summarized: true}}
	status := &fakeStatusReporter{report: service.StatusReport{Text: "status text"}}
	handler := Handler{Auth: &fakeAuthorizer{allowed: true}, Summary: summary, Status: status, Sender: sender}

	if err := handler.HandleMessage(context.Background(), testMessage("/summarize_podcast@TestBot https://youtu.be/abc12345678")); err != nil {
		t.Fatalf("summarize returned error: %v", err)
	}
	if err := handler.HandleMessage(context.Background(), testMessage("/summary_status@TestBot https://youtu.be/abc12345678")); err != nil {
		t.Fatalf("status returned error: %v", err)
	}
	if len(summary.commands) != 1 || summary.commands[0].RawURL != "https://youtu.be/abc12345678" {
		t.Fatalf("commands = %#v", summary.commands)
	}
	if len(status.queries) != 1 || status.queries[0].RawURL != "https://youtu.be/abc12345678" {
		t.Fatalf("queries = %#v", status.queries)
	}
}

func TestHandlerDefaultMessageIgnoresUnknownSlashCommand(t *testing.T) {
	sender := &fakeMessageSender{}
	handler := Handler{Sender: sender, ReplyPrompts: NewMemoryReplyPromptStore()}

	if err := handler.HandleDefaultMessage(context.Background(), testMessage("/unknown@OtherBot")); err != nil {
		t.Fatalf("HandleDefaultMessage returned error: %v", err)
	}
	if len(sender.messages) != 0 {
		t.Fatalf("messages = %#v", sender.messages)
	}
}

func TestHandlerDefaultMessageRoutesKnownBotSuffixCommand(t *testing.T) {
	sender := &fakeMessageSender{}
	summary := &fakeSummaryRequester{result: service.SummaryCommandResult{Summarized: true}}
	handler := Handler{Auth: &fakeAuthorizer{allowed: true}, Summary: summary, Sender: sender}

	if err := handler.HandleDefaultMessage(context.Background(), testMessage("/summarize_podcast@TestBot https://youtu.be/abc12345678")); err != nil {
		t.Fatalf("HandleDefaultMessage returned error: %v", err)
	}
	if len(summary.commands) != 1 || summary.commands[0].RawURL != "https://youtu.be/abc12345678" {
		t.Fatalf("commands = %#v", summary.commands)
	}
}

func TestHandlerIgnoresUnmatchedNonCommandText(t *testing.T) {
	sender := &fakeMessageSender{}
	handler := Handler{Sender: sender, ReplyPrompts: NewMemoryReplyPromptStore()}

	if err := handler.HandleMessage(context.Background(), testMessage("https://youtu.be/abc12345678")); err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	if len(sender.messages) != 0 {
		t.Fatalf("messages = %#v", sender.messages)
	}
}

func TestHandlerRoutesWatchCommands(t *testing.T) {
	sender := &fakeMessageSender{}
	watch := &fakeWatchManager{response: service.WatchResponse{Text: "watch ok"}}
	handler := Handler{Auth: &fakeAuthorizer{owner: true}, Watch: watch, Sender: sender}
	message := testMessage("/subscribe_podcast https://player.soundon.fm/p/00000000-0000-0000-0000-000000000000")
	message.ChatID = -100
	message.ChatType = auth.ChatTypeSupergroup
	message.ChatTitle = "Podcast Group"

	if err := handler.HandleMessage(context.Background(), message); err != nil {
		t.Fatalf("subscribe returned error: %v", err)
	}
	if err := handler.HandleMessage(context.Background(), testMessage("/unsubscribe_podcast https://player.soundon.fm/p/00000000-0000-0000-0000-000000000000")); err != nil {
		t.Fatalf("unsubscribe returned error: %v", err)
	}
	if err := handler.HandleMessage(context.Background(), testMessage("/subscriptions")); err != nil {
		t.Fatalf("subscriptions returned error: %v", err)
	}

	if len(watch.subscribeCommands) != 1 {
		t.Fatalf("subscribe commands = %#v", watch.subscribeCommands)
	}
	subscribe := watch.subscribeCommands[0]
	if subscribe.ChatID != -100 || subscribe.ChatType != string(auth.ChatTypeSupergroup) || subscribe.ChatTitle != "Podcast Group" || subscribe.CreatedByUserID != message.UserID {
		t.Fatalf("subscribe command = %#v", subscribe)
	}
	if len(watch.unsubscribeCommands) != 1 || watch.unsubscribeCommands[0].ChatID != 10 {
		t.Fatalf("unsubscribe commands = %#v", watch.unsubscribeCommands)
	}
	if len(watch.listChatIDs) != 1 || watch.listChatIDs[0] != 10 {
		t.Fatalf("list chat IDs = %#v", watch.listChatIDs)
	}
	if !slices.Equal(sender.messages, []string{"watch ok", "watch ok", "watch ok"}) {
		t.Fatalf("messages = %#v", sender.messages)
	}
}

func TestHandlerRejectsUnauthorizedWatchCommand(t *testing.T) {
	sender := &fakeMessageSender{}
	watch := &fakeWatchManager{}
	handler := Handler{Auth: &fakeAuthorizer{owner: false, allowed: true}, Watch: watch, Sender: sender}

	err := handler.HandleMessage(context.Background(), testMessage("/subscribe_podcast https://player.soundon.fm/p/00000000-0000-0000-0000-000000000000"))
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	if len(watch.subscribeCommands) != 0 {
		t.Fatalf("subscribe commands = %#v", watch.subscribeCommands)
	}
	if len(sender.messages) != 1 || sender.messages[0] != service.UnauthorizedNotification().Text {
		t.Fatalf("messages = %#v", sender.messages)
	}
}

func TestHandlerReturnsWatchValidationResponse(t *testing.T) {
	sender := &fakeMessageSender{}
	watch := &fakeWatchManager{response: service.WatchResponse{Text: "Please send a SoundOn or xiaoyuzhou podcast URL."}}
	handler := Handler{Auth: &fakeAuthorizer{owner: true}, Watch: watch, Sender: sender}

	err := handler.HandleMessage(context.Background(), testMessage("/subscribe_podcast https://youtu.be/abc12345678"))
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	if len(sender.messages) != 1 || !strings.Contains(sender.messages[0], "SoundOn") || !strings.Contains(sender.messages[0], "xiaoyuzhou") {
		t.Fatalf("messages = %#v", sender.messages)
	}
}

func TestHandlerRejectsUnauthorizedWhitelistCommand(t *testing.T) {
	sender := &fakeMessageSender{}
	handler := Handler{Auth: &fakeAuthorizer{owner: false}, Sender: sender}

	err := handler.HandleMessage(context.Background(), testMessage("/whitelist"))
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	if len(sender.messages) != 1 || sender.messages[0] != service.UnauthorizedNotification().Text {
		t.Fatalf("messages = %#v", sender.messages)
	}
}

func TestHandlerAllowsCurrentGroup(t *testing.T) {
	sender := &fakeMessageSender{}
	whitelist := newFakeWhitelist()
	handler := Handler{Auth: &fakeAuthorizer{owner: true}, Whitelist: whitelist, Sender: sender}
	message := testMessage("/allow_group")
	message.ChatID = -100
	message.ChatType = auth.ChatTypeSupergroup
	message.ChatTitle = "Podcast Group"

	err := handler.HandleMessage(context.Background(), message)
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	if whitelist.groups[-100].Title != "Podcast Group" || whitelist.groups[-100].CreatedByUserID != message.UserID {
		t.Fatalf("groups = %#v", whitelist.groups)
	}
	if len(sender.messages) != 1 || !strings.Contains(sender.messages[0], "Allowed group -100") {
		t.Fatalf("messages = %#v", sender.messages)
	}
}

func TestHandlerAllowsExplicitGroupAndUser(t *testing.T) {
	sender := &fakeMessageSender{}
	whitelist := newFakeWhitelist()
	handler := Handler{Auth: &fakeAuthorizer{owner: true}, Whitelist: whitelist, Sender: sender}

	if err := handler.HandleMessage(context.Background(), testMessage("/allow_group -200")); err != nil {
		t.Fatalf("allow group returned error: %v", err)
	}
	if err := handler.HandleMessage(context.Background(), testMessage("/allow_user 123")); err != nil {
		t.Fatalf("allow user returned error: %v", err)
	}
	if _, ok := whitelist.groups[-200]; !ok {
		t.Fatalf("groups = %#v", whitelist.groups)
	}
	if _, ok := whitelist.users[123]; !ok {
		t.Fatalf("users = %#v", whitelist.users)
	}
}

func TestHandlerAllowsMultipleGroups(t *testing.T) {
	sender := &fakeMessageSender{}
	whitelist := newFakeWhitelist()
	handler := Handler{Auth: &fakeAuthorizer{owner: true}, Whitelist: whitelist, Sender: sender}

	if err := handler.HandleMessage(context.Background(), testMessage("/allow_group -200,-300")); err != nil {
		t.Fatalf("allow groups returned error: %v", err)
	}
	if _, ok := whitelist.groups[-200]; !ok {
		t.Fatalf("groups = %#v", whitelist.groups)
	}
	if _, ok := whitelist.groups[-300]; !ok {
		t.Fatalf("groups = %#v", whitelist.groups)
	}
	if len(sender.messages) != 1 || !strings.Contains(sender.messages[0], "Allowed groups: -200, -300") {
		t.Fatalf("messages = %#v", sender.messages)
	}
}

func TestHandlerAllowsMultipleGroupsSkipsInvalid(t *testing.T) {
	sender := &fakeMessageSender{}
	whitelist := newFakeWhitelist()
	handler := Handler{Auth: &fakeAuthorizer{owner: true}, Whitelist: whitelist, Sender: sender}

	if err := handler.HandleMessage(context.Background(), testMessage("/allow_group -200,bad,-300")); err != nil {
		t.Fatalf("allow groups returned error: %v", err)
	}
	if _, ok := whitelist.groups[-200]; !ok {
		t.Fatalf("groups = %#v", whitelist.groups)
	}
	if _, ok := whitelist.groups[-300]; !ok {
		t.Fatalf("groups = %#v", whitelist.groups)
	}
	if len(sender.messages) != 1 || !strings.Contains(sender.messages[0], "Allowed groups: -200, -300") || !strings.Contains(sender.messages[0], "Skipped: bad") {
		t.Fatalf("messages = %#v", sender.messages)
	}
}

func TestHandlerAllowsDuplicateGroupsDeduped(t *testing.T) {
	sender := &fakeMessageSender{}
	whitelist := newFakeWhitelist()
	handler := Handler{Auth: &fakeAuthorizer{owner: true}, Whitelist: whitelist, Sender: sender}

	if err := handler.HandleMessage(context.Background(), testMessage("/allow_group -200,-200")); err != nil {
		t.Fatalf("allow groups returned error: %v", err)
	}
	if len(whitelist.groups) != 1 {
		t.Fatalf("groups = %#v", whitelist.groups)
	}
	if len(sender.messages) != 1 || !strings.Contains(sender.messages[0], "Allowed group -200") {
		t.Fatalf("messages = %#v", sender.messages)
	}
}

func TestHandlerRemovesMultipleGroups(t *testing.T) {
	sender := &fakeMessageSender{}
	whitelist := newFakeWhitelist()
	whitelist.groups[-200] = db.WhitelistedGroup{ChatID: -200}
	whitelist.groups[-300] = db.WhitelistedGroup{ChatID: -300}
	handler := Handler{Auth: &fakeAuthorizer{owner: true}, Whitelist: whitelist, Sender: sender}

	if err := handler.HandleMessage(context.Background(), testMessage("/remove_group -200,-300")); err != nil {
		t.Fatalf("remove groups returned error: %v", err)
	}
	if len(whitelist.groups) != 0 {
		t.Fatalf("groups = %#v", whitelist.groups)
	}
	if len(sender.messages) != 1 || !strings.Contains(sender.messages[0], "Removed groups: -200, -300") {
		t.Fatalf("messages = %#v", sender.messages)
	}
}

func TestHandlerRemovesGroupAndUser(t *testing.T) {
	sender := &fakeMessageSender{}
	whitelist := newFakeWhitelist()
	whitelist.groups[-200] = db.WhitelistedGroup{ChatID: -200}
	whitelist.users[123] = db.WhitelistedDMUser{UserID: 123}
	handler := Handler{Auth: &fakeAuthorizer{owner: true}, Whitelist: whitelist, Sender: sender}

	if err := handler.HandleMessage(context.Background(), testMessage("/remove_group -200")); err != nil {
		t.Fatalf("remove group returned error: %v", err)
	}
	if err := handler.HandleMessage(context.Background(), testMessage("/remove_user 123")); err != nil {
		t.Fatalf("remove user returned error: %v", err)
	}
	if len(whitelist.groups) != 0 || len(whitelist.users) != 0 {
		t.Fatalf("groups = %#v users = %#v", whitelist.groups, whitelist.users)
	}
}

func TestHandlerListsWhitelist(t *testing.T) {
	sender := &fakeMessageSender{}
	whitelist := newFakeWhitelist()
	whitelist.groups[-200] = db.WhitelistedGroup{ChatID: -200, Title: "Group"}
	whitelist.users[123] = db.WhitelistedDMUser{UserID: 123, Username: "alice"}
	handler := Handler{Auth: &fakeAuthorizer{owner: true}, Whitelist: whitelist, Sender: sender}

	err := handler.HandleMessage(context.Background(), testMessage("/whitelist"))
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	if len(sender.messages) != 1 || !strings.Contains(sender.messages[0], "- -200 Group") || !strings.Contains(sender.messages[0], "- 123 @alice") {
		t.Fatalf("messages = %#v", sender.messages)
	}
}

func TestHandlerRequiresGroupIDOutsideGroupChat(t *testing.T) {
	sender := &fakeMessageSender{}
	handler := Handler{Auth: &fakeAuthorizer{owner: true}, Whitelist: newFakeWhitelist(), Sender: sender}

	err := handler.HandleMessage(context.Background(), testMessage("/allow_group"))
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	if len(sender.messages) != 1 || !strings.Contains(sender.messages[0], "usage: /allow_group") {
		t.Fatalf("messages = %#v", sender.messages)
	}
}

func TestHandlerSendsGenericReplyForTelegramHandlerError(t *testing.T) {
	sender := &fakeMessageSender{}
	handler := Handler{Sender: sender}

	handler.handleTelegramError(context.Background(), testMessage("/summary_status https://youtu.be/abc12345678"), context.DeadlineExceeded)

	if len(sender.messages) != 1 || !strings.Contains(sender.messages[0], "something went wrong") {
		t.Fatalf("messages = %#v", sender.messages)
	}
}

func TestHandlerStartAndHelp(t *testing.T) {
	sender := &fakeMessageSender{}
	handler := Handler{Sender: sender}

	if err := handler.HandleStart(context.Background(), testMessage("/start")); err != nil {
		t.Fatalf("HandleStart returned error: %v", err)
	}
	if err := handler.HandleHelp(context.Background(), testMessage("/help")); err != nil {
		t.Fatalf("HandleHelp returned error: %v", err)
	}
	if len(sender.messages) != 2 || !strings.Contains(sender.messages[1], "/summarize_podcast") {
		t.Fatalf("messages = %#v", sender.messages)
	}
}

func TestRegisterHandlersRegistersCommands(t *testing.T) {
	registrar := &fakeRegistrar{}
	RegisterHandlers(registrar, Handler{})

	want := []string{
		"start",
		"help",
		"summarize_podcast",
		"subscribe_podcast",
		"unsubscribe_podcast",
		"subscriptions",
		"allow_group",
		"remove_group",
		"allow_user",
		"remove_user",
		"whitelist",
		"summary_status",
		summaryVariantCallbackPrefix,
	}
	if !slices.Equal(registrar.patterns, want) {
		t.Fatalf("patterns = %#v", registrar.patterns)
	}
}

func testMessage(text string) Message {
	return Message{Text: text, ChatID: 10, ChatType: auth.ChatTypePrivate, UserID: 20, MessageID: 30}
}

func testReplyMessage(text string, replyToMessageID int64) Message {
	message := testMessage(text)
	message.MessageID = 31
	message.ReplyToMessageID = replyToMessageID
	return message
}

type deliveryClassifiedError struct {
	ambiguous bool
	terminal  bool
}

func (e deliveryClassifiedError) Error() string {
	return "delivery classified"
}

func (e deliveryClassifiedError) AmbiguousDelivery() bool {
	return e.ambiguous
}

func (e deliveryClassifiedError) TerminalDeliveryFailure() bool {
	return e.terminal
}

type fakeMessageSender struct {
	messages          []string
	replies           []int64
	forceReplies      []int64
	forcePlaceholders []string
}

func (f *fakeMessageSender) SendText(_ context.Context, _ int64, text string) error {
	f.messages = append(f.messages, text)
	return nil
}

func (f *fakeMessageSender) SendReplyText(_ context.Context, _ int64, replyToMessageID int64, text string) (int64, error) {
	f.messages = append(f.messages, text)
	f.replies = append(f.replies, replyToMessageID)
	return int64(len(f.messages)), nil
}

func (f *fakeMessageSender) SendForceReplyText(_ context.Context, _ int64, replyToMessageID int64, text, placeholder string) (int64, error) {
	f.messages = append(f.messages, text)
	f.replies = append(f.replies, replyToMessageID)
	f.forceReplies = append(f.forceReplies, replyToMessageID)
	f.forcePlaceholders = append(f.forcePlaceholders, placeholder)
	return int64(len(f.messages)), nil
}

type fakeProgressReporter struct {
	results []service.SummaryCommandResult
	err     error
}

func (f *fakeProgressReporter) InitialRequestStatus(_ context.Context, result service.SummaryCommandResult) error {
	f.results = append(f.results, result)
	return f.err
}

type fakeAuthorizer struct {
	allowed     bool
	owner       bool
	err         error
	canUseCalls int
}

func (f *fakeAuthorizer) CanUse(_ context.Context, _, _ int64, _ auth.ChatType) (bool, error) {
	f.canUseCalls++
	if f.err != nil {
		return false, f.err
	}
	return f.allowed, nil
}

func (f *fakeAuthorizer) CanManageWhitelist(_ int64) bool {
	return f.owner
}

func (f *fakeAuthorizer) CanManageSubscriptions(_ int64) bool {
	return f.owner
}

type fakeSummaryRequester struct {
	result   service.SummaryCommandResult
	err      error
	commands []service.SummaryCommand
}

func (f *fakeSummaryRequester) RequestSummary(_ context.Context, command service.SummaryCommand) (service.SummaryCommandResult, error) {
	f.commands = append(f.commands, command)
	if f.err != nil {
		return service.SummaryCommandResult{}, f.err
	}
	return f.result, nil
}

type fakeSummaryVariantSwitcher struct {
	commands []service.SwitchSummaryVariantCommand
	err      error
}

func (f *fakeSummaryVariantSwitcher) SwitchSummaryVariant(_ context.Context, command service.SwitchSummaryVariantCommand) error {
	f.commands = append(f.commands, command)
	return f.err
}

type fakeStatusReporter struct {
	report  service.StatusReport
	err     error
	queries []service.StatusQuery
}

func (f *fakeStatusReporter) Status(_ context.Context, query service.StatusQuery) (service.StatusReport, error) {
	f.queries = append(f.queries, query)
	if f.err != nil {
		return service.StatusReport{}, f.err
	}
	return f.report, nil
}

type fakeWatchManager struct {
	response            service.WatchResponse
	err                 error
	subscribeCommands   []service.WatchSubscribeCommand
	unsubscribeCommands []service.WatchUnsubscribeCommand
	listChatIDs         []int64
}

func (f *fakeWatchManager) Subscribe(_ context.Context, command service.WatchSubscribeCommand) (service.WatchResponse, error) {
	f.subscribeCommands = append(f.subscribeCommands, command)
	if f.err != nil {
		return service.WatchResponse{}, f.err
	}
	return f.response, nil
}

func (f *fakeWatchManager) Unsubscribe(_ context.Context, command service.WatchUnsubscribeCommand) (service.WatchResponse, error) {
	f.unsubscribeCommands = append(f.unsubscribeCommands, command)
	if f.err != nil {
		return service.WatchResponse{}, f.err
	}
	return f.response, nil
}

func (f *fakeWatchManager) ListSubscriptions(_ context.Context, chatID int64) (service.WatchResponse, error) {
	f.listChatIDs = append(f.listChatIDs, chatID)
	if f.err != nil {
		return service.WatchResponse{}, f.err
	}
	return f.response, nil
}

type fakeWhitelist struct {
	groups map[int64]db.WhitelistedGroup
	users  map[int64]db.WhitelistedDMUser
}

func newFakeWhitelist() *fakeWhitelist {
	return &fakeWhitelist{
		groups: map[int64]db.WhitelistedGroup{},
		users:  map[int64]db.WhitelistedDMUser{},
	}
}

func (f *fakeWhitelist) UpsertWhitelistedGroup(_ context.Context, group db.WhitelistedGroup) error {
	f.groups[group.ChatID] = group
	return nil
}

func (f *fakeWhitelist) RemoveWhitelistedGroup(_ context.Context, chatID int64) error {
	delete(f.groups, chatID)
	return nil
}

func (f *fakeWhitelist) UpsertWhitelistedDMUser(_ context.Context, user db.WhitelistedDMUser) error {
	f.users[user.UserID] = user
	return nil
}

func (f *fakeWhitelist) RemoveWhitelistedDMUser(_ context.Context, userID int64) error {
	delete(f.users, userID)
	return nil
}

func (f *fakeWhitelist) ListWhitelistedGroups(_ context.Context) ([]db.WhitelistedGroup, error) {
	groups := make([]db.WhitelistedGroup, 0, len(f.groups))
	for _, group := range f.groups {
		groups = append(groups, group)
	}
	return groups, nil
}

func (f *fakeWhitelist) ListWhitelistedDMUsers(_ context.Context) ([]db.WhitelistedDMUser, error) {
	users := make([]db.WhitelistedDMUser, 0, len(f.users))
	for _, user := range f.users {
		users = append(users, user)
	}
	return users, nil
}

type fakeRegistrar struct {
	patterns []string
}

func (f *fakeRegistrar) RegisterHandler(_ telegram.HandlerType, pattern string, _ telegram.MatchType, _ telegram.HandlerFunc, _ ...telegram.Middleware) string {
	f.patterns = append(f.patterns, pattern)
	return pattern
}
