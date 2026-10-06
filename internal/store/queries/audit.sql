-- name: InsertAuditRecord :exec
INSERT INTO audit_records (run_id, call_id, event, tool, args, decision, reason, approver, result, error)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: AuditRecordsOfRun :many
SELECT * FROM audit_records
WHERE run_id = $1
ORDER BY id;
