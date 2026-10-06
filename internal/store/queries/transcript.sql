-- name: InsertRunMessage :exec
INSERT INTO run_messages (run_id, position, role, text, tool_calls, tool_results)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: RunMessages :many
SELECT * FROM run_messages
WHERE run_id = $1
ORDER BY position;
