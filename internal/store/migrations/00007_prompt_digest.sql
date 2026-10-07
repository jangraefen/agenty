-- +goose Up

-- What a run sent the model: its model, instructions and tools, and the
-- conversation of earlier runs before its input, as digests. A follow-up
-- compares them with its own to tell whether the earlier runs' replies, as
-- the provider keeps them, can be sent again: some providers bind a reply's
-- reasoning to exactly what came before it. Runs stored before have none,
-- which matches no run.
ALTER TABLE runs
    ADD COLUMN prompt_digest  text NOT NULL DEFAULT '',
    ADD COLUMN history_digest text NOT NULL DEFAULT '';

-- A message stored other than as the model saw or wrote it, because
-- redaction changed it. Messages stored before are not marked, but their
-- runs have no digests, so their replies are never sent again as the
-- provider kept them.
ALTER TABLE run_messages ADD COLUMN altered boolean NOT NULL DEFAULT false;

-- +goose Down

ALTER TABLE run_messages DROP COLUMN altered;
ALTER TABLE runs
    DROP COLUMN prompt_digest,
    DROP COLUMN history_digest;
