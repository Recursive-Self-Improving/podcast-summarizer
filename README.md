# Podcast Summarizer

## Brotli security baseline

The container build requires Debian Bookworm `libbrotli1 >= 1.0.9-2+deb12u1`
([CVE-2026-111497 / DLA-4855-1](https://security-tracker.debian.org/tracker/CVE-2026-111497)).
Node 24.22.1 fixes its bundled decoder independently. Rebuild affected images and
devcontainers without cache, then redeploy; restarting an old image does not patch it.

Telegram bot that turns podcast/video URLs into Chinese investment-oriented summaries. It normalizes supported media URLs, reuses subtitles when available, falls back to faster-whisper transcription, summarizes through the OpenAI Responses API, caches results in SQLite, and sends rich Telegram summaries with language switch buttons.

Supported media sources:

- YouTube
- Xiaoyuzhou
- SoundOn

## Configuration

Create a `.env` file from the example:

```sh
cp .env.example .env
```

The bot loads `.env` without overriding existing shell environment variables. See [`.env.example`](.env.example) for the full configuration reference.

Required values:

```dotenv
TELEGRAM_BOT_TOKEN=123456:telegram-token
BOT_OWNER_ID=123456789
OPENAI_API_KEY=sk-...
OPENAI_MODEL=gpt-4.1
```

Common optional values:

```dotenv
# OpenAI-compatible endpoint; defaults to OpenAI.
OPENAI_BASE_URL=https://api.openai.com/v1

# Default summary language for new requests: zh-hans or zh-hant.
DEFAULT_SUMMARY_VARIANT=zh-hans

# faster-whisper defaults.
WHISPER_MODEL=small
WHISPER_DEVICE=cpu
WHISPER_COMPUTE=int8
WHISPER_SEGMENT_SECONDS=300

# Extra yt-dlp args for current YouTube JS challenges.
YT_DLP_ARGS=--extractor-args youtube:player_client=android_vr --js-runtimes node --remote-components ejs:github
```

`YT_DLP_ARGS` applies to both subtitle and audio downloads. Use it for deployment-specific extractor flags (for example, `--extractor-args "youtube:player_client=mweb"`), cookies (`--cookies /path/to/cookies.txt`), or `--config-location /path/to/yt-dlp.conf`. Keep `youtube:player_client=...` attached to `--extractor-args`, never as a standalone argument. YouTube JS challenge handling needs Node.js or Deno; the example above selects Node.js and the remote EJS solver. If using Deno instead, select it with `--js-runtimes deno`.

`YT_DLP_PATH`, `FFMPEG_PATH`, and `PYTHON_PATH` default to `yt-dlp`, `ffmpeg`, and `python3`. Set absolute paths when a deployment user or systemd service has a restricted `PATH`. The selected Python environment must have `faster-whisper` installed.

Outside Docker, leave `TEMP_ROOT` empty to use Go's system temp directory (`TMPDIR` or `/tmp`). Set it to a directory writable by the service user only when the system temp directory is unsuitable.

For Docker, leave `SQLITE_PATH` and `TEMP_ROOT` unset in `.env` unless you intentionally want to override the container defaults. `docker-compose.yml` sets:

- `SQLITE_PATH=/data/bot.db`
- `TEMP_ROOT=/tmp/podcast`

## Run with Docker Compose

Build and start the bot:

```sh
docker compose up -d --build
```

Follow logs:

```sh
docker compose logs -f podcast-summarizer
```

Stop the bot:

```sh
docker compose down
```

Stop and remove persistent data/model cache volumes:

```sh
docker compose down -v
```

Compose uses two named volumes:

- `podcast-data` mounted at `/data` for SQLite and app data.
- `podcast-model-cache` mounted at `/home/podcast/.cache/huggingface` so faster-whisper downloads the model once and reuses it across restarts.

The runtime image is CPU-only. It includes the Go bot binary, `yt-dlp`, `ffmpeg`, `python3`, `faster-whisper`, and `nodejs` for yt-dlp YouTube JS challenge handling.

## Local development

Use Go 1.27.3 or newer (see [`go.mod`](go.mod)), plus `yt-dlp`, `ffmpeg`, Python 3.13.17+ with `faster-whisper`, and Node.js 24.22.1+ for YouTube JS challenges. Container builds pin those runtimes, pip 26.2.2, patched Debian CA certificates, and certifi 2026.10.19 or newer; rebuild existing images to refresh trust stores. Configure `.env` as above, then use the existing Makefile targets:

```sh
make run    # Run the bot with go run ./cmd/bot.
make build  # Build bin/podcast-summarizer.
make check  # Format Go code, run go vet, and run all Go tests.
```

For build-only validation without repository artifacts, use `go build -o /tmp/podcast-summarizer-bot ./cmd/bot`. Avoid plain `go build ./cmd/bot`, which leaves a root-level `bot` binary.

Contributor conventions and implementation pitfalls are maintained in [AGENTS.md](AGENTS.md).

## Bot commands

User commands:

```text
/summarize_podcast [MEDIA_URL]
/summary_status [MEDIA_URL]
/help
```

Owner commands:

```text
/subscribe_podcast <PODCAST_URL>
/unsubscribe_podcast <PODCAST_URL>
/subscriptions
/allow_group [CHAT_ID[,CHAT_ID...]]
/remove_group [CHAT_ID[,CHAT_ID...]]
/allow_user <USER_ID>
/remove_user <USER_ID>
/whitelist
```

`/summarize_podcast` may return immediately if a transcript and summary cache already exist. Otherwise it sends progress updates while downloading, splitting, transcribing, summarizing, and delivering the final rich Telegram summary.

Omit the URL from `/summarize_podcast` or `/summary_status` to receive a targeted reply prompt. Reply to that prompt with the media URL; unrelated text is ignored.

The `简中` (`zh-hans`) and `繁中` (`zh-hant`) buttons switch only the current summary's language. They do not change the deployment's `DEFAULT_SUMMARY_VARIANT` for future requests.

## Workflow

```mermaid
flowchart TD
    U[Telegram user sends /summarize_podcast URL] --> B[Bot handler validates auth and parses URL]
    B --> P[Provider registry normalizes media URL]
    P --> DB[(SQLite: media, requests, cache, jobs)]
    DB --> T{Transcript already stored?}
    T -- yes --> S[Summarize transcript]
    T -- no --> Sub{Provider subtitle lookup supported?}
    Sub -- subtitle found --> SaveSub[Store subtitle transcript]
    SaveSub --> S
    Sub -- no subtitle / lookup failed --> Q[Create transcription job]
    Q --> W[Queue worker claims job]
    W --> A[Resolve and download audio]
    A --> C[ffmpeg converts to 16 kHz mono WAV]
    C --> L{Audio longer than segment size?}
    L -- no --> FW[faster-whisper transcribes WAV]
    L -- yes --> Split[ffmpeg splits WAV into segments]
    Split --> FW
    FW --> SaveWhisper[Store whisper transcript]
    SaveWhisper --> S
    S --> Cache{Summary cached for prompt + model?}
    Cache -- hit --> Send[Send cached Telegram summary]
    Cache -- miss --> OAI[OpenAI Responses API creates structured summary]
    OAI --> Store[Store summary cache]
    Store --> Send
    Send --> Done[Rich Telegram message + 简中/繁中 buttons]
```

### Transcription defaults

The fallback transcription path uses `faster-whisper`.

Default settings:

| Setting | Environment variable | Default |
| --- | --- | --- |
| Whisper model | `WHISPER_MODEL` | `small` |
| Device | `WHISPER_DEVICE` | `cpu` |
| Compute type | `WHISPER_COMPUTE` | `int8` |
| Audio split size | `WHISPER_SEGMENT_SECONDS` | `300` seconds / 5 minutes |

Long audio is converted to 16 kHz mono WAV first. If the WAV duration is greater than `WHISPER_SEGMENT_SECONDS`, ffmpeg splits it into `part_000.wav`, `part_001.wav`, etc.; otherwise the single WAV is passed to faster-whisper. The same segment size is also passed to the faster-whisper helper.

The faster-whisper Python helper is embedded in the Go binary and written into the audio work directory at runtime. Deployments do not need a separate copy of `scripts/faster_whisper_transcribe.py`.

## Crash recovery

The bot keeps durable state in SQLite, so Docker or process restarts do not lose queued work as long as the `podcast-data` volume is preserved.

On startup it recovers interrupted work before accepting new traffic:

- Transcription jobs left in `downloading_audio`, `converting_audio`, `splitting_audio`, or `transcribing` are moved back to `queued` when the media item still has no transcript.
- Summary requests left in `summarizing` are moved back to `pending_summary`.
- Summary requests left in `sending` become `delivery_unknown` because Telegram may or may not have received the final message before the crash.

While running, a recovery loop executes every minute:

- Summary requests stuck in `summarizing` for more than 10 minutes are requeued as `pending_summary`.
- Pending transcript requests without an active transcription job get a new `queued` transcription job.
- Media with pending summary requests is processed again, using cached transcripts and summaries when available.

Graceful shutdown is handled through SIGTERM/SIGINT. Docker Compose gives the container 30 seconds (`stop_grace_period: 30s`) to drain loops. If shutdown interrupts transcription, the worker requeues the job without incrementing the retry count. Non-cancellation transcription failures are retried up to two attempts by default.

Operational note: avoid `docker compose down -v` unless you intentionally want to delete recovery state. It removes the SQLite volume and the faster-whisper model cache.
