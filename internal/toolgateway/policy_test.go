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
	read := &gatewaytest.Tool{Name: "tickets.read"}
	label := &gatewaytest.Tool{Name: "tickets.label", Effect: toolgateway.EffectWrite, Err: errors.New("label service down")}
	policy := &gatewaytest.Policy{Verdicts: map[string]toolgateway.Verdict{
		"tickets.close": {Decision: toolgateway.Deny, Reasons: []string{"no closing"}},
	}}
	gw, err := toolgateway.New(toolgateway.Config{
		RunID:        runID,
		Harness:      "triage",
		MaxToolCalls: 100,
		Policy:       policy,
		Granted:      []string{"tickets.read", "tickets.label", "tickets.close"},
		Tools:        []toolgateway.Tool{read, label, &gatewaytest.Tool{Name: "tickets.close", Effect: toolgateway.EffectWrite}},
		Audit:        &gatewaytest.Audit{},
	})
	require.NoError(t, err)

	ctx := context.Background()
	_, err = gw.Call(ctx, toolgateway.ToolCall{Name: "tickets.read", Args: json.RawMessage(`{"id":1}`)})
	require.NoError(t, err)
	_, err = gw.Call(ctx, toolgateway.ToolCall{Name: "tickets.close"})
	require.ErrorIs(t, err, toolgateway.ErrDenied)
	_, err = gw.Call(ctx, toolgateway.ToolCall{Name: "tickets.label"})
	require.Error(t, err, "the tool fails, but it was executed")
	_, err = gw.Call(ctx, toolgateway.ToolCall{Name: "tickets.nope"})
	require.ErrorIs(t, err, toolgateway.ErrDenied)
	_, err = gw.Call(ctx, toolgateway.ToolCall{Name: "tickets.read", Args: json.RawMessage(`{"id":2}`)})
	require.NoError(t, err)

	require.Len(t, policy.Inputs, 4, "ungranted calls never reach policy")
	first := policy.Inputs[0]
	assert.Equal(t, runID, first.RunID)
	assert.Equal(t, "triage", first.Harness)
	assert.Equal(t, "tickets.read", first.Tool)
	assert.Equal(t, toolgateway.EffectRead, first.Effect)
	assert.JSONEq(t, `{"id":1}`, string(first.Args))
	assert.Equal(t, toolgateway.CallCounts{ByTool: map[string]int{}, ByEffect: map[toolgateway.Effect]int{}}, first.Calls)

	assert.Equal(t, toolgateway.EffectWrite, policy.Inputs[2].Effect)
	assert.Equal(t, toolgateway.CallCounts{
		Total:    2,
		ByTool:   map[string]int{"tickets.read": 1, "tickets.label": 1},
		ByEffect: map[toolgateway.Effect]int{toolgateway.EffectRead: 1, toolgateway.EffectWrite: 1},
	}, policy.Inputs[3].Calls, "policy sees executed calls only, failed executions included")
}

func TestCall_ApproverSeesTheRequest(t *testing.T) {
	approver := &gatewaytest.Approver{Approval: toolgateway.Approval{Approved: true, Approver: "alice"}}
	gw, err := toolgateway.New(toolgateway.Config{
		RunID:        runID,
		MaxToolCalls: 100,
		Policy: &gatewaytest.Policy{Verdicts: map[string]toolgateway.Verdict{
			"tickets.label": {Decision: toolgateway.RequireApproval, Reasons: []string{"writes need a human", "label is public"}},
		}},
		Approver: approver,
		Granted:  []string{"tickets.label"},
		Tools:    []toolgateway.Tool{&gatewaytest.Tool{Name: "tickets.label", Effect: toolgateway.EffectWrite}},
		Audit:    &gatewaytest.Audit{},
	})
	require.NoError(t, err)

	_, err = gw.Call(context.Background(), toolgateway.ToolCall{Name: "tickets.label", Args: json.RawMessage(`{"label":"urgent"}`)})
	require.NoError(t, err)

	require.Len(t, approver.Requests, 1)
	assert.Equal(t, toolgateway.ApprovalRequest{
		RunID:   runID,
		Tool:    "tickets.label",
		Effect:  toolgateway.EffectWrite,
		Args:    json.RawMessage(`{"label":"urgent"}`),
		Reasons: []string{"writes need a human", "label is public"},
	}, approver.Requests[0])
}

func TestCall_ToolCallLimitCountsEveryAttempt(t *testing.T) {
	read := &gatewaytest.Tool{Name: "tickets.read"}
	policy := &gatewaytest.Policy{}
	audit := &gatewaytest.Audit{}
	gw, err := toolgateway.New(toolgateway.Config{
		RunID:        runID,
		MaxToolCalls: 2,
		Policy:       policy,
		Granted:      []string{"tickets.read"},
		Tools:        []toolgateway.Tool{read},
		Audit:        audit,
	})
	require.NoError(t, err)

	ctx := context.Background()
	_, err = gw.Call(ctx, toolgateway.ToolCall{Name: "tickets.delete"})
	require.ErrorIs(t, err, toolgateway.ErrDenied, "attempt 1: denied, but counted")
	_, err = gw.Call(ctx, toolgateway.ToolCall{Name: "tickets.read"})
	require.NoError(t, err, "attempt 2: within the limit")
	_, err = gw.Call(ctx, toolgateway.ToolCall{Name: "tickets.read"})
	require.ErrorIs(t, err, toolgateway.ErrDenied, "attempt 3: over the limit")
	require.ErrorContains(t, err, "tool call limit reached")

	assert.Equal(t, 1, read.Calls)
	assert.Len(t, policy.Inputs, 1, "a call over the limit never reaches policy")
	require.Len(t, audit.Records, 4)
	assert.Equal(t, "tool call limit reached", audit.Records[3].Reason)
}
