-- +goose Up

-- Migration 13 copied every audit record into audit_events.
DROP TABLE audit_records;

-- +goose Down

-- The audit log is evidence: going back past this would drop it.
-- +goose StatementBegin
DO $$
BEGIN
    RAISE EXCEPTION 'irreversible: the audit records live in audit_events now';
END
$$;
-- +goose StatementEnd
