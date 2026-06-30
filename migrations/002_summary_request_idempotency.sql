WITH ranked_message_duplicates AS (
    SELECT id,
           ROW_NUMBER() OVER (
               PARTITION BY chat_id, message_id
               ORDER BY CASE status
                   WHEN 'sent' THEN 1
                   WHEN 'delivery_unknown' THEN 2
                   WHEN 'sending' THEN 3
                   WHEN 'failed' THEN 4
                   ELSE 5
               END, id
           ) AS survivor_rank
    FROM summary_requests
    WHERE message_id IS NOT NULL
)
UPDATE summary_requests
SET message_id = NULL,
    status = CASE
        WHEN status IN ('pending_transcript', 'pending_summary', 'summarizing', 'sending') THEN 'failed'
        ELSE status
    END,
    error = COALESCE(NULLIF(error, ''), 'duplicate Telegram message replay before idempotency migration'),
    updated_at = CURRENT_TIMESTAMP
WHERE id IN (SELECT id FROM ranked_message_duplicates WHERE survivor_rank > 1);

CREATE UNIQUE INDEX IF NOT EXISTS idx_summary_requests_chat_message
ON summary_requests(chat_id, message_id)
WHERE message_id IS NOT NULL;
