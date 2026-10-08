-- +goose Up

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

CREATE INDEX audit_events_run_id ON audit_events (run_id, id) WHERE run_id IS NOT NULL;

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

DROP TABLE audit_events;
DROP FUNCTION audit_events_refuse();
