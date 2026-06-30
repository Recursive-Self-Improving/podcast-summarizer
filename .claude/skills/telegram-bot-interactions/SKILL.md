---
name: telegram-bot-interactions
description: >-
  This skill should be used when adding, changing, or reviewing Telegram bot flows in projects: commands with optional URL args, ForceReply prompts for missing args, reply/default handler routing that ignores unrelated text, progress messages deleted after final success/error, or ParseModeHTML expandable blockquote outputs converted from Markdown to Telegram-safe HTML.
---

# Telegram bot interaction patterns

Use this skill when implementing or reviewing Telegram bot flows in projects. The goal is a bot that feels responsive, accepts both direct command arguments and guided replies, and renders final results in Telegram-native HTML without exposing raw Markdown artifacts.

## Core workflow

1. Inspect the existing command and message abstractions before editing.
   - Check how the code carries chat/user/message IDs and reply metadata.
   - Check how handlers are registered and how the default handler routes unmatched text.
2. Preserve both entry paths for command-like features:
   - If the command contains a usable argument, execute immediately.
   - If the command lacks the argument, send a targeted `ForceReply` prompt and wait for the user's reply.
3. Keep the user informed during long work:
   - Send an immediate acknowledgement after input is accepted.
   - Send meaningful progress messages at major phases so the user never thinks the command failed or hung.
   - Track transient status message IDs and delete them after the final result or final error has been sent.
4. Render final outputs with the output format the feature asks for. If expandable quote output is requested, use Telegram `ParseModeHTML` and convert the Markdown body to Telegram-safe HTML before wrapping it.
5. Add or update tests for both direct-command and reply-driven paths, including unrelated text that should be ignored.

## Optional args + ForceReply pattern

For commands that need one piece of user input, prefer this behavior:

- `/command value` or `/command https://example.test/...` executes directly.- `/command` sends a prompt message with `ForceReply{ForceReply: true, Selective: true}` a nd an `InputFieldPlaceholder`.
- The reply handler only accepts the intended user's reply to the prompt message in the same chat.
- Unrelated messages, replies to other bot messages, and text from other users are ignored safely.

Implementation checklist:

- Preserve the original command message ID and set `ReplyToMessageID` on bot prompts/responses where the user expects a threaded interaction.
- When sending the prompt, reply to the user's command message and include ForceReply:

```go
ReplyMarkup: models.ForceReply{
    ForceReply:            true,
    Selective:             true,
    InputFieldPlaceholder: "Send the <INPUT> to <ACTION>",
},
```

- Store enough pending-prompt context in the existing reply prompt store (`ReplyPromptStore` / `PendingReply`) to validate the reply: chat ID, user ID, prompt message ID, command kind, and creation time if expiration exists.
- Route normal slash-command updates through the command handler and reply updates through the default handler or shared text handler.
- Configure `telegram.WithDefaultHandler` so unmatched updates are handled intentionally instead of producing noisy `[TGBOT] [UPDATE]` dumps.
- Keep the default handler conservative: it should process recognized replies and otherwise ignore text.

## Progress messages and cleanup

Long-running commands should show visible progress through the existing `service.ProgressNotifier` path where applicable. A good transcribe-summarize progression is usually:

1. Accepted input / validating request.
2. Fetching or downloading source data.
3. Transcribing or extracting text if applicable.
4. Summarizing or formatting.
5. Sending final result.

Treat these messages as transient UI:

- Record each sent progress message ID as soon as Telegram returns it.
- Delete transient progress messages after the final result is sent successfully.
- On failure, send a concise final error first, then remove stale progress messages if doing so will not hide the error context.
- Do not delete the user's command/reply or the final result.
- Do not rely on a single early “working…” message for multi-minute tasks; update or send phase-specific messages.

Tests should verify that cleanup calls target only intermediate status messages and do not target the final result.

## Expandable quote output

When the requested output style is an expandable quote, use `ParseModeHTML` and produce Telegram-safe HTML:

```html
<blockquote expandable>...rendered body...</blockquote>
```

Avoid sending raw Markdown inside the blockquote. Telegram will not parse Markdown inside `ParseModeHTML`; users would see `##`, `**bold**`, bullets, or backticks literally if you merely escape the whole Markdown string.

Rendering rules:

- Parse Markdown structure outside code fences before wrapping in `<blockquote expandable>`.
- Escape all original text (e.g. generated by LLM) before inserting it into HTML tags.
- Convert supported Markdown emphasis to Telegram HTML tags, e.g. `**bold**` to `<b>bold</b>` where safe.
- Convert headings to readable HTML/plain text appropriate for Telegram, such as `<b>Heading</b>` with spacing.
- Convert lists into readable lines using escaped bullet text; preserve indentation when useful.
- Preserve fenced-code contents literally: do not parse Markdown inside fences, and escape the code text as text.
- Render inline-code Markdown inside expandable blockquote bodies as italic escaped text rather than `<code>`, because Telegram expandable blockquotes cannot contain `<code>` entities reliably.
- Keep link conversion conservative; only emit Telegram-supported HTML for URLs you can safely escape.
- Budget message length for the complete generated HTML, including blockquote wrappers, prefixes, and continuation titles like `(1/N)`.

Edge cases to test:

- A heading followed by paragraphs and bullets.
- Bold text and inline-code markers in normal prose.
- A fenced code block containing Markdown-looking text such as `## not a heading` or `- not a list`.
- Existing HTML-sensitive characters like `<`, `>`, and `&`.
- Chunked outputs with continuation titles.

## Review checklist

Before calling the Telegram bot work complete, confirm:

- Direct command arguments still work.
- Missing arguments produce a selective ForceReply prompt.
- Replies are tied to the correct prompt/user/chat.
- The default handler is quiet for unrelated text.
- Long-running tasks emit multiple progress states.
- Transient progress messages are cleaned up after the final result.
- Expandable quote output uses `ParseModeHTML` and Telegram-safe HTML, not raw Markdown.
- Default output headings still match the formatter's expected section titles.
- Tests cover direct args, interactive replies, wrong-user replies, ignored unrelated messages, progress cleanup, and Markdown/HTML rendering edge cases.

