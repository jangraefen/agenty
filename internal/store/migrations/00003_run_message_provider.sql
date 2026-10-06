-- +goose Up

-- A message in the form of the provider that generated it, such as an
-- Anthropic reply with its thinking blocks, so it can be replayed exactly.
ALTER TABLE run_messages
    ADD COLUMN provider      text NOT NULL DEFAULT '',
    ADD COLUMN provider_data json;

-- +goose Down

ALTER TABLE run_messages
    DROP COLUMN provider,
    DROP COLUMN provider_data;
