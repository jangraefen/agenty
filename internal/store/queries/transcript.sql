-- name: InsertRunMessage :exec
INSERT INTO run_messages (run_id, position, role, text, tool_calls, tool_results, provider, provider_data)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: RunMessages :many
SELECT * FROM run_messages
WHERE run_id = $1
ORDER BY position;
