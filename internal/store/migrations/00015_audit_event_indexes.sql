-- +goose Up

-- What a user did, and what changed in a workspace, are read newest first.
CREATE INDEX audit_events_actor ON audit_events (actor, id);
CREATE INDEX audit_events_workspace_action ON audit_events (workspace, action, id);

-- +goose Down

DROP INDEX audit_events_workspace_action;
DROP INDEX audit_events_actor;
