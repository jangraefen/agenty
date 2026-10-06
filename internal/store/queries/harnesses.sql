-- name: LatestHarnessVersion :one
SELECT * FROM harness_versions
WHERE name = $1
ORDER BY version DESC
LIMIT 1;

-- name: InsertHarnessVersion :one
INSERT INTO harness_versions (name, version, definition)
VALUES ($1, $2, $3)
RETURNING *;

-- name: LatestHarnessVersions :many
SELECT DISTINCT ON (name) * FROM harness_versions
ORDER BY name, version DESC;

-- name: HarnessVersionByID :one
SELECT * FROM harness_versions
WHERE id = $1;
