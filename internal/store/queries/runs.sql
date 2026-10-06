-- name: InsertRun :exec
INSERT INTO runs (id, harness_version_id, input, started_by)
VALUES ($1, $2, $3, $4);

-- name: FinishRun :execrows
UPDATE runs
SET status = $2, output = $3, steps = $4, error = $5, finished_at = now()
WHERE id = $1 AND status = 'running';

-- name: GetRun :one
-- A run is found only in the workspace of the harness version it runs.
SELECT runs.* FROM runs
JOIN harness_versions ON harness_versions.id = runs.harness_version_id
WHERE runs.id = sqlc.arg(id) AND harness_versions.workspace = sqlc.arg(workspace);

-- name: FailRunningRuns :execrows
UPDATE runs
SET status = 'failed', error = $1, finished_at = now()
WHERE status = 'running';
