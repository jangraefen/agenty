-- name: InsertRun :one
-- A run is stored as queued. A run that follows another joins its
-- conversation; any other run starts one of its own. It is returned as
-- stored, before a worker may claim it, with its harness.
INSERT INTO runs (id, harness_version_id, input, started_by, conversation_id, follows)
VALUES (
    sqlc.arg(id), sqlc.arg(harness_version_id), sqlc.arg(input), sqlc.arg(started_by),
    COALESCE((SELECT f.conversation_id FROM runs f WHERE f.id = sqlc.narg(follows)), sqlc.arg(id)),
    sqlc.narg(follows)
)
RETURNING sqlc.embed(runs),
    (SELECT hv.name FROM harness_versions hv WHERE hv.id = runs.harness_version_id)::text AS harness,
    (SELECT hv.version FROM harness_versions hv WHERE hv.id = runs.harness_version_id)::integer AS harness_version;

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

-- name: GetOwnRun :one
-- A run of the workspace, found only for the user who started it, who
-- started its conversation: only they follow it up.
SELECT sqlc.embed(runs), harness_versions.name AS harness, harness_versions.version AS harness_version
FROM runs
JOIN harness_versions ON harness_versions.id = runs.harness_version_id
WHERE runs.id = sqlc.arg(id) AND harness_versions.workspace = sqlc.arg(workspace)
  AND runs.started_by = sqlc.arg(owner);

-- name: ListAuditRuns :many
-- The runs of every workspace, newest first, each with its workspace,
-- optionally of one workspace, harness, starter or status, starting after
-- the run named by before. A before that is not a run matches nothing. For
-- auditors only.
SELECT sqlc.embed(runs), harness_versions.name AS harness, harness_versions.version AS harness_version,
       harness_versions.workspace
FROM runs
JOIN harness_versions ON harness_versions.id = runs.harness_version_id
WHERE (sqlc.narg(workspace)::text IS NULL OR harness_versions.workspace = sqlc.narg(workspace))
  AND (sqlc.narg(harness)::text IS NULL OR harness_versions.name = sqlc.narg(harness))
  AND (sqlc.narg(started_by)::text IS NULL OR runs.started_by = sqlc.narg(started_by))
  AND (sqlc.narg(status)::text IS NULL OR runs.status = sqlc.narg(status))
  AND (sqlc.narg(before)::text IS NULL
       OR (runs.created_at, runs.id) < (SELECT b.created_at, b.id FROM runs b WHERE b.id = sqlc.narg(before)))
ORDER BY runs.created_at DESC, runs.id DESC
LIMIT sqlc.arg(max_rows);

-- name: GetAuditRun :one
-- A run of any workspace, with its workspace. For auditors only.
SELECT sqlc.embed(runs), harness_versions.name AS harness, harness_versions.version AS harness_version,
       harness_versions.workspace
FROM runs
JOIN harness_versions ON harness_versions.id = runs.harness_version_id
WHERE runs.id = sqlc.arg(id);

-- name: FailRunningRuns :many
UPDATE runs
SET status = 'failed', error = $1, finished_at = now()
WHERE status = 'running'
RETURNING id, steps;

-- name: ConversationRuns :many
-- The runs of the conversation the run named by id belongs to, oldest first,
-- if that run is one of the workspace's.
SELECT sqlc.embed(runs), harness_versions.name AS harness, harness_versions.version AS harness_version
FROM runs
JOIN harness_versions ON harness_versions.id = runs.harness_version_id
WHERE harness_versions.workspace = sqlc.arg(workspace)
  AND runs.conversation_id = (SELECT c.conversation_id FROM runs c WHERE c.id = sqlc.arg(id))
ORDER BY runs.created_at, runs.id;

-- name: ClaimRun :one
-- Marks the oldest queued run as running and returns it with its workspace.
-- Concurrent claims skip each other's rows, so each run is claimed once.
WITH claimed AS (
    UPDATE runs
    SET status = 'running'
    WHERE status = 'queued' AND id = (
        SELECT q.id FROM runs q
        WHERE q.status = 'queued'
        ORDER BY q.created_at, q.id
        LIMIT 1
        FOR NO KEY UPDATE SKIP LOCKED)
    RETURNING runs.id, runs.harness_version_id
)
SELECT claimed.id, harness_versions.workspace
FROM claimed
JOIN harness_versions ON harness_versions.id = claimed.harness_version_id;

-- name: IdleRuns :many
-- The runs no worker holds that have not finished, queued or waiting, oldest
-- first, with their workspaces, harnesses and the users who started them.
SELECT runs.id, runs.status, harness_versions.workspace, harness_versions.name AS harness, runs.started_by AS owner
FROM runs
JOIN harness_versions ON harness_versions.id = runs.harness_version_id
WHERE runs.status IN ('queued', 'waiting')
ORDER BY runs.created_at, runs.id;

-- name: CancelIdleRun :many
UPDATE runs
SET status = 'cancelled', error = $2, finished_at = now()
WHERE id = $1 AND status IN ('queued', 'waiting')
RETURNING steps;

-- name: SetRunDigests :execrows
UPDATE runs
SET prompt_digest = $2, history_digest = $3
WHERE id = $1 AND status = 'running';

-- name: ListConversations :many
-- The conversations started_by started, in any workspace, latest activity
-- first, after the one with before_at and before_id when they are
-- set. A conversation is its first run, whose ID names it, and its latest
-- run, the one no run follows, whose creation is its latest activity.
SELECT first.id, harness_versions.workspace, harness_versions.name AS harness,
       left(first.input, 100)::text AS title, latest.status, latest.created_at AS updated_at
FROM runs first
JOIN harness_versions ON harness_versions.id = first.harness_version_id
JOIN runs latest ON latest.conversation_id = first.id
 AND NOT EXISTS (SELECT 1 FROM runs n WHERE n.follows = latest.id)
WHERE first.id = first.conversation_id
  AND first.started_by = sqlc.arg(started_by)
  AND (sqlc.narg(before_id)::text IS NULL
       OR (latest.created_at, first.id) < (sqlc.narg(before_at)::timestamptz, sqlc.narg(before_id)::text))
ORDER BY latest.created_at DESC, first.id DESC
LIMIT sqlc.arg(max_rows);

-- name: FindConversation :one
-- The conversation named by id, if started_by started it, in any workspace.
SELECT first.id, harness_versions.workspace, harness_versions.name AS harness,
       left(first.input, 100)::text AS title, latest.status
FROM runs first
JOIN harness_versions ON harness_versions.id = first.harness_version_id
JOIN runs latest ON latest.conversation_id = first.id
 AND NOT EXISTS (SELECT 1 FROM runs n WHERE n.follows = latest.id)
WHERE first.id = sqlc.arg(id) AND first.id = first.conversation_id
  AND first.started_by = sqlc.arg(started_by);
