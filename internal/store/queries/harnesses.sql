-- name: LatestHarnessVersion :one
-- The latest version of a harness in a workspace: the one new runs start
-- with, and the one a new version is compared with and numbered after.
SELECT * FROM harness_versions
WHERE workspace = $1 AND name = $2
ORDER BY version DESC
LIMIT 1;

-- name: InsertHarnessVersion :one
-- Stores a new version, numbered by the caller under LockHarnessName, and
-- returns it with its id and time.
INSERT INTO harness_versions (workspace, name, version, definition)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: LatestHarnessVersions :many
-- The latest version of every harness of a workspace, by name: DISTINCT ON
-- keeps the first row per name, which the ordering makes the highest
-- version.
SELECT DISTINCT ON (name) * FROM harness_versions
WHERE workspace = $1
ORDER BY name, version DESC;

-- name: HarnessVersionByID :one
-- A version by its id, as a run names it: a follow-up runs its
-- conversation's version, not the latest.
SELECT * FROM harness_versions
WHERE id = $1;

-- name: LockHarnessName :exec
-- Serializes versioning of one harness until the transaction ends. The key
-- is the workspace and harness name, joined by a character neither has.
-- An advisory lock, as the first version has no row to lock; two harnesses
-- whose keys hash alike only wait for each other, which is harmless.
SELECT pg_advisory_xact_lock(hashtext($1));
