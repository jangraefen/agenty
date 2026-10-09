-- name: InsertRunMessage :exec
-- Stores a message and adds its tokens to its run's. One statement does
-- both, so a run's totals always equal the sum of its stored messages. The
-- primary key refuses a position written before, and the foreign key a run
-- that is not stored.
WITH message AS (
    INSERT INTO run_messages (
        run_id, position, role, text, tool_calls, tool_results, provider, provider_data, altered,
        input_tokens, output_tokens, cache_write_tokens, cache_read_tokens
    )
    VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
    RETURNING run_id, input_tokens, output_tokens, cache_write_tokens, cache_read_tokens
)
UPDATE runs
SET input_tokens       = runs.input_tokens + message.input_tokens,
    output_tokens      = runs.output_tokens + message.output_tokens,
    cache_write_tokens = runs.cache_write_tokens + message.cache_write_tokens,
    cache_read_tokens  = runs.cache_read_tokens + message.cache_read_tokens
FROM message
WHERE runs.id = message.run_id;

-- name: RunMessages :many
-- A run's transcript in order. A run without messages, or one that does not
-- exist, has none.
SELECT * FROM run_messages
WHERE run_id = $1
ORDER BY position;
