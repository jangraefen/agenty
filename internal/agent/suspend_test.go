package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/agent"
	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/model/modeltest"
	"github.com/jangraefen/agenty/internal/policy"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

// labelsNeedApproval is central policy that asks before every label.
var labelsNeedApproval = []policy.Module{{Name: "central.rego", Source: `package agenty.tool

require_approval contains "labels need a human" if input.tool == "tickets_label"`}}

// suspendingAgent is an agent for m whose policy suspends the run at every
// label, recording its transcript in tr. records, if any, are the run's
// audit log so far, as for a run that resumes.
func (f *fixture) suspendingAgent(t *testing.T, m model.Model, tr *transcript, records ...toolgateway.Record) *agent.Agent {
	t.Helper()
	cfg := f.config(m)
	cfg.Policy = labelsNeedApproval
	cfg.Records = records
	if tr != nil {
		cfg.Transcript = tr
	}
	a, err := agent.New(context.Background(), cfg)
	require.NoError(t, err)
	return a
}

// suspend runs a reply of three calls whose second waits for approval.
func (f *fixture) suspend(t *testing.T, tr *transcript) (agent.Result, *agent.Suspended) {
	t.Helper()
	m := modeltest.NewScripted(modeltest.CallTools(call("c1", "tickets_read"), call("c2", "tickets_label"), call("c3", "tickets_read")))
	res, err := f.suspendingAgent(t, m, tr).Continue(context.Background(), nil, "ticket 7")
	var suspended *agent.Suspended
	require.ErrorAs(t, err, &suspended)
	return res, suspended
}

func TestRun_SuspendsAtACallThatWaitsForApproval(t *testing.T) {
	f := newFixture(5)
	tr := &transcript{failAt: -1}

	res, suspended := f.suspend(t, tr)

	assert.Equal(t, 1, f.read.Calls, "the call before the waiting one ran")
	assert.Zero(t, f.label.Calls)
	assert.Equal(t, 1, suspended.Call, "the second call waits")
	require.Len(t, suspended.Results, 1)
	assert.Equal(t, model.ToolResult{CallID: "c1", Content: `{"title":"Printer on fire"}`}, suspended.Results[0])
	decisions := recordsOf(f.audit.Records, toolgateway.EventDecision)
	require.Len(t, decisions, 2)
	assert.Equal(t, decisions[1].CallID, suspended.CallID)
	assert.Equal(t, []string{"labels need a human"}, suspended.Reasons)
	assert.Equal(t, 1, res.Steps)
	require.Len(t, res.Messages, 2, "the run stops after the reply, before its results")
	assert.Equal(t, res.Messages, tr.messages)
}

func TestResume_ContinuesAtTheWaitingCall(t *testing.T) {
	f := newFixture(5)
	first, suspended := f.suspend(t, &transcript{failAt: -1})
	tr := &transcript{failAt: -1}
	m := modeltest.NewScripted(modeltest.Reply("labelled"))
	a := f.suspendingAgent(t, m, tr, f.audit.Records...)

	res, err := a.Resume(context.Background(), nil, first.Messages, agent.Resumption{
		CallID:  suspended.CallID,
		Call:    suspended.Call,
		Results: suspended.Results,
		Answer:  toolgateway.Approval{Approved: true, Approver: "ana"},
		Note:    "Note: the server started anew.",
	})

	require.NoError(t, err)
	assert.Equal(t, "r1", a.ID())
	assert.Equal(t, "labelled", res.Output)
	assert.Equal(t, 2, res.Steps, "the step before the suspension counts")
	assert.Equal(t, 1, f.label.Calls, "the approved call ran")
	assert.Equal(t, 2, f.read.Calls, "the call after it ran, the one before did not run again")
	require.Len(t, res.Messages, 4)
	results := res.Messages[2].ToolResults
	require.Len(t, results, 3)
	assert.Equal(t, suspended.Results[0], results[0])
	assert.Equal(t, "c2", results[1].CallID)
	assert.Equal(t, `{"ok":true}`+"\n\nNote: the server started anew.", results[1].Content)
	assert.Equal(t, "c3", results[2].CallID)
	assert.Equal(t, []int{2, 3}, tr.indexes, "the transcript goes on where it stopped")
	require.Len(t, m.Requests(), 1)
	assert.Equal(t, res.Messages[:3], m.Requests()[0].Messages)
	approvals := recordsOf(f.audit.Records, toolgateway.EventApproval)
	require.Len(t, approvals, 1)
	assert.Equal(t, suspended.CallID, approvals[0].CallID)
	assert.Equal(t, "ana", approvals[0].Approver)
}

func TestResume_ReportsARejectedCallToTheModel(t *testing.T) {
	f := newFixture(5)
	first, suspended := f.suspend(t, &transcript{failAt: -1})
	m := modeltest.NewScripted(modeltest.Reply("left as is"))

	res, err := f.suspendingAgent(t, m, nil, f.audit.Records...).Resume(context.Background(), nil, first.Messages, agent.Resumption{
		CallID: suspended.CallID, Call: suspended.Call, Results: suspended.Results,
		Answer: toolgateway.Approval{Approver: "ana", Reason: "no answer within 1h"},
	})

	require.NoError(t, err)
	assert.Zero(t, f.label.Calls)
	result := res.Messages[2].ToolResults[1]
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "approval rejected: no answer within 1h")
}

func TestResume_SuspendsAgainAtTheNextWaitingCall(t *testing.T) {
	f := newFixture(5)
	m := modeltest.NewScripted(modeltest.CallTools(call("c1", "tickets_label"), call("c2", "tickets_label")))
	first, err := f.suspendingAgent(t, m, nil).Continue(context.Background(), nil, "ticket 7")
	var suspended *agent.Suspended
	require.ErrorAs(t, err, &suspended)
	require.Equal(t, 0, suspended.Call)

	res, err := f.suspendingAgent(t, modeltest.NewScripted(), nil, f.audit.Records...).Resume(context.Background(), nil, first.Messages, agent.Resumption{
		CallID: suspended.CallID, Call: suspended.Call, Answer: toolgateway.Approval{Approved: true},
	})

	var again *agent.Suspended
	require.ErrorAs(t, err, &again)
	assert.Equal(t, 1, again.Call)
	require.Len(t, again.Results, 1, "the results so far carry over")
	assert.Equal(t, "c1", again.Results[0].CallID)
	assert.NotEqual(t, suspended.CallID, again.CallID)
	assert.Len(t, res.Messages, 2)
	assert.Equal(t, 1, f.label.Calls)
}

func TestResume_RejectsWhatItCannotResume(t *testing.T) {
	f := newFixture(5)
	first, suspended := f.suspend(t, &transcript{failAt: -1})
	answered := []model.Message{first.Messages[0], {Role: model.RoleAssistant, Text: "done"}}
	tests := []struct {
		name    string
		own     []model.Message
		call    int
		wantErr string
	}{
		{"nothing to resume", nil, 1, "does not end with tool calls"},
		{"a run that answered", answered, 1, "does not end with tool calls"},
		{"a call the reply does not make", first.Messages, 3, "has no call 3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := f.suspendingAgent(t, modeltest.NewScripted(), nil, f.audit.Records...)

			_, err := run.Resume(context.Background(), nil, tt.own, agent.Resumption{CallID: suspended.CallID, Call: tt.call, Answer: toolgateway.Approval{Approved: true}})

			require.ErrorContains(t, err, tt.wantErr)
			assert.Zero(t, f.label.Calls)
		})
	}
}
