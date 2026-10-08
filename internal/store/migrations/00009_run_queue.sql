-- +goose Up

-- The runs table is the queue of runs: a run is stored as queued, and a
-- worker claims it, oldest first, as running.
ALTER TABLE runs DROP CONSTRAINT runs_status_check;
ALTER TABLE runs ADD CONSTRAINT runs_status_check
    CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled'));
ALTER TABLE runs ALTER COLUMN status SET DEFAULT 'queued';

CREATE INDEX runs_queued ON runs (created_at, id) WHERE status = 'queued';

-- +goose Down

DROP INDEX runs_queued;
UPDATE runs SET status = 'cancelled', error = 'cancelled by a downgrade', finished_at = now()
WHERE status = 'queued';
ALTER TABLE runs ALTER COLUMN status SET DEFAULT 'running';
ALTER TABLE runs DROP CONSTRAINT runs_status_check;
ALTER TABLE runs ADD CONSTRAINT runs_status_check
    CHECK (status IN ('running', 'succeeded', 'failed', 'cancelled'));
