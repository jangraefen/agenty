-- +goose Up

-- The whole schema of Agenty's state. Until the first release it is this one
-- migration, rewritten rather than migrated (see docs/IDEA.md); goose applies
-- it when the store opens.
--
-- Tables and how they refer to each other:
--
--   harness_versions  1 ─< runs           (runs.harness_version_id)
--   runs              1 ─o runs           (runs.follows, at most one follower)
--   runs              1 ─< run_messages   (run_messages.run_id)
--   runs              1 ─< approvals      (approvals.run_id)
--   runs              1 ─< audit_events   (audit_events.run_id, nullable)
--
-- There is no table of workspaces, users or conversations: workspaces and
-- users come from the operator config, a run's workspace is that of its
-- harness version, and a conversation is the chain of runs that follow one
-- another. Names of users and workspaces are therefore stored as text, not
-- as foreign keys.

-- harness_versions holds every harness ever applied, in the workspace it
-- belongs to, where its name is unique. A version is never changed;
-- applying a changed harness adds the next version.
--
-- id identifies a version across all harnesses and is what runs refer to, so
-- a run keeps pointing at the exact definition it ran. definition is the
-- harness as canonical JSON; jsonb suffices, as no hash covers its bytes.
CREATE TABLE harness_versions (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    workspace  text        NOT NULL,
    name       text        NOT NULL,
    version    integer     NOT NULL CHECK (version > 0),
    definition jsonb       NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    -- Also serves the lookup of a harness's latest version.
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
--
-- id is chosen by the server, not generated here. started_by is the user who
-- started the run; the conversation's owner is the starter of its first
-- run, and a run may follow only a run of its own starter, so every run of a
-- conversation has that one starter. conversation_id is the first run's id,
-- stored on every run so a conversation is read with one index lookup
-- rather than by walking follows. follows is UNIQUE, which is what keeps a
-- conversation from branching, even under concurrent follow-ups.
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
-- runs_queued is the queue's index: a claim takes the oldest queued run
-- from it, and as it holds only queued rows, it stays small however many
-- runs have finished.
CREATE INDEX runs_queued ON runs (created_at, id) WHERE status = 'queued';
-- runs_first_by_starter holds only first runs, which is what a user's list
-- of conversations starts from.
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
--
-- The primary key (run_id, position) means a position is written once: a
-- message is never overwritten, and the transcript reads back in order.
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
--
-- args is json, as the transcript's tool calls are, to keep the arguments
-- exactly as the call had them. reasons and results are written by the
-- store itself, always as lists, so jsonb suffices. approver is empty until
-- someone answers, and stays empty for an expired or withdrawn request.
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
-- The next pending request to expire, which the server sleeps until, and
-- those that have expired.
CREATE INDEX approvals_expiring ON approvals (expires_at) WHERE status = 'pending';
-- A run's latest request.
CREATE INDEX approvals_of_run ON approvals (run_id, created_at);

-- audit_events is the audit log: every event in the order it was recorded,
-- each linked to the one before it by a hash, as package auditlog computes
-- it. ids run from 1 without gaps. details is json, not jsonb, so it keeps
-- its canonical text, which the hash covers. Nothing that refers to a run
-- cascades, so no run's deletion takes its events along.
--
-- id is not an identity column: the store assigns it, holding a lock, as the
-- latest id plus one, because a sequence would leave gaps when a
-- transaction rolls back, and gaps would look like removed events. actor is
-- empty for the server itself, and workspace for an event of the whole
-- organisation. run_id names the run an event is about; target names what
-- else it acted on, such as a harness. prev_hash is the hash of the event
-- before (all zeros for the first), and hash is SHA-256 over this event's
-- fields and prev_hash, so changing any event breaks every link after it.
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

-- The log is append-only (trust-model guarantee 9): no event is changed or
-- removed, one by one or all at once. Row triggers catch UPDATE and DELETE;
-- TRUNCATE fires no row triggers, so a statement trigger catches it. An owner of the table can still drop these triggers; the hash
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
