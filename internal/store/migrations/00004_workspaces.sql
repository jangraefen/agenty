-- +goose Up

-- A harness belongs to a workspace, and its name is unique there. A run
-- belongs to the workspace of the harness version it runs. Harnesses stored
-- before workspaces existed move to the workspace named default.
ALTER TABLE harness_versions ADD COLUMN workspace text NOT NULL DEFAULT 'default';
ALTER TABLE harness_versions ALTER COLUMN workspace DROP DEFAULT;
ALTER TABLE harness_versions DROP CONSTRAINT harness_versions_name_version_key;
ALTER TABLE harness_versions ADD CONSTRAINT harness_versions_workspace_name_version_key UNIQUE (workspace, name, version);

-- started_by names the user who started a run; it is empty for runs started
-- before the API had sign-in.
ALTER TABLE runs ADD COLUMN started_by text NOT NULL DEFAULT '';
ALTER TABLE runs ALTER COLUMN started_by DROP DEFAULT;

-- +goose Down

ALTER TABLE runs DROP COLUMN started_by;
ALTER TABLE harness_versions DROP CONSTRAINT harness_versions_workspace_name_version_key;
-- Fails if two workspaces hold a harness of the same name and version.
ALTER TABLE harness_versions ADD CONSTRAINT harness_versions_name_version_key UNIQUE (name, version);
ALTER TABLE harness_versions DROP COLUMN workspace;
