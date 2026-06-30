package bot

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/summarize"
)

type CommandName string

const (
	CommandSummarize        CommandName = "summarize_podcast"
	CommandStatus           CommandName = "summary_status"
	CommandSubscribePodcast CommandName = "subscribe_podcast"
	CommandUnsubscribePodcast      CommandName = "unsubscribe_podcast"
	CommandSubscriptions    CommandName = "subscriptions"
	CommandAllowGroup       CommandName = "allow_group"
	CommandRemoveGroup      CommandName = "remove_group"
	CommandAllowUser        CommandName = "allow_user"
	CommandRemoveUser       CommandName = "remove_user"
	CommandWhitelist        CommandName = "whitelist"
)

type Command struct {
	Name      CommandName
	URL       string
	HasURL    bool
	Prompt    string
	HasPrompt bool
	ChatID    int64
	UserID    int64
	HasChatID bool
	HasUserID bool
}

func ParseCommand(text string) (Command, error) {
	text = strings.TrimSpace(text)
	if text == "" || !strings.HasPrefix(text, "/") {
		return Command{}, errors.New("command is required")
	}

	nameToken, rest := splitCommandToken(text)
	name := strings.TrimPrefix(nameToken, "/")
	if at := strings.IndexByte(name, '@'); at >= 0 {
		name = name[:at]
	}

	switch CommandName(name) {
	case CommandSummarize:
		return parseSummarize(rest)
	case CommandStatus:
		return parseStatus(rest)
	case CommandSubscribePodcast:
		return parseRequiredURL(CommandSubscribePodcast, rest)
	case CommandUnsubscribePodcast:
		return parseRequiredURL(CommandUnsubscribePodcast, rest)
	case CommandSubscriptions:
		if strings.TrimSpace(rest) != "" {
			return Command{}, errors.New("usage: /subscriptions")
		}
		return Command{Name: CommandSubscriptions}, nil
	case CommandAllowGroup:
		return parseOptionalChatID(CommandAllowGroup, rest)
	case CommandRemoveGroup:
		return parseOptionalChatID(CommandRemoveGroup, rest)
	case CommandAllowUser:
		return parseRequiredUserID(CommandAllowUser, rest)
	case CommandRemoveUser:
		return parseRequiredUserID(CommandRemoveUser, rest)
	case CommandWhitelist:
		if strings.TrimSpace(rest) != "" {
			return Command{}, errors.New("usage: /whitelist")
		}
		return Command{Name: CommandWhitelist}, nil
	default:
		return Command{}, fmt.Errorf("unknown command: %s", name)
	}
}

func parseSummarize(rest string) (Command, error) {
	url, prompt, ok := splitFirstArg(rest)
	if !ok {
		return Command{Name: CommandSummarize, Prompt: summarize.DefaultPrompt}, nil
	}
	command := Command{Name: CommandSummarize, URL: url, HasURL: true, Prompt: summarize.DefaultPrompt}
	if strings.TrimSpace(prompt) != "" {
		command.Prompt = strings.TrimSpace(prompt)
		command.HasPrompt = true
	}
	return command, nil
}

func parseStatus(rest string) (Command, error) {
	url, extra, ok := splitFirstArg(rest)
	if !ok {
		return Command{Name: CommandStatus}, nil
	}
	if strings.TrimSpace(extra) != "" {
		return Command{}, errors.New("usage: /summary_status <MEDIA_URL>")
	}
	return Command{Name: CommandStatus, URL: url, HasURL: true}, nil
}

func parseRequiredURL(name CommandName, rest string) (Command, error) {
	url, extra, ok := splitFirstArg(rest)
	if !ok || strings.TrimSpace(extra) != "" {
		return Command{}, fmt.Errorf("usage: /%s <url>", name)
	}
	return Command{Name: name, URL: url, HasURL: true}, nil
}

func parseOptionalChatID(name CommandName, rest string) (Command, error) {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return Command{Name: name}, nil
	}
	if strings.ContainsAny(rest, " \t\n") {
		return Command{}, fmt.Errorf("usage: /%s [chat_id]", name)
	}
	chatID, err := strconv.ParseInt(rest, 10, 64)
	if err != nil {
		return Command{}, fmt.Errorf("chat_id must be numeric: %w", err)
	}
	return Command{Name: name, ChatID: chatID, HasChatID: true}, nil
}

func parseRequiredUserID(name CommandName, rest string) (Command, error) {
	rest = strings.TrimSpace(rest)
	if rest == "" || strings.ContainsAny(rest, " \t\n") {
		return Command{}, fmt.Errorf("usage: /%s <user_id>", name)
	}
	userID, err := strconv.ParseInt(rest, 10, 64)
	if err != nil {
		return Command{}, fmt.Errorf("user_id must be numeric: %w", err)
	}
	return Command{Name: name, UserID: userID, HasUserID: true}, nil
}

func splitCommandToken(text string) (string, string) {
	for i, r := range text {
		if unicode.IsSpace(r) {
			return text[:i], strings.TrimLeftFunc(text[i:], unicode.IsSpace)
		}
	}
	return text, ""
}

func splitFirstArg(text string) (string, string, bool) {
	text = strings.TrimLeft(text, " \t\n")
	if text == "" {
		return "", "", false
	}
	for i, r := range text {
		if r == ' ' || r == '\t' || r == '\n' {
			return text[:i], strings.TrimLeft(text[i:], " \t\n"), true
		}
	}
	return text, "", true
}
