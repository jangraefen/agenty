-- name: InsertRun :exec
-- A run that follows another joins its conversation; any other run starts
-- one of its own.
INSERT INTO runs (id, harness_version_id, input, started_by, prompt_digest, conversation_id, follows)
VALUES (
    sqlc.arg(id), sqlc.arg(harness_version_id), sqlc.arg(input), sqlc.arg(started_by), sqlc.arg(prompt_digest),
    COALESCE((SELECT f.conversation_id FROM runs f WHERE f.id = sqlc.narg(follows)), sqlc.arg(id)),
    sqlc.narg(follows)
);

-- name: FinishRun :execrows
UPDATE runs
SET status = $2, output = $3, steps = $4, error = $5, finished_at = now()
WHERE id = $1 AND status = 'running';

-- name: GetRun :one
-- A run is found only in the workspace of the harness version it runs.
SELECT sqlc.embed(runs), harness_versions.name AS harness, harness_versions.version AS harness_version
FROM runs
JOIN harness_versions ON harness_versions.id = runs.harness_version_id
WHERE runs.id = sqlc.arg(id) AND harness_versions.workspace = sqlc.arg(workspace);

-- name: ListRuns :many
-- The runs of a workspace, newest first, optionally of one harness or with
-- one status, starting after the run named by before. A before that is not
-- a run of the workspace matches nothing.
SELECT sqlc.embed(runs), harness_versions.name AS harness, harness_versions.version AS harness_version
FROM runs
JOIN harness_versions ON harness_versions.id = runs.harness_version_id
WHERE harness_versions.workspace = sqlc.arg(workspace)
  AND (sqlc.narg(harness)::text IS NULL OR harness_versions.name = sqlc.narg(harness))
  AND (sqlc.narg(status)::text IS NULL OR runs.status = sqlc.narg(status))
  AND (sqlc.narg(before)::text IS NULL
       OR (runs.created_at, runs.id) < (
           SELECT b.created_at, b.id FROM runs b
           JOIN harness_versions bv ON bv.id = b.harness_version_id
           WHERE b.id = sqlc.narg(before) AND bv.workspace = sqlc.arg(workspace)))
ORDER BY runs.created_at DESC, runs.id DESC
LIMIT sqlc.arg(max_rows);

-- name: FailRunningRuns :execrows
UPDATE runs
SET status = 'failed', error = $1, finished_at = now()
WHERE status = 'running';

-- name: ConversationRuns :many
-- The runs of the conversation the run named by id belongs to, oldest first,
-- if that run is one of the workspace's.
SELECT sqlc.embed(runs), harness_versions.name AS harness, harness_versions.version AS harness_version
FROM runs
JOIN harness_versions ON harness_versions.id = runs.harness_version_id
WHERE harness_versions.workspace = sqlc.arg(workspace)
  AND runs.conversation_id = (SELECT c.conversation_id FROM runs c WHERE c.id = sqlc.arg(id))
ORDER BY runs.created_at, runs.id;
