# CLAUDE.md

## Lessons

- `go mod tidy` removes dependencies that are not imported; for initial dependency setup before real usage, keep explicit placeholder imports until the packages are used directly.
- `.env` loading uses `github.com/joho/godotenv` v1.5.1 via `godotenv.Load`, so shell environment variables take precedence over `.env` values.
- Go embed patterns are relative to the declaring package directory; root-level `migrations/*.sql` are embedded from the module root package and consumed by `internal/db`.
- When renaming bot slash commands, avoid broad `/summarize` string replacement because it can corrupt the `internal/summarize` import path.
- `yt-dlp` can write subtitle files before exiting nonzero on YouTube extractor/client failures; parse generated subtitle files before returning the command error.
- YouTube extraction may require deployment-specific `yt-dlp` flags such as `--extractor-args "youtube:player_client=mweb"`, cookies, or `--config-location`; pass them through `YT_DLP_ARGS` so subtitle and audio downloads share the same environment.
- The faster-whisper Python helper is embedded in the Go binary and written into the audio workdir at runtime; do not rely on `scripts/faster_whisper_transcribe.py` being present on the deployment host.
- `internal/commandrunner` cancellation uses OS-specific helpers; keep Unix process-group code/tests behind `//go:build unix` and preserve `ctx.Err()` in cancellation errors.
- `exec.Cmd.WaitDelay` does not by itself bound waits after manual context cancellation; if using `exec.Command` plus custom cancellation, also bound post-kill receives from `cmd.Wait()`.
- Faster Whisper orphan-process protection belongs in `internal/transcribe.Helper` immediately before launching Python; match existing Python command lines containing `faster_whisper_transcribe.py`, `transcribe.py`, or `extract.py` and keep the wait context-cancelable.
- `github.com/go-telegram/bot` logs unmatched updates via its default handler; set `telegram.WithDefaultHandler` when constructing the bot to avoid noisy `[TGBOT] [UPDATE]` dumps.
- Telegram HTML summary chunks must budget for all generated wrappers and prefixes, including continuation titles like `(1/N)`, before sending with `ParseModeHTML`.
- Keep the default summary prompt's Markdown `##` headings exactly aligned with the Telegram summary formatter's expected section titles.
- Summary variants are language-only buttons: `简中` (`zh-hans`) and `繁中` (`zh-hant`); legacy callback aliases `js`/`fs` are accepted, but retired long codes `jl`/`fl` are rejected.
- Telegram expandable summaries use `ParseModeHTML`; convert Markdown body formatting to Telegram HTML before wrapping it in `<blockquote expandable>` instead of escaping and sending raw Markdown.
- Telegram expandable blockquotes cannot contain `<code>` entities; render inline-code Markdown as escaped plain text inside summary bodies.
- Telegram reply-based interactions need `ReplyToMessageID` carried through `internal/bot.Message` and a safe default handler that ignores unrelated text.
- Telegram prompts that require the user to reply should send `models.ForceReply{ForceReply: true, Selective: true}` with an input placeholder, not just a normal reply message.
- Reusable Telegram bot UX patterns belong in `.claude/skills/telegram-bot-interactions`: optional args fall back to ForceReply, long tasks send cleanup-tracked progress messages, and expandable quote output converts Markdown to Telegram-safe `ParseModeHTML`.
- Provider parsing canonicalizes accepted media URL variants and rejects encoded path separators like `%2F`; downstream subtitle/audio code should receive canonical page URLs or resolved direct audio sources rather than raw user URLs.
- SoundOn media IDs are stored as `{podcastID}/{episodeID}` so queue-time RSS lookup can derive `https://feeds.soundon.fm/podcasts/{podcastID}.xml` while matching the episode by GUID/link.
- New Go HTTP paths for provider metadata or direct audio must use bounded clients/timeouts, cap response sizes where practical, and check file close errors after streaming downloads.
- `go build ./cmd/bot` writes a root-level `bot` binary; remove it before committing or use `go build -o /tmp/podcast-summarizer-bot ./cmd/bot` for validation.
- Final summary display metadata belongs in the sender/rendering layer, not prepended to `summary_cache.summary_text`, so summary variant caches stay pure and the five-heading formatter keeps working.
