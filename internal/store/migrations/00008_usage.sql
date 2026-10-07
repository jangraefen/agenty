-- +goose Up

-- The tokens of the model call that wrote a message, which only replies
-- have, and their sums for each run, kept as the messages are stored.
ALTER TABLE run_messages
    ADD COLUMN input_tokens       bigint NOT NULL DEFAULT 0,
    ADD COLUMN output_tokens      bigint NOT NULL DEFAULT 0,
    ADD COLUMN cache_write_tokens bigint NOT NULL DEFAULT 0,
    ADD COLUMN cache_read_tokens  bigint NOT NULL DEFAULT 0;

ALTER TABLE runs
    ADD COLUMN input_tokens       bigint NOT NULL DEFAULT 0,
    ADD COLUMN output_tokens      bigint NOT NULL DEFAULT 0,
    ADD COLUMN cache_write_tokens bigint NOT NULL DEFAULT 0,
    ADD COLUMN cache_read_tokens  bigint NOT NULL DEFAULT 0;

-- +goose Down

ALTER TABLE runs
    DROP COLUMN input_tokens,
    DROP COLUMN output_tokens,
    DROP COLUMN cache_write_tokens,
    DROP COLUMN cache_read_tokens;

ALTER TABLE run_messages
    DROP COLUMN input_tokens,
    DROP COLUMN output_tokens,
    DROP COLUMN cache_write_tokens,
    DROP COLUMN cache_read_tokens;
