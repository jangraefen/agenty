-- +goose Up

-- Migration 13 copied every audit record into audit_events.
DROP TABLE audit_records;

-- +goose Down

SELECT 'the audit records live in audit_events now; they are not copied back';
