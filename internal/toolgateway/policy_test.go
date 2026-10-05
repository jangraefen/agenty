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
	gw, err := toolgateway.New(toolgateway.Config{
		RunID:        runID,
		Harness:      "triage",
		MaxToolCalls: 100,
		Policy:       policy,
		Granted:      []string{"tickets_read", "tickets_label", "tickets_close"},
		Tools:        []toolgateway.Tool{read, label, &gatewaytest.Tool{Name: "tickets_close"}},
		Audit:        &gatewaytest.Audit{},
	})
	require.NoError(t, err)

	ctx := context.Background()
	_, err = gw.Call(ctx, toolgateway.ToolCall{Name: "tickets_read", Args: json.RawMessage(`{"id":1}`)})
	require.NoError(t, err)
	_, err = gw.Call(ctx, toolgateway.ToolCall{Name: "tickets_close"})
	require.ErrorIs(t, err, toolgateway.ErrDenied)
	_, err = gw.Call(ctx, toolgateway.ToolCall{Name: "tickets_label"})
	require.Error(t, err, "the tool fails, but it was executed")
	_, err = gw.Call(ctx, toolgateway.ToolCall{Name: "tickets_nope"})
	require.ErrorIs(t, err, toolgateway.ErrDenied)
	_, err = gw.Call(ctx, toolgateway.ToolCall{Name: "tickets_read", Args: json.RawMessage(`{"id":2}`)})
	require.NoError(t, err)

	require.Len(t, policy.Inputs, 4, "ungranted calls never reach policy")
	first := policy.Inputs[0]
	assert.Equal(t, runID, first.RunID)
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
	gw, err := toolgateway.New(toolgateway.Config{
		RunID:        runID,
		Harness:      "triage",
		MaxToolCalls: 100,
		Policy: &gatewaytest.Policy{Verdicts: map[string]toolgateway.Verdict{
			"tickets_label": {Decision: toolgateway.RequireApproval, Reasons: []string{"writes need a human", "label is public"}},
		}},
		Approver: approver,
		Granted:  []string{"tickets_label"},
		Tools:    []toolgateway.Tool{&gatewaytest.Tool{Name: "tickets_label"}},
		Audit:    &gatewaytest.Audit{},
	})
	require.NoError(t, err)

	_, err = gw.Call(context.Background(), toolgateway.ToolCall{Name: "tickets_label", Args: json.RawMessage(`{"label":"urgent"}`)})
	require.NoError(t, err)

	require.Len(t, approver.Calls, 1)
	assert.Equal(t, gatewaytest.ApprovalCall{
		Request: toolgateway.Request{
			RunID:   runID,
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
	gw, err := toolgateway.New(toolgateway.Config{
		RunID:        runID,
		MaxToolCalls: 2,
		Policy:       policy,
		Granted:      []string{"tickets_read"},
		Tools:        []toolgateway.Tool{read},
		Audit:        audit,
	})
	require.NoError(t, err)

	ctx := context.Background()
	_, err = gw.Call(ctx, toolgateway.ToolCall{Name: "tickets_delete"})
	require.ErrorIs(t, err, toolgateway.ErrDenied, "attempt 1: denied, but counted")
	_, err = gw.Call(ctx, toolgateway.ToolCall{Name: "tickets_read"})
	require.NoError(t, err, "attempt 2: within the limit")
	_, err = gw.Call(ctx, toolgateway.ToolCall{Name: "tickets_read"})
	require.ErrorIs(t, err, toolgateway.ErrDenied, "attempt 3: over the limit")
	require.ErrorContains(t, err, "tool call limit reached")

	assert.Equal(t, 1, read.Calls)
	assert.Len(t, policy.Inputs, 1, "a call over the limit never reaches policy")
	require.Len(t, audit.Records, 4)
	assert.Equal(t, "tool call limit reached", audit.Records[3].Reason)
}
