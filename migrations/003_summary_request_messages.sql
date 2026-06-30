CREATE TABLE IF NOT EXISTS summary_request_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    summary_request_id INTEGER NOT NULL,
    chat_id INTEGER NOT NULL,
    telegram_message_id INTEGER NOT NULL,
    kind TEXT NOT NULL,
    deleted_at TEXT,
    created_at TEXT NOT NULL,
    FOREIGN KEY(summary_request_id) REFERENCES summary_requests(id)
);

CREATE INDEX IF NOT EXISTS idx_summary_request_messages_request_kind_active
ON summary_request_messages(summary_request_id, kind, deleted_at);

CREATE INDEX IF NOT EXISTS idx_summary_request_messages_request_active
ON summary_request_messages(summary_request_id, deleted_at);
