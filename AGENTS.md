# Lessons

- CVE-2026-111497 affects native Brotli, not pure-Go compression implementations. Node 24.22.1 fixes its bundled decoder, but curl/native clients still need the separate Bookworm `libbrotli1 >= 1.0.9-2+deb12u1` floor (DLA-4855-1). Explicitly install and check that package: an apt refresh alone may retain an old base-image dependency.

- A multi-language media image has independent trust stores: pin patched Go, Python, Node, pip's vendored CA bundle, certifi, and Debian ca-certificates together; upgrading only the Go binary leaves Python downloads exposed. Isolated CLI smoke must run outside the repository to avoid loading an operator's `.env` and contacting Telegram.

## Documentation and Go development

- Keep setup, configuration, operations, and user-facing behavior in [README.md](README.md); keep reusable engineering lessons in this file's `Lessons` section.
- `go mod tidy` removes dependencies that are not imported. Add real imports before tidying; do not retain placeholder imports from dependency-only scaffolding.
- Go embed patterns are relative to the declaring package directory; root-level `migrations/*.sql` are embedded from the module root package and consumed by `internal/db`.
- When renaming bot slash commands, avoid broad `/summarize` string replacement because it can corrupt the `internal/summarize` import path.

## Runtime and deployment

- `.env` loading uses `github.com/joho/godotenv` v1.5.1 via `godotenv.Load`, so shell environment variables take precedence over `.env` values.
- Runtime temp directory configuration is `TEMP_ROOT`; leave it empty to preserve Go's default temp behavior (`TMPDIR`/`/tmp`) and set it explicitly only for deployments where the service user cannot write the system temp directory.
- Runtime external tools (`YT_DLP_PATH`, `FFMPEG_PATH`, `PYTHON_PATH`) should support absolute paths because deployment users and systemd services may not inherit an interactive shell `PATH`.
- For yt-dlp 2026.x YouTube JS challenges, install a JS runtime (`nodejs` or `deno`) and select it with `--js-runtimes` alongside `--remote-components ejs:github`. Pass deployment-specific extractor flags, cookies, or `--config-location` through `YT_DLP_ARGS` so subtitle and audio downloads share the same environment. Keep `youtube:player_client=...` attached to `--extractor-args`, never as a standalone argument; the `.env.example` currently uses `android_vr`, while deployments may need another client such as `mweb`.
- The Docker runtime image (Dockerfile) is CPU-only and bakes in yt-dlp, ffmpeg, python3, faster-whisper, and nodejs; the Go binary is built statically (CGO_ENABLED=0, modernc.org/sqlite is pure Go) in a multi-stage build. The faster-whisper helper script is embedded in the Go binary and written into the audio workdir at runtime; do not rely on `scripts/faster_whisper_transcribe.py` being present on the deployment host or copied separately into the image.
- The Docker compose file mounts named volumes at `/data` (SQLite) and `/home/podcast/.cache/huggingface` (faster-whisper model cache) so the model downloads once and is reused across restarts; `HF_HOME` is set to that cache path in the Dockerfile.

## Media and subprocesses

- Provider parsing canonicalizes accepted media URL variants and rejects encoded path separators like `%2F`; downstream subtitle/audio code should receive canonical page URLs or resolved direct audio sources rather than raw user URLs.
- SoundOn media IDs are stored as `{podcastID}/{episodeID}` so queue-time RSS lookup can derive `https://feeds.soundon.fm/podcasts/{podcastID}.xml` while matching the episode by GUID/link.
- New Go HTTP paths for provider metadata or direct audio must use bounded clients/timeouts, cap response sizes where practical, and check file close errors after streaming downloads.
- `yt-dlp` can write subtitle files before exiting nonzero on YouTube extractor/client failures; parse generated subtitle files before returning the command error.
- `internal/commandrunner` cancellation uses OS-specific helpers; keep Unix process-group code/tests behind `//go:build unix` and preserve `ctx.Err()` in cancellation errors.
- `exec.Cmd.WaitDelay` does not by itself bound waits after manual context cancellation; if using `exec.Command` plus custom cancellation, also bound post-kill receives from `cmd.Wait()`.
- Faster Whisper orphan-process protection belongs in `internal/transcribe.Helper` immediately before launching Python. Match Python script basenames `faster_whisper_transcribe.py`, `transcribe.py`, or `extract.py`, not arbitrary command-line substrings, and keep the wait context-cancelable.

## Telegram command interactions

- The `bot.Command` struct carries parsed command results (URL, Prompt, ChatIDs []int64, SkippedChatIDs []string, UserID); `ChatIDs` replaced the former single `ChatID int64`+`HasChatID bool` to support comma-separated `/allow_group` and `/remove_group`. `SkippedChatIDs` is the only field carrying parse diagnostics (tolerated invalid tokens) rather than a parsed value — acceptable because it keeps `ParseCommand`'s single-return signature stable for its many callers. The `WhitelistManager` upsert/remove are idempotent (`ON CONFLICT` / `DELETE`), so looping them over multiple IDs is safe and a mid-loop error leaves only partial-but-retryable state.
- `github.com/go-telegram/bot` logs unmatched updates via its default handler; set `telegram.WithDefaultHandler` when constructing the bot to avoid noisy `[TGBOT] [UPDATE]` dumps and safely ignore unrelated text.
- Telegram reply-based interactions need `ReplyToMessageID` carried through `internal/bot.Message` so replies can be matched to the intended prompt, chat, and user.
- Telegram prompts that require the user to reply should send `models.ForceReply{ForceReply: true, Selective: true}` with an input placeholder, not just a normal reply message.
- Reuse the [Telegram bot interaction skill](.claude/skills/telegram-bot-interactions/SKILL.md): missing URL arguments fall back to ForceReply, long tasks send cleanup-tracked progress messages, and legacy expandable quote output converts Markdown to Telegram-safe `ParseModeHTML`. Rich-message rendering follows the separate rules below.

## Summary generation and delivery

### Rich messages

- Telegram Rich Messages (Bot API 10.1, June 11 2026) is a distinct feature from HTML parse mode: new `sendRichMessage` method takes a `rich_message` JSON parameter (`InputRichMessage`) with `html` or `markdown` field; `editMessageText` gained a `rich_message` parameter too. Rich messages support `<details>`/`<summary>` collapsible blocks, headings, lists, blockquotes, tables, 32,768 char limit ("Show more" after ~8000), 500 blocks, 16 nesting levels. `sendRichMessage` accepts `reply_markup` so inline keyboards work.
- The pinned `github.com/go-telegram/bot` v1.20.0 predates Bot API 10.1 and does not support `sendRichMessage`; `rawRequest` is unexported. To use Rich Messages, make direct HTTP POST calls to `https://api.telegram.org/bot<token>/sendRichMessage` and `/editMessageText` (with `rich_message`) via a bounded `http.Client`, bypassing the library only for those two operations.

- Rich messages render as real HTML, where bare `\n` collapses to a space (unlike legacy `parse_mode=HTML` text mode where `\n` is a line break). To get visual separation between section collapsibles, titles, paragraphs, and list items in rich messages, emit block-level tags (`<details>/<summary>`/`<p>`/`<ul><li>`/`<pre>`/`<h4>`/`<br>`) — never rely on `\n` between inline elements. The shared `renderSummaryMetadataHeaderHTML` and `renderSummaryMarkdownLineHTML` in `internal/bot/summary_html.go` are tuned for the legacy text path (newline-joined); the rich path in `internal/bot/richmessage.go` post-processes the header's `\n` into `<br>` and uses its own block-level `renderRichBlocksHTML` instead of the shared line renderer.
- The rich-message summary renders each detected investment section as its own `<details><summary>{title}</summary>{body}</details>` block (not one outer `<details>` wrapping all sections). This keeps the five subtitles ("核心摘要", "容易被忽略但有价值的信息", "直观地可以 bullish / bearish on 什么", "隐含地可以 bullish / bearish on 什么", "可能利好/利空的股票") always visible and individually collapsible, with no top-level "摘要" wrapper. When `detectSummarySections` cannot find the five sections, the whole body falls back to a single `<details><summary>摘要</summary>…</details>`. `renderRichBlocksHTML` (paragraphs/lists/pre/headings) is reused unchanged for each section's body.
- `detectSummarySections` must accept common LLM heading variants for the five investment sections (for example `bullish/bearish` without spaces, `可能利好、利空的股票` with Chinese punctuation, and simplified `容易被忽略但有价值的资讯` even though the prompt says `信息`). If one title is missed, rich-message rendering intentionally falls back to a single `<details><summary>摘要</summary>…</details>` block, which visually looks like the old one-big-expandable behavior.

### Variants, structured output, and cache

- Default summary output language is deployment-configurable via `DEFAULT_SUMMARY_VARIANT` (`zh-hans` or `zh-hant`). Keep the existing per-summary `简中`/`繁中` inline buttons one-off: they switch only that request's cache/message and do not update future defaults. Bot command parsing should leave default prompts empty so `SummaryService`/`StatusService` apply the configured default consistently for manual, ForceReply, watch, and `/summary_status` flows.
- Summary variants are language-only. Callback parsing accepts legacy aliases `js`/`fs` for `zh-hans`/`zh-hant`, but rejects retired long codes `jl`/`fl`; `DEFAULT_SUMMARY_VARIANT` accepts only the canonical language codes.
- Default investment-summary generation uses OpenAI Responses API structured output for built-in `zh-hans`/`zh-hant` prompts: `text.format` JSON schema with strict mode, language-neutral English keys, a validated `language` field, and non-empty string sections. The raw JSON is rendered into canonical Markdown with code-owned Chinese section titles before storing in `summary_cache.summary_text`; custom prompts stay on the legacy plain-text path and old Markdown renderer fallbacks remain for old/custom cache rows. `github.com/openai/openai-go/v3` v3.36.0 supports this via `responses.ResponseTextConfigParam{Format: responses.ResponseFormatTextConfigParamOfJSONSchema(...)}` plus `Strict` on `OfJSONSchema`.
- Structured summary validation should normalize misplaced section content before enforcing non-empty fields: OpenAI-compatible models can return schema-valid JSON where one string field contains all five Markdown headings while sibling fields are empty. Salvage those embedded sections into canonical fields, and do not make `detectSummarySections` fall back to one big `摘要` block just because one detected section body is empty; only missing/duplicate titles should force the one-block fallback.
- Keep the built-in prompt sections, code-owned Markdown `##` headings, and Telegram formatter's expected section titles aligned. Built-in prompts request structured JSON without Markdown headings inside field values; canonical headings are added by the renderer.
- Final summary display metadata belongs in the sender/rendering layer, not prepended to `summary_cache.summary_text`, so summary variant caches stay pure and the five-heading formatter keeps working.

### Legacy HTML summaries

- The legacy expandable-summary path uses `ParseModeHTML`; convert Markdown body formatting to Telegram HTML before wrapping it in `<blockquote expandable>` instead of escaping and sending raw Markdown. Do not apply these legacy blockquote rules to rich-message `<details>` blocks.
- Telegram expandable blockquotes cannot contain `<code>` entities; render inline-code Markdown as escaped text inside legacy summary bodies.
- Telegram HTML summary chunks must budget for all generated wrappers and prefixes, including continuation titles like `(1/N)`, before sending with `ParseModeHTML`.