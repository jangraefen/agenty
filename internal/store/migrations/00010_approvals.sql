-- +goose Up

-- A run whose call waits for approval is waiting: no worker holds it until
-- the call is answered, which queues it again.
ALTER TABLE runs DROP CONSTRAINT runs_status_check;
ALTER TABLE runs ADD CONSTRAINT runs_status_check
    CHECK (status IN ('queued', 'running', 'waiting', 'succeeded', 'failed', 'cancelled'));

-- approvals holds every approval request: the call that waits, as the
-- approver sees it, redacted, and what the run needs to resume at it: the
-- call's ID in the audit log, its index in the model's reply, and the
-- results of the calls before it in that reply. A request is answered once;
-- one nobody answers before it expires is rejected, and one whose run is
-- cancelled is withdrawn.
CREATE TABLE approvals (
    id          text        PRIMARY KEY,
    run_id      text        NOT NULL REFERENCES runs (id),
    call_id     text        NOT NULL,
    call_index  integer     NOT NULL CHECK (call_index >= 0),
    tool        text        NOT NULL,
    args        json,
    reasons     jsonb       NOT NULL,
    results     jsonb       NOT NULL,
    status      text        NOT NULL DEFAULT 'pending'
                            CHECK (status IN ('pending', 'approved', 'rejected', 'withdrawn')),
    approver    text        NOT NULL DEFAULT '',
    reason      text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    answered_at timestamptz
);

-- A run waits for one call at a time.
CREATE UNIQUE INDEX approvals_pending ON approvals (run_id) WHERE status = 'pending';
CREATE INDEX approvals_expiring ON approvals (expires_at) WHERE status = 'pending';
CREATE INDEX approvals_of_run ON approvals (run_id, created_at);

-- +goose Down

-- A run that waited, or is queued to resume after an answer, cannot resume
-- without its request.
UPDATE runs SET status = 'failed', error = 'waiting for approval at a downgrade', finished_at = now()
WHERE status = 'waiting' OR (status = 'queued' AND id IN (SELECT run_id FROM approvals));
DROP TABLE approvals;
ALTER TABLE runs DROP CONSTRAINT runs_status_check;
ALTER TABLE runs ADD CONSTRAINT runs_status_check
    CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled'));
