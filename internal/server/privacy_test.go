package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/api"
	"github.com/jangraefen/agenty/internal/auditlog"
	"github.com/jangraefen/agenty/internal/model/modeltest"
)

// TestInvariant_RunsArePrivate guards private runs: a run is seen, continued,
// cancelled and answered only by the user who started its conversation.
// Every operation the spec has on a run is tried by another member of its
// workspace and by an auditor who is a member too, on a running run and on
// one that waits for approval.
func TestInvariant_RunsArePrivate(t *testing.T) {
	f := newFixture(t, options{policy: writesNeedApproval})
	f.putNotes(t)
	running, release := f.busy(t)
	f.script(modeltest.CallTools(call("c2", "files_write", `{"path":"notes.md"}`)))
	waiting := f.startRun(t, "write my notes")
	events := f.events(t, waiting.ID)
	events.next()
	req := decodeAs[api.ApprovalRequest](t, events.next())

	c, err := spec()
	require.NoError(t, err)
	var paths []string
	for path := range c.doc.Paths.Map() {
		if strings.HasPrefix(path, "/v1/workspaces/{workspace}/runs/{id}") {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	require.NotEmpty(t, paths)
	for _, run := range []api.Run{running, waiting} {
		for _, path := range paths {
			for method := range c.doc.Paths.Find(path).Operations() {
				url := strings.NewReplacer("{workspace}", "home", "{id}", run.ID, "{approval}", req.ID).Replace(path)
				var body any
				if method == http.MethodPost {
					body = map[string]any{"input": "x", "approved": true}
				}
				for _, bearer := range []string{bobToken, danaToken} {
					var e api.Error
					assert.Equal(t, http.StatusNotFound, f.doAs(t, bearer, method, url, body, &e), "%s %s", method, url)
				}
			}
		}
		for _, bearer := range []string{bobToken, danaToken} {
			var e api.Error
			assert.Equal(t, http.StatusNotFound, f.doAs(t, bearer, http.MethodGet, "/v1/conversations/"+run.ID, nil, &e))
		}
	}
	var theirs []api.ApprovalRequest
	require.Equal(t, http.StatusOK, f.doAs(t, bobToken, http.MethodGet, home+"/approvals", nil, &theirs))
	assert.Empty(t, theirs, "another member's requests are not listed")
	var listed api.ConversationList
	require.Equal(t, http.StatusOK, f.doAs(t, bobToken, http.MethodGet, "/v1/conversations", nil, &listed))
	assert.Empty(t, listed.Conversations)

	assert.Equal(t, api.RunStatusRunning, f.status(t, running.ID), "no one else cancelled the run")
	assert.Equal(t, api.RunStatusWaiting, f.status(t, waiting.ID), "no one else answered the request")
	assert.Zero(t, f.write.Calls)
	release()
	f.finish(t, running.ID)
	// The run resumes, once answered, on a model of its own.
	f.script(modeltest.Reply("written"))
	require.Equal(t, http.StatusNoContent, f.do(t, http.MethodPost, home+"/runs/"+waiting.ID+"/approvals/"+req.ID, api.Answer{Approved: true}, nil))
	rest := events.rest()
	assert.Equal(t, api.RunStatusSucceeded, decodeAs[api.Run](t, rest[len(rest)-1]).Status, "the run's starter answers it")
}

// TestInvariant_OnlyAuditorsReadTheAuditLog guards the audit log: only
// auditors read it, across every workspace, and they see what happened in a
// run, not what was said in it. Being an auditor gives nothing in a
// workspace.
func TestInvariant_OnlyAuditorsReadTheAuditLog(t *testing.T) {
	f := newFixture(t, options{})
	f.putNotes(t)
	f.script(modeltest.CallTools(call("c1", "files_read", `{"path":"notes.md"}`)), modeltest.Reply("a private reply"))
	mine := f.startRun(t, "a private prompt")
	f.finish(t, mine.ID)
	require.Equal(t, http.StatusOK, f.doAs(t, bobToken, http.MethodPut, "/v1/workspaces/work/harnesses/notes", notes(), nil))
	f.script(modeltest.Reply("done"))
	var theirs api.Run
	require.Equal(t, http.StatusCreated, f.doAs(t, bobToken, http.MethodPost, "/v1/workspaces/work/runs", api.CreateRun{Harness: "notes", Input: "x"}, &theirs))

	for _, bearer := range []string{aliceToken, bobToken, carolToken} {
		for _, path := range []string{"/v1/audit/runs", "/v1/audit/runs/" + mine.ID} {
			var e api.Error
			assert.Equal(t, http.StatusForbidden, f.doAs(t, bearer, http.MethodGet, path, nil, &e), path)
		}
	}

	var raw json.RawMessage
	require.Equal(t, http.StatusOK, f.doAs(t, danaToken, http.MethodGet, "/v1/audit/runs", nil, &raw))
	var list api.AuditRunList
	require.NoError(t, json.Unmarshal(raw, &list))
	require.Len(t, list.Runs, 2, "the runs of every workspace")
	assert.Equal(t, []string{theirs.ID, mine.ID}, []string{list.Runs[0].ID, list.Runs[1].ID})
	assert.Equal(t, "work", list.Runs[0].Workspace)
	assert.Equal(t, "alice", list.Runs[1].StartedBy)
	var rawDetail json.RawMessage
	require.Equal(t, http.StatusOK, f.doAs(t, danaToken, http.MethodGet, "/v1/audit/runs/"+mine.ID, nil, &rawDetail))
	var detail api.AuditRunDetail
	require.NoError(t, json.Unmarshal(rawDetail, &detail))
	assert.Equal(t, list.Runs[1], detail.Run)
	require.NotEmpty(t, detail.Records)
	assert.Equal(t, "files_read", detail.Records[0].Tool, "the run's calls are evidence")
	for _, body := range []json.RawMessage{raw, rawDetail} {
		assert.NotContains(t, string(body), "a private prompt", "an auditor does not see what was said")
		assert.NotContains(t, string(body), "a private reply")
	}

	var filtered api.AuditRunList
	require.Equal(t, http.StatusOK, f.doAs(t, danaToken, http.MethodGet, "/v1/audit/runs?workspace=home&started_by=alice&harness=notes&status=succeeded", nil, &filtered))
	require.Len(t, filtered.Runs, 1)
	assert.Equal(t, mine.ID, filtered.Runs[0].ID)
	var e api.Error
	assert.Equal(t, http.StatusNotFound, f.doAs(t, danaToken, http.MethodGet, "/v1/audit/runs/ghost", nil, &e))
	assert.Equal(t, http.StatusNotFound, f.doAs(t, danaToken, http.MethodGet, "/v1/workspaces/work/harnesses", nil, &e), "an auditor is no member")
	assert.Equal(t, http.StatusNotFound, f.doAs(t, danaToken, http.MethodGet, home+"/runs/"+mine.ID, nil, &e), "nor does an auditor see a run as a member")
}

func TestExportAuditLog(t *testing.T) {
	f := newFixture(t, options{})
	f.putNotes(t)
	f.script(modeltest.CallTools(call("c1", "files_read", `{"path":"notes.md"}`)), modeltest.Reply("read"))
	run := f.startRun(t, "read my notes")
	f.finish(t, run.ID)

	for _, bearer := range []string{aliceToken, carolToken} {
		var e api.Error
		assert.Equal(t, http.StatusForbidden, f.doAs(t, bearer, http.MethodGet, "/v1/audit/export", nil, &e))
	}
	whole, trailer := f.export(t, "")
	assert.Equal(t, "true", trailer, "a complete export says so")
	sum, err := auditlog.Verify(strings.NewReader(whole))
	require.NoError(t, err)
	assert.Equal(t, int64(1), sum.First)
	assert.Contains(t, whole, `"action":"tool.decision"`, "the run's calls are in it")
	assert.Contains(t, whole, `"action":"tool.result"`)
	assert.Contains(t, whole, `"read":"export"`, "and the export itself, an auditor's read")

	rest, trailer := f.export(t, "?after=1")
	assert.Equal(t, "true", trailer)
	_, err = auditlog.Verify(strings.NewReader(rest), auditlog.Anchor{ID: 1, Hash: mustFirstHash(t, whole)})
	require.NoError(t, err, "a later part verifies against the hash before it")

	var e api.Error
	assert.Equal(t, http.StatusBadRequest, f.doAs(t, danaToken, http.MethodGet, "/v1/audit/export?after=-1", nil, &e))
}

// export reads the audit log as dana, the auditor, with the given query, and
// returns it with its completion trailer.
func (f *fixture) export(t *testing.T, query string) (string, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, f.http.URL+"/v1/audit/export"+query, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+danaToken)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { assert.NoError(t, resp.Body.Close()) }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body := readChecked(t, req, resp)
	b, err := io.ReadAll(body)
	require.NoError(t, err)
	return string(b), resp.Trailer.Get("Audit-Export-Complete")
}

// mustFirstHash returns the hash of an export's first event.
func mustFirstHash(t *testing.T, export string) auditlog.Hash {
	t.Helper()
	var first auditlog.Event
	line, _, _ := strings.Cut(export, "\n")
	require.NoError(t, json.Unmarshal([]byte(line), &first))
	return first.Hash
}
