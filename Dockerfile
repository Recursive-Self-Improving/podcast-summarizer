# syntax=docker/dockerfile:1.7

# ----- Builder stage: compile the Go binary -----
FROM golang:1.27.3-bookworm AS builder

WORKDIR /src

# Cache module downloads separately from source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Static, stripped binary; modernc.org/sqlite is pure Go so CGO is unnecessary.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/podcast-summarizer ./cmd/bot

# ----- Runtime stage: Python + external media tools -----
FROM node:24.22.1-bookworm-slim AS node
FROM python:3.13.17-slim-bookworm AS runtime

# DLA-4855-1: refresh the system decoder used by curl and other native clients.
RUN apt-get update \
    && apt-get install -y --no-install-recommends libbrotli1 \
    && dpkg --compare-versions "$(dpkg-query -W -f='${Version}' libbrotli1)" ge '1.0.9-2+deb12u1' \
    && rm -rf /var/lib/apt/lists/*

COPY --from=node /usr/local/bin/node /usr/local/bin/node

# ffmpeg: audio extraction/splitting.
# nodejs: required by yt-dlp 2026.x for YouTube JS challenges (--js-runtimes node).
# yt-dlp: subtitle and audio downloads.
# ca-certificates curl: TLS + health for yt-dlp remote components.
RUN apt-get update && apt-get install -y --no-install-recommends \
        ffmpeg \
        ca-certificates=20250419~deb12u2 \
        curl \
    && rm -rf /var/lib/apt/lists/* \
    && pip install --no-cache-dir --upgrade 'pip==26.2.2' \
    && pip install --no-cache-dir 'certifi>=2026.10.19' yt-dlp faster-whisper

# Non-root user for the long-running bot process.
# Create /data as root and hand it to the non-root user so the named volume
# (which Docker mounts as root-owned by default) is writable at first run.
RUN useradd --create-home --uid 1000 podcast \
    && mkdir -p /data /tmp/podcast /home/podcast/.cache/huggingface \
    && chown -R podcast:podcast /data /tmp/podcast /home/podcast/.cache
USER podcast
WORKDIR /home/podcast

# App data lives under /data; the SQLite DB and temp files persist via volumes.
ENV SQLITE_PATH=/data/bot.db \
    TEMP_ROOT=/tmp/podcast \
    HF_HOME=/home/podcast/.cache/huggingface \
    PYTHON_PATH=python3 \
    YT_DLP_PATH=yt-dlp \
    FFMPEG_PATH=ffmpeg

COPY --from=builder --chown=podcast:podcast /out/podcast-summarizer /usr/local/bin/podcast-summarizer

VOLUME ["/data", "/home/podcast/.cache/huggingface"]

CMD ["podcast-summarizer"]
