-- name: InsertRun :exec
INSERT INTO runs (id, harness_version_id, input)
VALUES ($1, $2, $3);

-- name: FinishRun :execrows
UPDATE runs
SET status = $2, output = $3, steps = $4, error = $5, finished_at = now()
WHERE id = $1 AND status = 'running';

-- name: GetRun :one
SELECT * FROM runs
WHERE id = $1;

-- name: FailRunningRuns :execrows
UPDATE runs
SET status = 'failed', error = $1, finished_at = now()
WHERE status = 'running';
