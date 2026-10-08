-- +goose Up

-- harness_versions holds every harness ever applied, in the workspace it
-- belongs to, where its name is unique. A version is never changed;
-- applying a changed harness adds the next version.
CREATE TABLE harness_versions (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    workspace  text        NOT NULL,
    name       text        NOT NULL,
    version    integer     NOT NULL CHECK (version > 0),
    definition jsonb       NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace, name, version)
);

-- runs holds one row per run, with the harness version it runs, whose
-- workspace it belongs to, and the user who started it. The table is the
-- queue of runs: a run is stored as queued, and a worker claims it, oldest
-- first, as running. A run whose call waits for approval is waiting: no
-- worker holds it until the call is answered, which queues it again.
--
-- A run may follow an earlier run, continuing its conversation with the
-- model. The runs that follow one another form a conversation, named by the
-- ID of its first run. A run is followed by one run at most, so a
-- conversation never branches.
--
-- prompt_digest and history_digest are what a run sent the model: its
-- model, instructions and tools, and the conversation of earlier runs before
-- its input. A follow-up compares them with its own to tell whether the
-- earlier runs' replies, as the provider keeps them, can be sent again: some
-- providers bind a reply's reasoning to exactly what came before it. The
-- token columns sum those of the run's messages.
CREATE TABLE runs (
    id                 text        PRIMARY KEY,
    harness_version_id bigint      NOT NULL REFERENCES harness_versions (id),
    input              text        NOT NULL,
    started_by         text        NOT NULL,
    conversation_id    text        NOT NULL,
    follows            text        UNIQUE REFERENCES runs (id),
    status             text        NOT NULL DEFAULT 'queued'
                                   CHECK (status IN ('queued', 'running', 'waiting', 'succeeded', 'failed', 'cancelled')),
    output             text        NOT NULL DEFAULT '',
    steps              integer     NOT NULL DEFAULT 0,
    error              text        NOT NULL DEFAULT '',
    prompt_digest      text        NOT NULL DEFAULT '',
    history_digest     text        NOT NULL DEFAULT '',
    input_tokens       bigint      NOT NULL DEFAULT 0,
    output_tokens      bigint      NOT NULL DEFAULT 0,
    cache_write_tokens bigint      NOT NULL DEFAULT 0,
    cache_read_tokens  bigint      NOT NULL DEFAULT 0,
    created_at         timestamptz NOT NULL DEFAULT now(),
    finished_at        timestamptz
);

-- Runs are listed newest first, a conversation's oldest first, and a user's
-- conversations by the first runs they started.
CREATE INDEX runs_created_at ON runs (created_at DESC, id DESC);
CREATE INDEX runs_conversation_id ON runs (conversation_id, created_at, id);
CREATE INDEX runs_queued ON runs (created_at, id) WHERE status = 'queued';
CREATE INDEX runs_first_by_starter ON runs (started_by) WHERE id = conversation_id;

-- run_messages is the transcript of each run: its conversation with the
-- model, one row per message, in order. Tool calls and results are json, not
-- jsonb: json keeps the text as written (key order, duplicate keys) and
-- accepts the \u0000 escape, which jsonb rejects. provider and provider_data
-- hold a reply in the form of the provider that generated it, such as an
-- Anthropic reply with its thinking blocks, so it can be replayed exactly.
-- altered marks a message stored other than as the model saw or wrote it,
-- because redaction changed it. The tokens are those of the model call that
-- wrote the message, which only replies have.
CREATE TABLE run_messages (
    run_id             text        NOT NULL REFERENCES runs (id),
    position           integer     NOT NULL CHECK (position >= 0),
    role               text        NOT NULL CHECK (role IN ('user', 'assistant')),
    text               text        NOT NULL DEFAULT '',
    tool_calls         json,
    tool_results       json,
    provider           text        NOT NULL DEFAULT '',
    provider_data      json,
    altered            boolean     NOT NULL DEFAULT false,
    input_tokens       bigint      NOT NULL DEFAULT 0,
    output_tokens      bigint      NOT NULL DEFAULT 0,
    cache_write_tokens bigint      NOT NULL DEFAULT 0,
    cache_read_tokens  bigint      NOT NULL DEFAULT 0,
    created_at         timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, position)
);

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

-- audit_events is the audit log: every event in the order it was recorded,
-- each linked to the one before it by a hash, as package auditlog computes
-- it. ids run from 1 without gaps. details is json, not jsonb, so it keeps
-- its canonical text, which the hash covers. Nothing that refers to a run
-- cascades, so no run's deletion takes its events along.
CREATE TABLE audit_events (
    id          bigint      PRIMARY KEY,
    recorded_at timestamptz NOT NULL,
    actor       text        NOT NULL,
    action      text        NOT NULL,
    workspace   text        NOT NULL,
    run_id      text        REFERENCES runs (id),
    target      text        NOT NULL,
    details     json        NOT NULL,
    prev_hash   bytea       NOT NULL,
    hash        bytea       NOT NULL
);

-- A run's events, what a user did, and what changed in a workspace are read
-- in order.
CREATE INDEX audit_events_run_id ON audit_events (run_id, id) WHERE run_id IS NOT NULL;
CREATE INDEX audit_events_actor ON audit_events (actor, id);
CREATE INDEX audit_events_workspace_action ON audit_events (workspace, action, id);

-- The log is append-only: no event is changed or removed, one by one or all
-- at once. An owner of the table can still drop these triggers; the hash
-- chain, held against an anchor kept elsewhere, shows what was done then.
-- +goose StatementBegin
CREATE FUNCTION audit_events_refuse() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'the audit log is append-only: % refused', TG_OP;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER audit_events_append_only BEFORE UPDATE OR DELETE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION audit_events_refuse();
CREATE TRIGGER audit_events_no_truncate BEFORE TRUNCATE ON audit_events
    FOR EACH STATEMENT EXECUTE FUNCTION audit_events_refuse();

-- +goose Down

-- The audit log is evidence: going back past this would drop it.
-- +goose StatementBegin
DO $$
BEGIN
    RAISE EXCEPTION 'irreversible: dropping the schema would drop the audit log';
END
$$;
-- +goose StatementEnd
