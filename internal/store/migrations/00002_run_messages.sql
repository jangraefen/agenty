-- +goose Up

-- run_messages is the transcript of each run: its conversation with the
-- model, one row per message, in order. Tool calls and results are json, not
-- jsonb, for the reasons given for audit_records.
CREATE TABLE run_messages (
    run_id       text        NOT NULL REFERENCES runs (id),
    position     integer     NOT NULL CHECK (position >= 0),
    role         text        NOT NULL CHECK (role IN ('user', 'assistant')),
    text         text        NOT NULL DEFAULT '',
    tool_calls   json,
    tool_results json,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, position)
);

-- +goose Down

DROP TABLE run_messages;
