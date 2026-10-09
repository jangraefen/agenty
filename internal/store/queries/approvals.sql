-- name: InsertApproval :exec
-- Stores a pending request; SuspendRun calls it in the transaction that
-- marks its run as waiting. The unique index approvals_pending refuses a
-- second pending request of one run.
INSERT INTO approvals (id, run_id, call_id, call_index, tool, args, reasons, results, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: SuspendRun :execrows
-- Marks a running run as waiting, so no worker holds it. It changes no row
-- for a run that is not running, which the caller reports as not found.
UPDATE runs SET status = 'waiting'
WHERE id = $1 AND status = 'running';

-- name: PendingApprovals :many
-- The waiting approval requests of the runs of a workspace that owner
-- started, oldest first.
SELECT sqlc.embed(approvals), harness_versions.name AS harness
FROM approvals
JOIN runs ON runs.id = approvals.run_id
JOIN harness_versions ON harness_versions.id = runs.harness_version_id
WHERE harness_versions.workspace = sqlc.arg(workspace) AND runs.started_by = sqlc.arg(owner)
  AND approvals.status = 'pending'
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
-- returns its run. The workspace is part of the match, so a request cannot
-- be answered through another workspace's URL. Matching only a pending
-- request makes the answer happen once; the row lock of the update makes a
-- concurrent answer, expiry or withdrawal wait and then match nothing.
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
-- Like AnswerApproval, it locks requests before their runs.
UPDATE approvals
SET status = 'rejected', reason = $1, answered_at = now()
WHERE status = 'pending' AND expires_at <= now()
RETURNING run_id;

-- name: QueueWaitingRuns :execrows
-- Puts waiting runs back in the queue once their requests are answered or
-- expired. Only waiting runs move, so a run cancelled meanwhile stays
-- cancelled.
UPDATE runs SET status = 'queued'
WHERE id = ANY(sqlc.arg(ids)::text[]) AND status = 'waiting';

-- name: NextApprovalExpiry :one
-- When the next pending request expires, from the index approvals_expiring.
SELECT expires_at FROM approvals WHERE status = 'pending'
ORDER BY expires_at
LIMIT 1;

-- name: WithdrawApprovals :exec
-- Withdraws the pending request of a run that is queued or waiting. It locks
-- the request before the run, as answering and expiring do, so they never
-- wait for each other in a circle.
UPDATE approvals
SET status = 'withdrawn', reason = $2, answered_at = now()
WHERE run_id = $1 AND status = 'pending'
  AND run_id IN (SELECT id FROM runs WHERE id = $1 AND status IN ('queued', 'waiting'));

-- name: GetApproval :one
-- An approval request of a run, with its harness.
SELECT sqlc.embed(approvals), harness_versions.name AS harness
FROM approvals
JOIN runs ON runs.id = approvals.run_id
JOIN harness_versions ON harness_versions.id = runs.harness_version_id
WHERE approvals.run_id = sqlc.arg(run_id) AND approvals.id = sqlc.arg(id);
