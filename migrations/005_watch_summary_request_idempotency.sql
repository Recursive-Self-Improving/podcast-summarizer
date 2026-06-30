WITH ranked_watch_duplicates AS (
    SELECT id,
           ROW_NUMBER() OVER (
               PARTITION BY media_item_id, chat_id, prompt_hash
               ORDER BY CASE status
                   WHEN 'sent' THEN 1
                   WHEN 'delivery_unknown' THEN 2
                   WHEN 'sending' THEN 3
                   WHEN 'summarizing' THEN 4
                   WHEN 'pending_summary' THEN 5
                   WHEN 'pending_transcript' THEN 6
                   ELSE 7
               END, id
           ) AS survivor_rank
    FROM summary_requests
    WHERE message_id IS NULL AND status != 'failed'
)
UPDATE summary_requests
SET status = 'failed',
    error = COALESCE(NULLIF(error, ''), 'duplicate watch summary request before idempotency migration'),
    updated_at = CURRENT_TIMESTAMP
WHERE id IN (SELECT id FROM ranked_watch_duplicates WHERE survivor_rank > 1);

CREATE UNIQUE INDEX IF NOT EXISTS idx_summary_requests_watch_idempotency
ON summary_requests(media_item_id, chat_id, prompt_hash)
WHERE message_id IS NULL AND status != 'failed';
