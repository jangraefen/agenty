-- +goose Up

-- A user's conversations are listed by the first runs they started.
CREATE INDEX runs_first_by_starter ON runs (started_by) WHERE id = conversation_id;

-- +goose Down

DROP INDEX runs_first_by_starter;
