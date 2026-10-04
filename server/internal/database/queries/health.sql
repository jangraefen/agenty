-- name: Ping :one
-- Ping proves that a pooled connection can run a query.
SELECT 1::integer AS ok;
