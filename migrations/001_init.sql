CREATE TABLE IF NOT EXISTS auth_whitelisted_groups (
    chat_id INTEGER PRIMARY KEY,
    title TEXT,
    created_at TEXT NOT NULL,
    created_by_user_id INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS auth_whitelisted_dm_users (
    user_id INTEGER PRIMARY KEY,
    username TEXT,
    first_name TEXT,
    created_at TEXT NOT NULL,
    created_by_user_id INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS media_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    provider TEXT NOT NULL,
    provider_media_id TEXT NOT NULL,
    canonical_url TEXT NOT NULL,
    title TEXT,
    duration_seconds INTEGER,
    status TEXT NOT NULL,
    status_detail TEXT,
    transcript_source TEXT,
    transcript_text TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(provider, provider_media_id)
);

CREATE TABLE IF NOT EXISTS transcription_jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    media_item_id INTEGER NOT NULL,
    status TEXT NOT NULL,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    created_at TEXT NOT NULL,
    started_at TEXT,
    finished_at TEXT,
    FOREIGN KEY(media_item_id) REFERENCES media_items(id)
);

CREATE TABLE IF NOT EXISTS summary_cache (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    media_item_id INTEGER NOT NULL,
    prompt_hash TEXT NOT NULL,
    prompt_text TEXT NOT NULL,
    summary_text TEXT NOT NULL,
    model TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(media_item_id, prompt_hash, model),
    FOREIGN KEY(media_item_id) REFERENCES media_items(id)
);

CREATE TABLE IF NOT EXISTS summary_requests (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    media_item_id INTEGER NOT NULL,
    chat_id INTEGER NOT NULL,
    user_id INTEGER NOT NULL,
    message_id INTEGER,
    prompt_hash TEXT NOT NULL,
    prompt_text TEXT NOT NULL,
    status TEXT NOT NULL,
    summary_cache_id INTEGER,
    error TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY(media_item_id) REFERENCES media_items(id),
    FOREIGN KEY(summary_cache_id) REFERENCES summary_cache(id)
);

CREATE INDEX IF NOT EXISTS idx_summary_requests_media_status ON summary_requests(media_item_id, status);
CREATE INDEX IF NOT EXISTS idx_transcription_jobs_status_created ON transcription_jobs(status, created_at);

CREATE UNIQUE INDEX IF NOT EXISTS idx_transcription_jobs_one_active_per_media
ON transcription_jobs(media_item_id)
WHERE status IN ('queued', 'downloading_audio', 'converting_audio', 'splitting_audio', 'transcribing');
