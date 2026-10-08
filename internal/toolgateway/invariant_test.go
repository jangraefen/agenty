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

// TestInvariant_DefaultDeny guards trust-model guarantee 3:
// a tool not granted to the harness is denied and never executed, a grant no
// server serves stops the gateway from starting, and when policy or approval
// cannot give a clear allow, the call is denied too.
func TestInvariant_DefaultDeny(t *testing.T) {
	tests := []struct {
		name    string
		granted []string
		call    string
		reason  string
	}{
		{"ungranted tool", []string{"tickets_read"}, "tickets_delete", "tool not granted"},
		{"no grants at all", nil, "tickets_read", "tool not granted"},
		{"grant match is case-sensitive", []string{"tickets_read"}, "Tickets_Read", "tool not granted"},
		{"empty tool name", []string{"tickets_read"}, "", "tool not granted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			read := &gatewaytest.Tool{Name: "tickets_read", Result: json.RawMessage(`{}`)}
			del := &gatewaytest.Tool{Name: "tickets_delete", Result: json.RawMessage(`{}`)}
			audit := &gatewaytest.Audit{}
			gw, err := toolgateway.New(context.Background(), toolgateway.Config{
				RunID:        "r1",
				Redactor:     gatewaytest.NoSecrets,
				MaxToolCalls: 100,
				Policy:       &gatewaytest.Policy{},
				Granted:      tt.granted,
				Servers:      gatewaytest.Servers(read, del),
				Audit:        audit,
			})
			require.NoError(t, err)

			result, err := gw.Call(context.Background(), toolgateway.ToolCall{Name: tt.call})

			require.ErrorIs(t, err, toolgateway.ErrDenied)
			require.ErrorContains(t, err, tt.reason)
			assert.Nil(t, result)
			assert.Zero(t, read.Calls, "denied call must not execute any tool")
			assert.Zero(t, del.Calls, "denied call must not execute any tool")
			require.Len(t, audit.Records, 1)
			assert.Equal(t, toolgateway.Deny, audit.Records[0].Decision)
			assert.Equal(t, tt.reason, audit.Records[0].Reason)
		})
	}

	failClosed := []struct {
		name   string
		policy *gatewaytest.Policy
		reason string
	}{
		{
			name:   "policy error",
			policy: &gatewaytest.Policy{Err: errors.New("opa down")},
			reason: "policy error: opa down",
		},
		{
			name: "policy denies",
			policy: &gatewaytest.Policy{Verdicts: map[string]toolgateway.Verdict{
				"tickets_read": {Decision: toolgateway.Deny, Reasons: []string{"frozen", "audit week"}},
			}},
			reason: "policy: frozen; audit week",
		},
		{
			name: "unknown policy decision",
			policy: &gatewaytest.Policy{Verdicts: map[string]toolgateway.Verdict{
				"tickets_read": {Decision: "maybe"},
			}},
			reason: `invalid policy decision "maybe"`,
		},
	}
	for _, tt := range failClosed {
		t.Run("fails closed: "+tt.name, func(t *testing.T) {
			read := &gatewaytest.Tool{Name: "tickets_read", Result: json.RawMessage(`{}`)}
			audit := &gatewaytest.Audit{}
			gw, err := toolgateway.New(context.Background(), toolgateway.Config{
				RunID:        "r1",
				Redactor:     gatewaytest.NoSecrets,
				MaxToolCalls: 100,
				Policy:       tt.policy,
				Granted:      []string{"tickets_read"},
				Servers:      gatewaytest.Servers(read),
				Audit:        audit,
			})
			require.NoError(t, err)

			_, err = gw.Call(context.Background(), toolgateway.ToolCall{Name: "tickets_read"})

			require.ErrorIs(t, err, toolgateway.ErrDenied)
			assert.Zero(t, read.Calls)
			require.Len(t, audit.Records, 1)
			assert.Equal(t, toolgateway.Deny, audit.Records[0].Decision)
			assert.Equal(t, tt.reason, audit.Records[0].Reason)
		})
	}
}

// TestInvariant_EveryCallAudited guards trust-model guarantee 6:
// every tool call is recorded with its decision, approval (if one was needed)
// and result, and a call whose decision or approval cannot be recorded is not
// executed. A call that needs approval suspends the run with only its
// decision recorded; once answered, Resume records the answer before the
// call runs.
func TestInvariant_EveryCallAudited(t *testing.T) {
	args := json.RawMessage(`{"id":42}`)
	toolErr := errors.New("ticket system unavailable")
	needsApproval := toolgateway.Verdict{Decision: toolgateway.RequireApproval, Reasons: []string{"needs a human"}}

	tests := []struct {
		name      string
		call      string
		toolErr   error
		failOn    toolgateway.Event
		verdict   *toolgateway.Verdict
		cancelled bool
		// answer, if set, answers the call, which must have suspended.
		answer        *toolgateway.Approval
		wantSuspended bool
		wantCalls     int
		wantResult    json.RawMessage
		wantErrIs     []error
		wantRecords   []toolgateway.Record
	}{
		{
			name:       "allowed call records decision and result",
			call:       "tickets_read",
			wantCalls:  1,
			wantResult: json.RawMessage(`{"ok":true}`),
			wantRecords: []toolgateway.Record{
				{Event: toolgateway.EventDecision, Tool: "tickets_read", Args: args, Decision: toolgateway.Allow},
				{Event: toolgateway.EventResult, Tool: "tickets_read", Args: args, Decision: toolgateway.Allow, Result: json.RawMessage(`{"ok":true}`)},
			},
		},
		{
			name:      "denied call records the denial",
			call:      "tickets_delete",
			wantErrIs: []error{toolgateway.ErrDenied},
			wantRecords: []toolgateway.Record{
				{Event: toolgateway.EventDecision, Tool: "tickets_delete", Args: args, Decision: toolgateway.Deny, Reason: "tool not granted"},
			},
		},
		{
			name:      "failing tool records the error",
			call:      "tickets_read",
			toolErr:   toolErr,
			wantCalls: 1,
			wantErrIs: []error{toolErr},
			wantRecords: []toolgateway.Record{
				{Event: toolgateway.EventDecision, Tool: "tickets_read", Args: args, Decision: toolgateway.Allow},
				{Event: toolgateway.EventResult, Tool: "tickets_read", Args: args, Decision: toolgateway.Allow, Err: "ticket system unavailable"},
			},
		},
		{
			name:      "unrecorded decision blocks execution",
			call:      "tickets_read",
			failOn:    toolgateway.EventDecision,
			wantErrIs: []error{toolgateway.ErrAudit, gatewaytest.ErrAuditDown},
		},
		{
			name:      "unrecorded denial is still a denial",
			call:      "tickets_delete",
			failOn:    toolgateway.EventDecision,
			wantErrIs: []error{toolgateway.ErrDenied, toolgateway.ErrAudit, gatewaytest.ErrAuditDown},
		},
		{
			name:       "unrecorded result is reported with the result",
			call:       "tickets_read",
			failOn:     toolgateway.EventResult,
			wantCalls:  1,
			wantResult: json.RawMessage(`{"ok":true}`),
			wantErrIs:  []error{toolgateway.ErrAudit, gatewaytest.ErrAuditDown},
			wantRecords: []toolgateway.Record{
				{Event: toolgateway.EventDecision, Tool: "tickets_read", Args: args, Decision: toolgateway.Allow},
			},
		},
		{
			name:          "call that needs approval suspends with its decision recorded",
			call:          "tickets_read",
			verdict:       &needsApproval,
			wantSuspended: true,
			wantRecords: []toolgateway.Record{
				{Event: toolgateway.EventDecision, Tool: "tickets_read", Args: args, Decision: toolgateway.RequireApproval, Reason: "policy: needs a human"},
			},
		},
		{
			name:       "approved call records decision, approval and result",
			call:       "tickets_read",
			verdict:    &needsApproval,
			answer:     &toolgateway.Approval{Approved: true, Approver: "alice", Reason: "looks fine"},
			wantCalls:  1,
			wantResult: json.RawMessage(`{"ok":true}`),
			wantRecords: []toolgateway.Record{
				{Event: toolgateway.EventDecision, Tool: "tickets_read", Args: args, Decision: toolgateway.RequireApproval, Reason: "policy: needs a human"},
				{Event: toolgateway.EventDecision, Tool: "tickets_read", Args: args, Decision: toolgateway.RequireApproval, Reason: "policy: needs a human"},
				{Event: toolgateway.EventApproval, Tool: "tickets_read", Args: args, Decision: toolgateway.Allow, Reason: "looks fine", Approver: "alice"},
				{Event: toolgateway.EventResult, Tool: "tickets_read", Args: args, Decision: toolgateway.Allow, Reason: "looks fine", Approver: "alice", Result: json.RawMessage(`{"ok":true}`)},
			},
		},
		{
			name:      "rejected approval records the rejection",
			call:      "tickets_read",
			verdict:   &needsApproval,
			answer:    &toolgateway.Approval{Approved: false, Approver: "bob", Reason: "not today"},
			wantErrIs: []error{toolgateway.ErrDenied},
			wantRecords: []toolgateway.Record{
				{Event: toolgateway.EventDecision, Tool: "tickets_read", Args: args, Decision: toolgateway.RequireApproval, Reason: "policy: needs a human"},
				{Event: toolgateway.EventDecision, Tool: "tickets_read", Args: args, Decision: toolgateway.RequireApproval, Reason: "policy: needs a human"},
				{Event: toolgateway.EventApproval, Tool: "tickets_read", Args: args, Decision: toolgateway.Deny, Reason: "approval rejected: not today", Approver: "bob"},
			},
		},
		{
			name:      "call that cannot wait for approval records the failure",
			call:      "tickets_read",
			verdict:   &needsApproval,
			cancelled: true,
			wantErrIs: []error{toolgateway.ErrDenied},
			wantRecords: []toolgateway.Record{
				{Event: toolgateway.EventDecision, Tool: "tickets_read", Args: args, Decision: toolgateway.RequireApproval, Reason: "policy: needs a human"},
				{Event: toolgateway.EventApproval, Tool: "tickets_read", Args: args, Decision: toolgateway.Deny, Reason: "approval failed: context canceled"},
			},
		},
		{
			name:      "unrecorded approval blocks execution",
			call:      "tickets_read",
			verdict:   &needsApproval,
			answer:    &toolgateway.Approval{Approved: true, Approver: "alice"},
			failOn:    toolgateway.EventApproval,
			wantErrIs: []error{toolgateway.ErrAudit, gatewaytest.ErrAuditDown},
			wantRecords: []toolgateway.Record{
				{Event: toolgateway.EventDecision, Tool: "tickets_read", Args: args, Decision: toolgateway.RequireApproval, Reason: "policy: needs a human"},
				{Event: toolgateway.EventDecision, Tool: "tickets_read", Args: args, Decision: toolgateway.RequireApproval, Reason: "policy: needs a human"},
			},
		},
		{
			name:      "unrecorded rejection is still a denial",
			call:      "tickets_read",
			verdict:   &needsApproval,
			answer:    &toolgateway.Approval{Approved: false, Approver: "bob"},
			failOn:    toolgateway.EventApproval,
			wantErrIs: []error{toolgateway.ErrDenied, toolgateway.ErrAudit, gatewaytest.ErrAuditDown},
			wantRecords: []toolgateway.Record{
				{Event: toolgateway.EventDecision, Tool: "tickets_read", Args: args, Decision: toolgateway.RequireApproval, Reason: "policy: needs a human"},
				{Event: toolgateway.EventDecision, Tool: "tickets_read", Args: args, Decision: toolgateway.RequireApproval, Reason: "policy: needs a human"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			read := &gatewaytest.Tool{Name: "tickets_read", Result: json.RawMessage(`{"ok":true}`), Err: tt.toolErr}
			if tt.toolErr != nil {
				read.Result = nil
			}
			del := &gatewaytest.Tool{Name: "tickets_delete"}
			audit := &gatewaytest.Audit{FailOn: tt.failOn}
			policy := &gatewaytest.Policy{}
			if tt.verdict != nil {
				policy.Verdicts = map[string]toolgateway.Verdict{"tickets_read": *tt.verdict}
			}
			gw, err := toolgateway.New(context.Background(), toolgateway.Config{
				RunID:        "r1",
				Redactor:     gatewaytest.NoSecrets,
				MaxToolCalls: 100,
				Policy:       policy,
				Granted:      []string{"tickets_read"},
				Servers:      gatewaytest.Servers(read, del),
				Audit:        audit,
			})
			require.NoError(t, err)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancelled {
				cancel()
			}
			call := toolgateway.ToolCall{Name: tt.call, Args: args}

			result, err := gw.Call(ctx, call)
			var suspended *toolgateway.Suspended
			if tt.answer != nil {
				require.ErrorAs(t, err, &suspended)
				require.Zero(t, read.Calls, "a call that waits for approval does not run")
				result, err = gw.Resume(ctx, suspended.CallID, call, *tt.answer)
			}

			if tt.wantSuspended {
				require.ErrorAs(t, err, &suspended)
				require.NotErrorIs(t, err, toolgateway.ErrDenied)
			} else if len(tt.wantErrIs) == 0 {
				require.NoError(t, err)
			}
			for _, target := range tt.wantErrIs {
				require.ErrorIs(t, err, target)
			}
			assert.Equal(t, tt.wantResult, result)
			assert.Equal(t, tt.wantCalls, read.Calls+del.Calls)
			assert.Equal(t, tt.wantRecords, gatewaytest.WithoutIDs(audit.Records))
			for _, r := range audit.Records {
				assert.Equal(t, gw.ID(), r.RunID, "records carry the run's ID")
				assert.Equal(t, audit.Records[0].CallID, r.CallID, "records of one call share a call ID")
				assert.NotEmpty(t, r.CallID)
			}
		})
	}
}
