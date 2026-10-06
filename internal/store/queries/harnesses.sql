-- name: LatestHarnessVersion :one
SELECT * FROM harness_versions
WHERE workspace = $1 AND name = $2
ORDER BY version DESC
LIMIT 1;

-- name: InsertHarnessVersion :one
INSERT INTO harness_versions (workspace, name, version, definition)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: LatestHarnessVersions :many
SELECT DISTINCT ON (name) * FROM harness_versions
WHERE workspace = $1
ORDER BY name, version DESC;

-- name: HarnessVersionByID :one
SELECT * FROM harness_versions
WHERE id = $1;

-- name: LockHarnessName :exec
-- Serializes versioning of one harness until the transaction ends. The key
-- is the workspace and harness name, joined by a character neither has.
SELECT pg_advisory_xact_lock(hashtext($1));
