package bot

import (
	"strings"
	"testing"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/summarize"
)

func TestParseSummarizeDefaultPrompt(t *testing.T) {
	command, err := ParseCommand("/summarize_podcast https://youtu.be/abcdefghijk")
	if err != nil {
		t.Fatalf("ParseCommand returned error: %v", err)
	}
	if command.Name != CommandSummarize || command.URL != "https://youtu.be/abcdefghijk" || !command.HasURL {
		t.Fatalf("command = %#v", command)
	}
	if command.Prompt != summarize.DefaultPrompt || command.HasPrompt {
		t.Fatalf("prompt = %q hasPrompt=%v", command.Prompt, command.HasPrompt)
	}
}

func TestParseCommandSplitsCommandOnAnyWhitespace(t *testing.T) {
	for _, input := range []string{"/summarize_podcast\nhttps://youtu.be/abcdefghijk", "/summary_status\thttps://youtu.be/abcdefghijk"} {
		command, err := ParseCommand(input)
		if err != nil {
			t.Fatalf("ParseCommand(%q) returned error: %v", input, err)
		}
		if command.URL != "https://youtu.be/abcdefghijk" || !command.HasURL {
			t.Fatalf("command = %#v", command)
		}
	}
}

func TestParseSummarizeMultilinePrompt(t *testing.T) {
	text := "/summarize_podcast https://youtu.be/abcdefghijk first line\nsecond line\nthird line"
	command, err := ParseCommand(text)
	if err != nil {
		t.Fatalf("ParseCommand returned error: %v", err)
	}
	if command.Prompt != "first line\nsecond line\nthird line" {
		t.Fatalf("prompt = %q", command.Prompt)
	}
	if !command.HasPrompt {
		t.Fatal("expected HasPrompt")
	}
}

func TestParseSummarizeWithBotUsername(t *testing.T) {
	command, err := ParseCommand("/summarize_podcast@TestBot https://youtu.be/abcdefghijk")
	if err != nil {
		t.Fatalf("ParseCommand returned error: %v", err)
	}
	if command.Name != CommandSummarize || command.URL != "https://youtu.be/abcdefghijk" || !command.HasURL {
		t.Fatalf("command = %#v", command)
	}
}

func TestParseSummarizeWithoutURL(t *testing.T) {
	command, err := ParseCommand("/summarize_podcast")
	if err != nil {
		t.Fatalf("ParseCommand returned error: %v", err)
	}
	if command.Name != CommandSummarize || command.URL != "" || command.HasURL || command.Prompt != summarize.DefaultPrompt || command.HasPrompt {
		t.Fatalf("command = %#v", command)
	}
}

func TestParseSummarizeWithoutURLWithBotUsername(t *testing.T) {
	command, err := ParseCommand("/summarize_podcast@TestBot")
	if err != nil {
		t.Fatalf("ParseCommand returned error: %v", err)
	}
	if command.Name != CommandSummarize || command.URL != "" || command.HasURL || command.Prompt != summarize.DefaultPrompt || command.HasPrompt {
		t.Fatalf("command = %#v", command)
	}
}

func TestParseStatus(t *testing.T) {
	command, err := ParseCommand("/summary_status https://youtu.be/abcdefghijk")
	if err != nil {
		t.Fatalf("ParseCommand returned error: %v", err)
	}
	if command.Name != CommandStatus || command.URL != "https://youtu.be/abcdefghijk" || !command.HasURL {
		t.Fatalf("command = %#v", command)
	}
}

func TestParseStatusWithoutURL(t *testing.T) {
	command, err := ParseCommand("/summary_status")
	if err != nil {
		t.Fatalf("ParseCommand returned error: %v", err)
	}
	if command.Name != CommandStatus || command.URL != "" || command.HasURL {
		t.Fatalf("command = %#v", command)
	}
}

func TestParseStatusWithBotUsername(t *testing.T) {
	command, err := ParseCommand("/summary_status@TestBot https://youtu.be/abcdefghijk")
	if err != nil {
		t.Fatalf("ParseCommand returned error: %v", err)
	}
	if command.Name != CommandStatus || command.URL != "https://youtu.be/abcdefghijk" || !command.HasURL {
		t.Fatalf("command = %#v", command)
	}
}

func TestParseStatusWithoutURLWithBotUsername(t *testing.T) {
	command, err := ParseCommand("/summary_status@TestBot")
	if err != nil {
		t.Fatalf("ParseCommand returned error: %v", err)
	}
	if command.Name != CommandStatus || command.URL != "" || command.HasURL {
		t.Fatalf("command = %#v", command)
	}
}

func TestParseStatusRejectsExtraArgument(t *testing.T) {
	_, err := ParseCommand("/summary_status https://youtu.be/abcdefghijk extra")
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("expected usage error, got %v", err)
	}
}

func TestParseWatchCommands(t *testing.T) {
	for _, test := range []struct {
		input string
		name  CommandName
		url   string
	}{
		{input: "/subscribe_podcast https://player.soundon.fm/p/123", name: CommandSubscribePodcast, url: "https://player.soundon.fm/p/123"},
		{input: "/unsubscribe_podcast https://www.xiaoyuzhoufm.com/podcast/abc", name: CommandUnsubscribePodcast, url: "https://www.xiaoyuzhoufm.com/podcast/abc"},
	} {
		command, err := ParseCommand(test.input)
		if err != nil {
			t.Fatalf("ParseCommand(%q) returned error: %v", test.input, err)
		}
		if command.Name != test.name || command.URL != test.url || !command.HasURL {
			t.Fatalf("command = %#v", command)
		}
	}

	command, err := ParseCommand("/subscriptions")
	if err != nil {
		t.Fatalf("ParseCommand returned error: %v", err)
	}
	if command.Name != CommandSubscriptions || command.HasURL {
		t.Fatalf("command = %#v", command)
	}
}

func TestParseWatchCommandsRejectMalformedSyntax(t *testing.T) {
	for _, input := range []string{
		"/subscribe_podcast",
		"/subscribe_podcast https://player.soundon.fm/p/123 extra",
		"/unsubscribe_podcast",
		"/unsubscribe_podcast https://www.xiaoyuzhoufm.com/podcast/abc extra",
		"/subscriptions extra",
	} {
		_, err := ParseCommand(input)
		if err == nil || !strings.Contains(err.Error(), "usage") {
			t.Fatalf("ParseCommand(%q) expected usage error, got %v", input, err)
		}
	}
}

func TestParseOptionalGroupCommands(t *testing.T) {
	for _, input := range []string{"/allow_group", "/remove_group"} {
		command, err := ParseCommand(input)
		if err != nil {
			t.Fatalf("ParseCommand(%q) returned error: %v", input, err)
		}
		if len(command.ChatIDs) != 0 {
			t.Fatalf("command = %#v", command)
		}
	}

	command, err := ParseCommand("/allow_group -100")
	if err != nil {
		t.Fatalf("ParseCommand returned error: %v", err)
	}
	if command.Name != CommandAllowGroup || len(command.ChatIDs) != 1 || command.ChatIDs[0] != -100 {
		t.Fatalf("command = %#v", command)
	}
}

func TestParseOptionalGroupCommandsMultipleIDs(t *testing.T) {
	command, err := ParseCommand("/allow_group -100,-200,-300")
	if err != nil {
		t.Fatalf("ParseCommand returned error: %v", err)
	}
	if command.Name != CommandAllowGroup || len(command.ChatIDs) != 3 || command.ChatIDs[0] != -100 || command.ChatIDs[1] != -200 || command.ChatIDs[2] != -300 {
		t.Fatalf("command = %#v", command)
	}
	if len(command.SkippedChatIDs) != 0 {
		t.Fatalf("skipped = %#v", command.SkippedChatIDs)
	}
}

func TestParseOptionalGroupCommandsTrimsSpaces(t *testing.T) {
	command, err := ParseCommand("/remove_group -100, -200 , -300")
	if err != nil {
		t.Fatalf("ParseCommand returned error: %v", err)
	}
	if len(command.ChatIDs) != 3 || command.ChatIDs[0] != -100 || command.ChatIDs[1] != -200 || command.ChatIDs[2] != -300 {
		t.Fatalf("command = %#v", command)
	}
}

func TestParseOptionalGroupCommandsSkipsInvalid(t *testing.T) {
	command, err := ParseCommand("/allow_group -100,bad,-200")
	if err != nil {
		t.Fatalf("ParseCommand returned error: %v", err)
	}
	if len(command.ChatIDs) != 2 || command.ChatIDs[0] != -100 || command.ChatIDs[1] != -200 {
		t.Fatalf("chatIDs = %#v", command.ChatIDs)
	}
	if len(command.SkippedChatIDs) != 1 || command.SkippedChatIDs[0] != "bad" {
		t.Fatalf("skipped = %#v", command.SkippedChatIDs)
	}
}

func TestParseOptionalGroupCommandsAllInvalidErrors(t *testing.T) {
	for _, input := range []string{"/allow_group bad", "/allow_group bad,worse"} {
		_, err := ParseCommand(input)
		if err == nil {
			t.Fatalf("ParseCommand(%q) expected error", input)
		}
	}
}

func TestParseRequiredUserCommands(t *testing.T) {
	command, err := ParseCommand("/allow_user 123")
	if err != nil {
		t.Fatalf("ParseCommand returned error: %v", err)
	}
	if command.Name != CommandAllowUser || !command.HasUserID || command.UserID != 123 {
		t.Fatalf("command = %#v", command)
	}

	command, err = ParseCommand("/remove_user 456")
	if err != nil {
		t.Fatalf("ParseCommand returned error: %v", err)
	}
	if command.Name != CommandRemoveUser || !command.HasUserID || command.UserID != 456 {
		t.Fatalf("command = %#v", command)
	}
}

func TestParseRequiredUserCommandsMissingArgument(t *testing.T) {
	_, err := ParseCommand("/allow_user")
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("expected usage error, got %v", err)
	}
}

func TestParseInvalidNumericIDs(t *testing.T) {
	for _, input := range []string{"/allow_group nope", "/remove_user nope"} {
		_, err := ParseCommand(input)
		if err == nil || !strings.Contains(err.Error(), "numeric") {
			t.Fatalf("ParseCommand(%q) expected numeric error, got %v", input, err)
		}
	}
}

func TestParseWhitelist(t *testing.T) {
	command, err := ParseCommand("/whitelist")
	if err != nil {
		t.Fatalf("ParseCommand returned error: %v", err)
	}
	if command.Name != CommandWhitelist {
		t.Fatalf("command = %#v", command)
	}
}
