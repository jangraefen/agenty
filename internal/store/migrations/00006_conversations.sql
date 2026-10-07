-- +goose Up

-- A run may follow an earlier run, continuing its conversation with the
-- model. The runs that follow one another form a conversation, named by the
-- ID of its first run. A run is followed by one run at most, so a
-- conversation never branches.
ALTER TABLE runs ADD COLUMN conversation_id text;
UPDATE runs SET conversation_id = id;
ALTER TABLE runs ALTER COLUMN conversation_id SET NOT NULL;
ALTER TABLE runs ADD COLUMN follows text UNIQUE REFERENCES runs (id);

CREATE INDEX runs_conversation_id ON runs (conversation_id, created_at, id);

-- +goose Down

DROP INDEX runs_conversation_id;
ALTER TABLE runs DROP COLUMN follows;
ALTER TABLE runs DROP COLUMN conversation_id;
