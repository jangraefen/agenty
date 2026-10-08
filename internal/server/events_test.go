package server_test

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/api"
	"github.com/jangraefen/agenty/internal/auditlog"
	"github.com/jangraefen/agenty/internal/model/modeltest"
	"github.com/jangraefen/agenty/internal/policy"
	"github.com/jangraefen/agenty/internal/store/storetest"
)

// logEvents returns the audit log's events with the given action, in order.
func (f *fixture) logEvents(t *testing.T, action string) []auditlog.Event {
	t.Helper()
	all, err := f.store.AuditEvents(context.Background(), 0, math.MaxInt64, 1000)
	require.NoError(t, err)
	var out []auditlog.Event
	for _, e := range all {
		if e.Action == action {
			out = append(out, e)
		}
	}
	return out
}

func TestAuditEvents_ServerStarted(t *testing.T) {
	central := policy.RulesModule("central", `deny contains "no dotfiles" if startswith(input.args.path, ".")`)
	f := newFixture(t, options{policy: []policy.Module{central}})

	started := f.logEvents(t, "server.started")
	require.Len(t, started, 1, "a server's start is an event")
	assert.Empty(t, started[0].Actor)
	var details struct {
		Policy []policy.Module `json:"policy"`
		Users  []struct {
			Name    string `json:"name"`
			Auditor bool   `json:"auditor"`
		} `json:"users"`
		Workspaces []struct {
			Name    string   `json:"name"`
			Members []string `json:"members"`
		} `json:"workspaces"`
		MCPServers []struct {
			Name       string `json:"name"`
			Command    string `json:"command"`
			ArgsSHA256 string `json:"args_sha256"`
		} `json:"mcp_servers"`
	}
	require.NoError(t, json.Unmarshal(started[0].Details, &details))
	assert.Equal(t, []policy.Module{central}, details.Policy, "the central policy in force, as written")
	require.Len(t, details.Users, 4)
	assert.Equal(t, "dana", details.Users[3].Name)
	assert.True(t, details.Users[3].Auditor)
	assert.False(t, details.Users[0].Auditor)
	assert.Equal(t, "home", details.Workspaces[0].Name)
	assert.Equal(t, []string{"alice", "bob", "dana"}, details.Workspaces[0].Members)
	assert.Equal(t, "files", details.MCPServers[0].Name)
	assert.Equal(t, "unused", details.MCPServers[0].Command)
	assert.Regexp(t, `^[0-9a-f]{64}$`, details.MCPServers[0].ArgsSHA256, "arguments, which may hold a credential, only as a digest")
	assert.NotContains(t, string(started[0].Details), "postgres://", "not the arguments themselves")
	for _, secret := range []string{token, aliceToken, bobToken, carolToken, danaToken} {
		assert.NotContains(t, string(started[0].Details), secret, "no credential is part of the log")
	}

	f.restart(t, options{})
	assert.Len(t, f.logEvents(t, "server.started"), 2, "each start is an event")
}

func TestAuditEvents_CancelAndReads(t *testing.T) {
	f := newFixture(t, options{policy: writesNeedApproval})
	f.putNotes(t)
	run, _ := f.waitForApproval(t)
	require.Equal(t, http.StatusAccepted, f.do(t, http.MethodPost, home+"/runs/"+run.ID+"/cancel", nil, nil))
	f.finish(t, run.ID)

	requested := f.logEvents(t, "run.cancel_requested")
	require.Len(t, requested, 1)
	assert.Equal(t, "alice", requested[0].Actor, "a cancel names who asked for it")
	assert.Equal(t, run.ID, requested[0].RunID)
	finished := f.logEvents(t, "run.finished")
	require.Len(t, finished, 1)
	assert.Greater(t, finished[0].ID, requested[0].ID, "asked for before the run ended")
	assert.Len(t, f.logEvents(t, "harness.changed"), 1)
	assert.Equal(t, "alice", f.logEvents(t, "harness.changed")[0].Actor)

	var list api.AuditRunList
	require.Equal(t, http.StatusOK, f.doAs(t, danaToken, http.MethodGet, "/v1/audit/runs?workspace=home&status=cancelled", nil, &list))
	var detail api.AuditRunDetail
	require.Equal(t, http.StatusOK, f.doAs(t, danaToken, http.MethodGet, "/v1/audit/runs/"+run.ID, nil, &detail))
	f.export(t, "?after=2")
	var e api.Error
	require.Equal(t, http.StatusForbidden, f.doAs(t, aliceToken, http.MethodGet, "/v1/audit/runs", nil, &e))

	reads := f.logEvents(t, "audit.read")
	require.Len(t, reads, 3, "every read of an auditor is an event; a refused one reads nothing")
	for _, r := range reads {
		assert.Equal(t, "dana", r.Actor)
		assert.Empty(t, r.RunID, "a read is not an event of the run it read")
	}
	assert.JSONEq(t, `{"read":"runs","workspace":"home","status":"cancelled"}`, string(reads[0].Details))
	assert.JSONEq(t, `{"read":"run","run":"`+run.ID+`"}`, string(reads[1].Details))
	assert.JSONEq(t, `{"read":"export","after":2}`, string(reads[2].Details))

	whole, _ := f.export(t, "")
	_, err := auditlog.Verify(strings.NewReader(whole))
	require.NoError(t, err)
}

// TestAuditEvents_ReadsFailClosed: an auditor's read that the log cannot
// record does not happen.
func TestAuditEvents_ReadsFailClosed(t *testing.T) {
	f := newFixture(t, options{})
	f.putNotes(t)
	f.script(modeltest.Reply("done"))
	run := f.startRun(t, "tidy my notes")
	f.finish(t, run.ID)
	storetest.Exec(t, f.dbURL, `
		CREATE FUNCTION refuse_appends() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'the log is broken'; END $$;
		CREATE TRIGGER refuse_appends BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION refuse_appends()`)

	for _, path := range []string{"/v1/audit/runs", "/v1/audit/runs/" + run.ID, "/v1/audit/export"} {
		var raw json.RawMessage
		assert.Equal(t, http.StatusInternalServerError, f.doAs(t, danaToken, http.MethodGet, path, nil, &raw), path)
		assert.NotContains(t, string(raw), run.ID, "%s: nothing read", path)
	}
}

func TestAuditEvents_CancelARunningRun(t *testing.T) {
	f := newFixture(t, options{})
	f.putNotes(t)
	run, _ := f.busy(t)

	require.Equal(t, http.StatusAccepted, f.do(t, http.MethodPost, home+"/runs/"+run.ID+"/cancel", nil, nil))
	assert.Equal(t, api.RunStatusCancelled, f.finish(t, run.ID).Status)

	requested := f.logEvents(t, "run.cancel_requested")
	require.Len(t, requested, 1)
	assert.Equal(t, "alice", requested[0].Actor)
	finished := f.logEvents(t, "run.finished")
	require.Len(t, finished, 1)
	assert.Greater(t, finished[0].ID, requested[0].ID)
	assert.Contains(t, string(finished[0].Details), `"status":"cancelled"`)

	require.Equal(t, http.StatusConflict, f.do(t, http.MethodPost, home+"/runs/"+run.ID+"/cancel", nil, nil))
	assert.Len(t, f.logEvents(t, "run.cancel_requested"), 1, "a cancel of a finished run requests nothing")
}
