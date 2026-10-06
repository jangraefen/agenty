-- +goose Up

-- harness_versions holds every harness ever applied. A version is never
-- changed; applying a changed harness adds the next version.
CREATE TABLE harness_versions (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       text        NOT NULL,
    version    integer     NOT NULL CHECK (version > 0),
    definition jsonb       NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (name, version)
);

-- runs holds one row per run, with the harness version it ran.
CREATE TABLE runs (
    id                 text        PRIMARY KEY,
    harness_version_id bigint      NOT NULL REFERENCES harness_versions (id),
    input              text        NOT NULL,
    status             text        NOT NULL DEFAULT 'running'
                                   CHECK (status IN ('running', 'succeeded', 'failed')),
    output             text        NOT NULL DEFAULT '',
    steps              integer     NOT NULL DEFAULT 0,
    error              text        NOT NULL DEFAULT '',
    created_at         timestamptz NOT NULL DEFAULT now(),
    finished_at        timestamptz
);

-- audit_records is the audit log: one row per decision, approval and result
-- the tool gateway records, with the fields of toolgateway.Record.
CREATE TABLE audit_records (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    run_id      text        NOT NULL REFERENCES runs (id),
    call_id     text        NOT NULL,
    event       text        NOT NULL,
    tool        text        NOT NULL,
    args        jsonb,
    decision    text        NOT NULL,
    reason      text        NOT NULL DEFAULT '',
    approver    text        NOT NULL DEFAULT '',
    result      jsonb,
    error       text        NOT NULL DEFAULT '',
    recorded_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_records_run_id ON audit_records (run_id, id);

-- +goose Down

DROP TABLE audit_records;
DROP TABLE runs;
DROP TABLE harness_versions;
