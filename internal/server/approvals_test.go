package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/api"
	"github.com/jangraefen/agenty/internal/config"
	"github.com/jangraefen/agenty/internal/harness"
	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/model/modeltest"
	"github.com/jangraefen/agenty/internal/policy"
	"github.com/jangraefen/agenty/internal/store"
)

// writesNeedApproval is central policy that asks before every write.
var writesNeedApproval = []policy.Module{policy.RulesModule("central", `require_approval contains "writes need a human" if input.tool == "files_write"`)}

// waitForApproval starts a run whose model calls files_write, which waits for
// approval, and returns the run and its request.
func (f *fixture) waitForApproval(t *testing.T, steps ...modeltest.Step) (api.Run, api.ApprovalRequest) {
	t.Helper()
	f.script(append(steps, modeltest.CallTools(call("c1", "files_write", `{"path":"notes.md"}`)))...)
	run := f.startRun(t, "write my notes")
	events := f.events(t, run.ID)
	for {
		e := events.next()
		if e.name == api.EventApproval {
			return run, decodeAs[api.ApprovalRequest](t, e)
		}
	}
}

// answer answers req as alice.
func (f *fixture) answer(t *testing.T, req api.ApprovalRequest, a api.Answer) {
	t.Helper()
	require.Equal(t, http.StatusNoContent, f.do(t, http.MethodPost, home+"/runs/"+req.RunID+"/approvals/"+req.ID, a, nil))
}

func TestApprovals_AWaitingRunHoldsNoWorker(t *testing.T) {
	f := newFixture(t, options{workers: 1, policy: writesNeedApproval})
	f.putNotes(t)
	waiting, req := f.waitForApproval(t)
	f.script(modeltest.Reply("other"))

	other := f.startRun(t, "something else")

	assert.Equal(t, api.RunStatusSucceeded, f.finish(t, other.ID).Status, "the only worker is free while the run waits")
	assert.Equal(t, api.RunStatusWaiting, f.status(t, waiting.ID))
	f.script(modeltest.Reply("written"))
	f.answer(t, req, api.Answer{Approved: true})
	assert.Equal(t, "written", f.finish(t, waiting.ID).Output)
	assert.Equal(t, 1, f.write.Calls)
}

func TestApprovals_OutliveARestart(t *testing.T) {
	f := newFixture(t, options{policy: writesNeedApproval})
	f.putNotes(t)
	run, req := f.waitForApproval(t)

	f.restart(t, options{policy: writesNeedApproval})

	events := f.events(t, run.ID)
	decision := decodeAs[api.AuditRecord](t, events.next())
	assert.Equal(t, api.DecisionRequireApproval, decision.Decision, "the next server's stream replays what the run recorded")
	replayed := decodeAs[api.ApprovalRequest](t, events.next())
	assert.Equal(t, req, replayed, "and the request it waits for")
	var pending []api.ApprovalRequest
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/approvals", nil, &pending))
	assert.Equal(t, []api.ApprovalRequest{req}, pending)
	m := f.script(modeltest.Reply("written"))
	f.answer(t, req, api.Answer{Approved: true})
	rest := events.rest()
	finished := decodeAs[api.Run](t, rest[len(rest)-1])
	assert.Equal(t, api.RunStatusSucceeded, finished.Status)
	assert.Equal(t, "written", finished.Output)
	assert.Equal(t, 2, finished.Steps, "the step before the restart counts")
	assert.Equal(t, 1, f.write.Calls)
	require.Len(t, m.Requests(), 1)
	assert.Len(t, m.Requests()[0].Messages, 3, "the model sees the run so far and the call's result")
}

// TestInvariant_AnApprovalIsUsedOnce guards a trust-model guarantee: an
// answer resumes the one call it was given for, once.
func TestInvariant_AnApprovalIsUsedOnce(t *testing.T) {
	f := newFixture(t, options{policy: writesNeedApproval})
	f.putNotes(t)
	run, req := f.waitForApproval(t)
	m := f.script(modeltest.CallTools(call("c2", "files_write", `{"path":"other.md"}`)))
	f.answer(t, req, api.Answer{Approved: true})
	events := f.events(t, run.ID)
	var second api.ApprovalRequest
	for second.ID == "" {
		if e := events.next(); e.name == api.EventApproval {
			if r := decodeAs[api.ApprovalRequest](t, e); r.ID != req.ID {
				second = r
			}
		}
	}

	var resp api.Error
	assert.Equal(t, http.StatusNotFound, f.do(t, http.MethodPost, home+"/runs/"+run.ID+"/approvals/"+req.ID, api.Answer{Approved: true}, &resp), "an answer is given once")

	assert.Equal(t, 1, f.write.Calls, "the second write waits for an answer of its own")
	assert.JSONEq(t, `{"path":"other.md"}`, string(second.Args))
	assert.Equal(t, api.RunStatusWaiting, f.status(t, run.ID))
	require.Len(t, m.Requests(), 1)
}

// TestInvariant_ResumedCallsMeetTheCentralPolicyOfTheTime guards a
// trust-model guarantee: an approved call is decided again as it resumes, so
// a rule the operator added while it waited denies it.
func TestInvariant_ResumedCallsMeetTheCentralPolicyOfTheTime(t *testing.T) {
	f := newFixture(t, options{policy: writesNeedApproval})
	f.putNotes(t)
	run, req := f.waitForApproval(t)
	frozen := append([]policy.Module{policy.RulesModule("frozen", `deny contains "writes are frozen" if input.tool == "files_write"`)}, writesNeedApproval...)
	f.restart(t, options{policy: frozen})
	f.script(modeltest.Reply("could not"))

	f.answer(t, req, api.Answer{Approved: true})

	assert.Equal(t, api.RunStatusSucceeded, f.finish(t, run.ID).Status)
	assert.Zero(t, f.write.Calls, "the approval does not override the operator")
	audit := f.audit(t, run.ID)
	last := audit[len(audit)-1]
	assert.Equal(t, req.Tool, last.Tool)
	assert.Equal(t, api.AuditEventDecision, last.Event)
	assert.Equal(t, api.DecisionDeny, last.Decision)
	assert.Contains(t, last.Reason, "writes are frozen")
}

// TestInvariant_LimitsSurviveARestart guards a trust-model guarantee: a run
// resumed by the next server keeps counting the calls it made before, so its
// limits are not reset.
func TestInvariant_LimitsSurviveARestart(t *testing.T) {
	f := newFixture(t, options{policy: writesNeedApproval})
	limited := notes()
	limited.Limits.MaxToolCalls = 2
	require.Equal(t, http.StatusOK, f.do(t, http.MethodPut, home+"/harnesses/notes", limited, nil))
	f.script(modeltest.CallTools(call("c0", "files_read", `{"path":"notes.md"}`), call("c1", "files_write", `{"path":"notes.md"}`)))
	run := f.startRun(t, "write my notes")
	var req api.ApprovalRequest
	for events := f.events(t, run.ID); req.ID == ""; {
		if e := events.next(); e.name == api.EventApproval {
			req = decodeAs[api.ApprovalRequest](t, e)
		}
	}
	f.restart(t, options{policy: writesNeedApproval})
	f.script(modeltest.CallTools(call("c2", "files_read", `{"path":"notes.md"}`)), modeltest.Reply("done"))

	f.answer(t, req, api.Answer{Approved: true})

	assert.Equal(t, api.RunStatusSucceeded, f.finish(t, run.ID).Status)
	assert.Equal(t, 1, f.write.Calls)
	assert.Equal(t, 1, f.read.Calls, "the third call is over the limit of two")
	audit := f.audit(t, run.ID)
	last := audit[len(audit)-1]
	assert.Equal(t, "files_read", last.Tool)
	assert.Equal(t, api.DecisionDeny, last.Decision)
	assert.Contains(t, last.Reason, "tool call limit reached")
}

func TestApprovals_CancelAWaitingRun(t *testing.T) {
	f := newFixture(t, options{policy: writesNeedApproval})
	f.putNotes(t)
	run, req := f.waitForApproval(t)
	events := f.events(t, run.ID)

	require.Equal(t, http.StatusAccepted, f.do(t, http.MethodPost, home+"/runs/"+run.ID+"/cancel", nil, nil))

	streamed := events.rest()
	require.GreaterOrEqual(t, len(streamed), 2)
	failure := decodeAs[api.AuditRecord](t, streamed[len(streamed)-2])
	assert.Equal(t, api.AuditEventApproval, failure.Event, "the stream tells what became of the call before the run's end")
	cancelled := f.finish(t, run.ID)
	assert.Equal(t, decodeAs[api.Run](t, streamed[len(streamed)-1]), cancelled)
	assert.Equal(t, api.RunStatusCancelled, cancelled.Status)
	assert.Equal(t, "cancelled by alice", cancelled.Error)
	var pending []api.ApprovalRequest
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/approvals", nil, &pending))
	assert.Empty(t, pending, "the request is withdrawn")
	var resp api.Error
	assert.Equal(t, http.StatusNotFound, f.do(t, http.MethodPost, home+"/runs/"+run.ID+"/approvals/"+req.ID, api.Answer{Approved: true}, &resp))
	audit := f.audit(t, run.ID)
	last := audit[len(audit)-1]
	assert.Equal(t, api.AuditEventApproval, last.Event, "the audit log tells what became of the call")
	assert.Equal(t, api.DecisionDeny, last.Decision)
	assert.Equal(t, "approval failed: cancelled by alice", last.Reason)
	assert.Zero(t, f.write.Calls)
}

// TestApprovals_ACancelAsTheRunResumesRecordsTheCallAsNotRun: a run cancelled
// after a worker took it up to resume, before the call ran, ends with the
// call recorded as not run, in its audit log and its transcript.
func TestApprovals_ACancelAsTheRunResumesRecordsTheCallAsNotRun(t *testing.T) {
	var (
		mu       sync.Mutex
		models   int
		resuming = make(chan struct{})
		proceed  = make(chan struct{})
	)
	f := newFixture(t, options{policy: writesNeedApproval, newModel: func(harness.Model) (model.Model, error) {
		mu.Lock()
		models++
		first := models == 1
		mu.Unlock()
		if first {
			return modeltest.NewScripted(modeltest.CallTools(call("c0", "files_read", `{"path":"notes.md"}`), call("c1", "files_write", `{"path":"notes.md"}`))), nil
		}
		close(resuming)
		<-proceed
		return modeltest.NewScripted(), nil
	}})
	f.putNotes(t)
	run := f.startRun(t, "write my notes")
	var req api.ApprovalRequest
	for events := f.events(t, run.ID); req.ID == ""; {
		if e := events.next(); e.name == api.EventApproval {
			req = decodeAs[api.ApprovalRequest](t, e)
		}
	}
	f.answer(t, req, api.Answer{Approved: true})
	<-resuming

	require.Equal(t, http.StatusAccepted, f.do(t, http.MethodPost, home+"/runs/"+run.ID+"/cancel", nil, nil))
	close(proceed)

	assert.Equal(t, api.RunStatusCancelled, f.finish(t, run.ID).Status)
	assert.Zero(t, f.write.Calls)
	audit := f.audit(t, run.ID)
	last := audit[len(audit)-1]
	assert.Equal(t, api.AuditEventApproval, last.Event)
	assert.Equal(t, "approval failed: cancelled by alice", last.Reason)
	var transcript []api.TranscriptMessage
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/runs/"+run.ID+"/transcript", nil, &transcript))
	results := transcript[len(transcript)-1].ToolResults
	require.Len(t, results, 2, "the transcript holds the reply's results")
	assert.False(t, results[0].IsError, "the read before the write ran")
	assert.Contains(t, results[1].Content, "approval failed: cancelled by alice")
}

// TestInvariant_AWaitingCallNeverHoldsACredential guards trust-model
// guarantee 5 for durable approvals: a call that needs approval and whose
// arguments hold a credential is denied, so no request, event or stored
// approval ever holds the credential, nor waits for an answer it could not
// be run by.
func TestInvariant_AWaitingCallNeverHoldsACredential(t *testing.T) {
	f := newFixture(t, options{policy: writesNeedApproval})
	f.putNotes(t)
	f.script(modeltest.CallTools(call("c1", "files_write", `{"token":"`+token+`"}`)), modeltest.Reply("could not"))

	run := f.startRun(t, "write the token")
	events := f.events(t, run.ID).rest()

	assert.Equal(t, api.RunStatusSucceeded, decodeAs[api.Run](t, events[len(events)-1]).Status)
	for _, e := range events {
		assert.NotEqual(t, api.EventApproval, e.name, "nothing waits")
		assert.NotContains(t, e.data, token)
	}
	assert.Zero(t, f.write.Calls)
	audit := f.audit(t, run.ID)
	last := audit[len(audit)-1]
	assert.Equal(t, api.DecisionDeny, last.Decision)
	assert.Contains(t, last.Reason, "its arguments hold a secret")
	var pending []api.ApprovalRequest
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/approvals", nil, &pending))
	assert.Empty(t, pending)
	_, stored, err := f.store.LatestApproval(t.Context(), run.ID)
	require.NoError(t, err)
	assert.False(t, stored, "no approval is stored")
	auditJSON, err := json.Marshal(audit)
	require.NoError(t, err)
	assert.NotContains(t, string(auditJSON), token)
}

// TestApprovals_LeavingCancelsTheRunsOfWhoLeft: a run whose owner is no
// longer a member of its workspace when a server starts is cancelled before
// any worker takes it up; its waiting request is withdrawn and its
// conversation records the call as not run. Another member's run goes on,
// and the one who left still reads theirs.
func TestApprovals_LeavingCancelsTheRunsOfWhoLeft(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, options{policy: writesNeedApproval})
	f.putNotes(t)
	alices, _ := f.waitForApproval(t)
	f.script(modeltest.CallTools(call("c2", "files_write", `{"path":"bob.md"}`)))
	var bobs api.Run
	require.Equal(t, http.StatusCreated, f.doAs(t, bobToken, http.MethodPost, home+"/runs", api.CreateRun{Harness: "notes", Input: "write mine"}, &bobs))
	for e := f.eventsAs(t, bobToken, bobs.ID); e.next().name != api.EventApproval; {
	}

	f.restart(t, options{policy: writesNeedApproval, workspaces: map[string]config.Workspace{
		"home": {Members: []string{"bob", "dana"}},
	}})

	cancelled, err := f.store.Run(ctx, "home", alices.ID)
	require.NoError(t, err)
	assert.Equal(t, store.RunCancelled, cancelled.Status)
	assert.Equal(t, "cancelled by the server, as alice is no longer a member of home", cancelled.Error)
	request, ok, err := f.store.LatestApproval(ctx, alices.ID)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, store.ApprovalWithdrawn, request.Status)
	transcript, err := f.store.Transcript(ctx, alices.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, transcript[len(transcript)-1].ToolResults, "the call is recorded as not run")
	finished := f.logEvents(t, "run.finished")
	require.Len(t, finished, 1, "only the run of who left ended")
	assert.Equal(t, alices.ID, finished[0].RunID)
	assert.Contains(t, string(finished[0].Details), `"status":"cancelled"`)
	assert.Zero(t, f.write.Calls, "the agent never acted for someone who left")

	still, err := f.store.Run(ctx, "home", bobs.ID)
	require.NoError(t, err)
	assert.Equal(t, store.RunWaiting, still.Status, "another member's run goes on waiting")

	var read api.Run
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/runs/"+alices.ID, nil, &read), "alice still reads her run")
	assert.Equal(t, api.RunStatusCancelled, read.Status)
}

// TestApprovals_LeavingCancelsQueuedRuns: a queued run of someone who left,
// and one in a workspace the config no longer has, are cancelled at the
// start, and no agent ever runs them.
func TestApprovals_LeavingCancelsQueuedRuns(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, options{})
	f.putNotes(t)
	v, err := f.store.Harness(ctx, "home", "notes")
	require.NoError(t, err)
	gone, err := f.store.PutHarness(ctx, "gone", "bob", notes())
	require.NoError(t, err)
	// Queued while no server runs, so none takes them up before the start.
	f.server.Close()
	f.http.Close()
	for _, r := range []store.NewRun{
		{ID: "alices", HarnessVersionID: v.ID, Input: "x", StartedBy: "alice"},
		{ID: "bobs-gone", HarnessVersionID: gone.ID, Input: "x", StartedBy: "bob"},
	} {
		_, err := f.store.CreateRun(ctx, r)
		require.NoError(t, err)
	}
	m := f.script(modeltest.Reply("never"))
	f.serve(t, options{workspaces: map[string]config.Workspace{"home": {Members: []string{"bob", "dana"}}}})

	for _, tt := range []struct{ workspace, id, why string }{
		{"home", "alices", "alice is no longer a member of home"},
		{"gone", "bobs-gone", "bob is no longer a member of gone"},
	} {
		run, err := f.store.Run(ctx, tt.workspace, tt.id)
		require.NoError(t, err)
		assert.Equal(t, store.RunCancelled, run.Status, tt.id)
		assert.Contains(t, run.Error, tt.why)
	}
	assert.Empty(t, m.Requests(), "no agent ran")
}
