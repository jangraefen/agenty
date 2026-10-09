package server_test

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestAuditEvents_CancelAndNoReads(t *testing.T) {
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

	all, err := f.store.AuditEvents(context.Background(), 0, math.MaxInt64, 1000)
	require.NoError(t, err)
	require.NotEmpty(t, all)
	for _, e := range all {
		assert.NotEqual(t, "dana", e.Actor, "an auditor's reads are not part of any log")
	}

	whole, _ := f.export(t, "")
	_, err = auditlog.Verify(strings.NewReader(whole))
	require.NoError(t, err)
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

// eventPage is a page of the audit log as the API lists it.
type eventPage struct {
	Events []auditlog.Event `json:"events"`
	Next   int64            `json:"next"`
}

// actions lists a page's events as "actor action", so a test compares who
// did what in one assertion.
func actions(page eventPage) []string {
	out := make([]string, len(page.Events))
	for i, e := range page.Events {
		out[i] = e.Actor + " " + e.Action
	}
	return out
}

func TestAuditViews(t *testing.T) {
	f := newFixture(t, options{})
	f.putNotes(t)
	f.script(modeltest.Reply("done"))
	run := f.startRun(t, "tidy my notes")
	f.finish(t, run.ID)
	var detail api.AuditRunDetail
	require.Equal(t, http.StatusOK, f.doAs(t, danaToken, http.MethodGet, "/v1/audit/runs/"+run.ID, nil, &detail))

	t.Run("a user's own actions", func(t *testing.T) {
		var mine eventPage
		require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, "/v1/me/activity", nil, &mine))
		assert.Equal(t, []string{"alice run.started", "alice harness.changed"}, actions(mine),
			"what alice did, newest first; not the server's events")
		var theirs eventPage
		require.Equal(t, http.StatusOK, f.doAs(t, danaToken, http.MethodGet, "/v1/me/activity", nil, &theirs))
		assert.Empty(t, theirs.Events, "an auditor's reads are not recorded")
	})
	t.Run("a workspace's changes", func(t *testing.T) {
		var changes eventPage
		require.Equal(t, http.StatusOK, f.doAs(t, bobToken, http.MethodGet, home+"/audit", nil, &changes))
		assert.Equal(t, []string{"alice harness.changed"}, actions(changes), "a member sees the workspace's changes, not its members' runs")
		var e api.Error
		assert.Equal(t, http.StatusNotFound, f.doAs(t, carolToken, http.MethodGet, home+"/audit", nil, &e), "only members")
	})
	t.Run("everything, for auditors", func(t *testing.T) {
		var e api.Error
		assert.Equal(t, http.StatusForbidden, f.do(t, http.MethodGet, "/v1/audit/events", nil, &e))
		var all eventPage
		require.Equal(t, http.StatusOK, f.doAs(t, danaToken, http.MethodGet, "/v1/audit/events?limit=3", nil, &all))
		require.Len(t, all.Events, 3)
		assert.Equal(t, []string{" run.finished", "alice run.started", "alice harness.changed"}, actions(all))
		assert.Equal(t, all.Events[2].ID, all.Next)
		var older eventPage
		require.Equal(t, http.StatusOK, f.doAs(t, danaToken, http.MethodGet, fmt.Sprintf("/v1/audit/events?before=%d&action=server.started", all.Next), nil, &older))
		assert.Equal(t, []string{" server.started"}, actions(older))
		var ofRun eventPage
		require.Equal(t, http.StatusOK, f.doAs(t, danaToken, http.MethodGet, "/v1/audit/events?run="+run.ID+"&actor=alice", nil, &ofRun))
		assert.Equal(t, []string{"alice run.started"}, actions(ofRun))
		assert.Equal(t, http.StatusBadRequest, f.doAs(t, danaToken, http.MethodGet, "/v1/audit/events?before=-1", nil, &e))
	})
}
