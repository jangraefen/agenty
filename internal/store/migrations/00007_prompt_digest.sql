-- +goose Up

-- What a run sent the model before the conversation, its model,
-- instructions and tools, as a digest. A follow-up compares it with its own
-- to tell whether the earlier runs' replies, as the provider keeps them, can
-- be sent again: some providers bind a reply's reasoning to exactly that.
-- Runs stored before it have none, which matches no run.
ALTER TABLE runs ADD COLUMN prompt_digest text NOT NULL DEFAULT '';

-- A message stored other than as the model saw or wrote it, because
-- redaction changed it. Earlier messages are taken as unchanged.
ALTER TABLE run_messages ADD COLUMN altered boolean NOT NULL DEFAULT false;

-- +goose Down

ALTER TABLE run_messages DROP COLUMN altered;
ALTER TABLE runs DROP COLUMN prompt_digest;
