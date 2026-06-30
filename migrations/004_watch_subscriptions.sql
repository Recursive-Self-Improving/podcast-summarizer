CREATE TABLE IF NOT EXISTS watch_feeds (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    provider TEXT NOT NULL,
    provider_feed_id TEXT NOT NULL,
    canonical_url TEXT NOT NULL,
    title TEXT,
    status TEXT NOT NULL,
    last_checked_at TEXT,
    last_error TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(provider, provider_feed_id)
);

CREATE TABLE IF NOT EXISTS watch_subscriptions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    feed_id INTEGER NOT NULL,
    chat_id INTEGER NOT NULL,
    chat_type TEXT NOT NULL,
    chat_title TEXT,
    created_by_user_id INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(feed_id, chat_id),
    FOREIGN KEY(feed_id) REFERENCES watch_feeds(id)
);

CREATE TABLE IF NOT EXISTS watch_episodes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    feed_id INTEGER NOT NULL,
    provider_episode_id TEXT NOT NULL,
    media_item_id INTEGER,
    canonical_url TEXT NOT NULL,
    title TEXT,
    pub_date TEXT,
    status TEXT NOT NULL,
    first_seen_at TEXT NOT NULL,
    processed_at TEXT,
    last_error TEXT,
    UNIQUE(feed_id, provider_episode_id),
    CHECK(status != 'queued' OR media_item_id IS NOT NULL),
    FOREIGN KEY(feed_id) REFERENCES watch_feeds(id),
    FOREIGN KEY(media_item_id) REFERENCES media_items(id)
);

CREATE INDEX IF NOT EXISTS idx_watch_feeds_status_checked
ON watch_feeds(status, last_checked_at);

CREATE INDEX IF NOT EXISTS idx_watch_subscriptions_chat
ON watch_subscriptions(chat_id);

CREATE INDEX IF NOT EXISTS idx_watch_episodes_feed_status
ON watch_episodes(feed_id, status);

CREATE INDEX IF NOT EXISTS idx_watch_episodes_feed_pub_date
ON watch_episodes(feed_id, pub_date);
