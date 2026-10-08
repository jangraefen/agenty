-- name: LockAuditLog :exec
-- Serialises appends to the audit log until the transaction ends, so its
-- chain stays linear. Its two keys keep it apart from the one-key locks,
-- such as LockHarnessName's.
SELECT pg_advisory_xact_lock(hashtext('agenty'), hashtext('audit log'));

-- name: LastAuditEvent :one
-- The latest event's id and hash; call it holding the lock.
SELECT id, hash FROM audit_events ORDER BY id DESC LIMIT 1;

-- name: InsertAuditEvent :exec
INSERT INTO audit_events (id, recorded_at, actor, action, workspace, run_id, target, details, prev_hash, hash)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: RunOwner :one
-- Who started the run, and in which workspace, for its events.
SELECT runs.started_by, harness_versions.workspace
FROM runs
JOIN harness_versions ON harness_versions.id = runs.harness_version_id
WHERE runs.id = $1;

-- name: ToolEventsOfRun :many
-- What the tool gateway recorded for a run, in order.
SELECT action, details, recorded_at FROM audit_events
WHERE run_id = $1 AND action IN ('tool.decision', 'tool.approval', 'tool.result')
ORDER BY id;

-- name: LastAuditEventID :one
-- The latest event's id, 0 before the first.
SELECT COALESCE(max(id), 0)::bigint FROM audit_events;

-- name: AuditEventsAfter :many
-- The events after the one with the given id up to the one with the last,
-- in order, a page at a time.
SELECT * FROM audit_events WHERE id > sqlc.arg(after) AND id <= sqlc.arg(last) ORDER BY id LIMIT sqlc.arg(max_rows);
