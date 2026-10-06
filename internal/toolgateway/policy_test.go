package toolgateway_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/toolgateway"
	"github.com/jangraefen/agenty/internal/toolgateway/gatewaytest"
)

func TestCall_PolicySeesTheCallAndExecutedCounts(t *testing.T) {
	read := &gatewaytest.Tool{Name: "tickets_read"}
	label := &gatewaytest.Tool{Name: "tickets_label", Err: errors.New("label service down")}
	policy := &gatewaytest.Policy{Verdicts: map[string]toolgateway.Verdict{
		"tickets_close": {Decision: toolgateway.Deny, Reasons: []string{"no closing"}},
	}}
	gw, err := toolgateway.New(context.Background(), toolgateway.Config{
		Redactor:     gatewaytest.NoSecrets,
		Harness:      "triage",
		MaxToolCalls: 100,
		Policy:       policy,
		Granted:      []string{"tickets_read", "tickets_label", "tickets_close"},
		Servers:      gatewaytest.Servers(read, label, &gatewaytest.Tool{Name: "tickets_close"}),
		Audit:        &gatewaytest.Audit{},
	})
	require.NoError(t, err)
	run := gw.Start()

	ctx := context.Background()
	_, err = run.Call(ctx, toolgateway.ToolCall{Name: "tickets_read", Args: json.RawMessage(`{"id":1}`)})
	require.NoError(t, err)
	_, err = run.Call(ctx, toolgateway.ToolCall{Name: "tickets_close"})
	require.ErrorIs(t, err, toolgateway.ErrDenied)
	_, err = run.Call(ctx, toolgateway.ToolCall{Name: "tickets_label"})
	require.Error(t, err, "the tool fails, but it was executed")
	_, err = run.Call(ctx, toolgateway.ToolCall{Name: "tickets_nope"})
	require.ErrorIs(t, err, toolgateway.ErrDenied)
	_, err = run.Call(ctx, toolgateway.ToolCall{Name: "tickets_read", Args: json.RawMessage(`{"id":2}`)})
	require.NoError(t, err)

	require.Len(t, policy.Inputs, 4, "ungranted calls never reach policy")
	first := policy.Inputs[0]
	assert.Equal(t, run.ID(), first.RunID)
	assert.Equal(t, "triage", first.Harness)
	assert.Equal(t, "tickets_read", first.Tool)
	assert.JSONEq(t, `{"id":1}`, string(first.Args))
	assert.Equal(t, toolgateway.CallCounts{ByTool: map[string]int{}}, first.Calls)

	assert.Equal(t, toolgateway.CallCounts{
		Total:  2,
		ByTool: map[string]int{"tickets_read": 1, "tickets_label": 1},
	}, policy.Inputs[3].Calls, "policy sees executed calls only, failed executions included")
}

func TestCall_ApproverSeesTheRequest(t *testing.T) {
	approver := &gatewaytest.Approver{Approval: toolgateway.Approval{Approved: true, Approver: "alice"}}
	gw, err := toolgateway.New(context.Background(), toolgateway.Config{
		Redactor:     gatewaytest.NoSecrets,
		Harness:      "triage",
		MaxToolCalls: 100,
		Policy: &gatewaytest.Policy{Verdicts: map[string]toolgateway.Verdict{
			"tickets_label": {Decision: toolgateway.RequireApproval, Reasons: []string{"writes need a human", "label is public"}},
		}},
		Approver: approver,
		Granted:  []string{"tickets_label"},
		Servers:  gatewaytest.Servers(&gatewaytest.Tool{Name: "tickets_label"}),
		Audit:    &gatewaytest.Audit{},
	})
	require.NoError(t, err)
	run := gw.Start()

	_, err = run.Call(context.Background(), toolgateway.ToolCall{Name: "tickets_label", Args: json.RawMessage(`{"label":"urgent"}`)})
	require.NoError(t, err)

	require.Len(t, approver.Calls, 1)
	assert.Equal(t, gatewaytest.ApprovalCall{
		Request: toolgateway.Request{
			RunID:   run.ID(),
			Harness: "triage",
			Tool:    "tickets_label",
			Args:    json.RawMessage(`{"label":"urgent"}`),
			Calls:   toolgateway.CallCounts{ByTool: map[string]int{}},
		},
		Reasons: []string{"writes need a human", "label is public"},
	}, approver.Calls[0], "the approver sees the same request as policy")
}

func TestCall_ToolCallLimitCountsEveryAttempt(t *testing.T) {
	read := &gatewaytest.Tool{Name: "tickets_read"}
	policy := &gatewaytest.Policy{}
	audit := &gatewaytest.Audit{}
	gw, err := toolgateway.New(context.Background(), toolgateway.Config{
		Redactor:     gatewaytest.NoSecrets,
		MaxToolCalls: 2,
		Policy:       policy,
		Granted:      []string{"tickets_read"},
		Servers:      gatewaytest.Servers(read),
		Audit:        audit,
	})
	require.NoError(t, err)
	run := gw.Start()

	ctx := context.Background()
	_, err = run.Call(ctx, toolgateway.ToolCall{Name: "tickets_delete"})
	require.ErrorIs(t, err, toolgateway.ErrDenied, "attempt 1: denied, but counted")
	_, err = run.Call(ctx, toolgateway.ToolCall{Name: "tickets_read"})
	require.NoError(t, err, "attempt 2: within the limit")
	_, err = run.Call(ctx, toolgateway.ToolCall{Name: "tickets_read"})
	require.ErrorIs(t, err, toolgateway.ErrDenied, "attempt 3: over the limit")
	require.ErrorContains(t, err, "tool call limit reached")

	assert.Equal(t, 1, read.Calls)
	assert.Len(t, policy.Inputs, 1, "a call over the limit never reaches policy")
	require.Len(t, audit.Records, 4)
	assert.Equal(t, "tool call limit reached", audit.Records[3].Reason)
}

// approveAfterCancel cancels the run's context, then approves: an answer
// that raced the run's cancellation.
type approveAfterCancel struct {
	cancel context.CancelCauseFunc
}

func (a approveAfterCancel) Approve(context.Context, toolgateway.Request, []string) (toolgateway.Approval, error) {
	a.cancel(errors.New("cancelled by bob"))
	return toolgateway.Approval{Approved: true, Approver: "alice", Reason: "ok"}, nil
}

// TestCall_CancellationWinsOverAnApproval: a call is never executed once its
// run is cancelled, even if an approval arrives at the same moment.
func TestCall_CancellationWinsOverAnApproval(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	label := &gatewaytest.Tool{Name: "tickets_label"}
	audit := &gatewaytest.Audit{}
	gw, err := toolgateway.New(context.Background(), toolgateway.Config{
		Redactor:     gatewaytest.NoSecrets,
		Harness:      "triage",
		MaxToolCalls: 100,
		Policy: &gatewaytest.Policy{Verdicts: map[string]toolgateway.Verdict{
			"tickets_label": {Decision: toolgateway.RequireApproval, Reasons: []string{"writes need a human"}},
		}},
		Approver: approveAfterCancel{cancel: cancel},
		Granted:  []string{"tickets_label"},
		Servers:  gatewaytest.Servers(label),
		Audit:    audit,
	})
	require.NoError(t, err)

	_, err = gw.Start().Call(ctx, toolgateway.ToolCall{Name: "tickets_label", Args: json.RawMessage(`{}`)})

	require.ErrorIs(t, err, toolgateway.ErrDenied)
	assert.Zero(t, label.Calls, "nothing runs after the run was cancelled")
	require.Len(t, audit.Records, 2)
	assert.Equal(t, toolgateway.EventApproval, audit.Records[1].Event)
	assert.Equal(t, toolgateway.Deny, audit.Records[1].Decision)
	assert.Equal(t, "approval failed: cancelled by bob", audit.Records[1].Reason)
	assert.Equal(t, "alice", audit.Records[1].Approver, "who answered is still recorded")
}
