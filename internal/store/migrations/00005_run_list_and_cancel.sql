-- +goose Up

-- A run a user cancels ends as cancelled, rather than failed.
ALTER TABLE runs DROP CONSTRAINT runs_status_check;
ALTER TABLE runs ADD CONSTRAINT runs_status_check
    CHECK (status IN ('running', 'succeeded', 'failed', 'cancelled'));

-- Runs are listed newest first.
CREATE INDEX runs_created_at ON runs (created_at DESC, id DESC);

-- +goose Down

DROP INDEX runs_created_at;
UPDATE runs SET status = 'failed' WHERE status = 'cancelled';
ALTER TABLE runs DROP CONSTRAINT runs_status_check;
ALTER TABLE runs ADD CONSTRAINT runs_status_check
    CHECK (status IN ('running', 'succeeded', 'failed'));
