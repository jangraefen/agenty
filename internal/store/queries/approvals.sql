-- name: InsertApproval :exec
INSERT INTO approvals (id, run_id, call_id, call_index, tool, args, reasons, results, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: SuspendRun :execrows
UPDATE runs SET status = 'waiting'
WHERE id = $1 AND status = 'running';

-- name: PendingApprovals :many
-- The waiting approval requests of a workspace, oldest first.
SELECT sqlc.embed(approvals), harness_versions.name AS harness
FROM approvals
JOIN runs ON runs.id = approvals.run_id
JOIN harness_versions ON harness_versions.id = runs.harness_version_id
WHERE harness_versions.workspace = sqlc.arg(workspace) AND approvals.status = 'pending'
ORDER BY approvals.created_at, approvals.id;

-- name: LatestApproval :one
-- The latest approval request of a run, with its harness.
SELECT sqlc.embed(approvals), harness_versions.name AS harness
FROM approvals
JOIN runs ON runs.id = approvals.run_id
JOIN harness_versions ON harness_versions.id = runs.harness_version_id
WHERE approvals.run_id = $1
ORDER BY approvals.created_at DESC, approvals.id DESC
LIMIT 1;

-- name: AnswerApproval :one
-- Answers a pending, unexpired request of a run of the workspace, and
-- returns its run.
UPDATE approvals
SET status = sqlc.arg(status), approver = sqlc.arg(approver), reason = sqlc.arg(reason), answered_at = now()
FROM runs, harness_versions
WHERE approvals.id = sqlc.arg(id) AND approvals.run_id = sqlc.arg(run_id)
  AND runs.id = approvals.run_id AND harness_versions.id = runs.harness_version_id
  AND harness_versions.workspace = sqlc.arg(workspace)
  AND approvals.status = 'pending' AND approvals.expires_at > now()
RETURNING approvals.run_id;

-- name: ExpireApprovals :many
-- Rejects the pending requests that have expired, and returns their runs.
UPDATE approvals
SET status = 'rejected', reason = $1, answered_at = now()
WHERE status = 'pending' AND expires_at <= now()
RETURNING run_id;

-- name: QueueWaitingRuns :execrows
UPDATE runs SET status = 'queued'
WHERE id = ANY(sqlc.arg(ids)::text[]) AND status = 'waiting';

-- name: NextApprovalExpiry :one
SELECT expires_at FROM approvals WHERE status = 'pending'
ORDER BY expires_at
LIMIT 1;

-- name: WithdrawApprovals :exec
UPDATE approvals
SET status = 'withdrawn', reason = $2, answered_at = now()
WHERE run_id = $1 AND status = 'pending';
