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
// a tool not granted to the harness is denied and never executed.
func TestInvariant_DefaultDeny(t *testing.T) {
	tests := []struct {
		name    string
		granted []string
		call    string
		reason  string
	}{
		{"ungranted tool", []string{"tickets.read"}, "tickets.delete", "tool not granted"},
		{"no grants at all", nil, "tickets.read", "tool not granted"},
		{"grant match is case-sensitive", []string{"tickets.read"}, "Tickets.Read", "tool not granted"},
		{"empty tool name", []string{"tickets.read"}, "", "tool not granted"},
		{"granted but not resolvable", []string{"tickets.read", "tickets.ghost"}, "tickets.ghost", "tool not resolved"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			read := &gatewaytest.Tool{Name: "tickets.read", Result: json.RawMessage(`{}`)}
			del := &gatewaytest.Tool{Name: "tickets.delete", Result: json.RawMessage(`{}`)}
			audit := &gatewaytest.Audit{}
			gw, err := toolgateway.New(toolgateway.Config{
				RunID:   runID,
				Granted: tt.granted,
				Tools:   []toolgateway.Tool{read, del},
				Audit:   audit,
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
}

// TestInvariant_EveryCallAudited guards trust-model guarantee 6:
// every tool call is recorded with its decision and result, and a call
// whose decision cannot be recorded is not executed.
func TestInvariant_EveryCallAudited(t *testing.T) {
	args := json.RawMessage(`{"id":42}`)
	toolErr := errors.New("ticket system unavailable")

	tests := []struct {
		name        string
		call        string
		toolErr     error
		failOn      toolgateway.Event
		wantCalls   int
		wantResult  json.RawMessage
		wantErrIs   []error
		wantRecords []toolgateway.Record
	}{
		{
			name:       "allowed call records decision and result",
			call:       "tickets.read",
			wantCalls:  1,
			wantResult: json.RawMessage(`{"ok":true}`),
			wantRecords: []toolgateway.Record{
				{RunID: runID, Event: toolgateway.EventDecision, Tool: "tickets.read", Args: args, Decision: toolgateway.Allow},
				{RunID: runID, Event: toolgateway.EventResult, Tool: "tickets.read", Args: args, Decision: toolgateway.Allow, Result: json.RawMessage(`{"ok":true}`)},
			},
		},
		{
			name:      "denied call records the denial",
			call:      "tickets.delete",
			wantErrIs: []error{toolgateway.ErrDenied},
			wantRecords: []toolgateway.Record{
				{RunID: runID, Event: toolgateway.EventDecision, Tool: "tickets.delete", Args: args, Decision: toolgateway.Deny, Reason: "tool not granted"},
			},
		},
		{
			name:      "failing tool records the error",
			call:      "tickets.read",
			toolErr:   toolErr,
			wantCalls: 1,
			wantErrIs: []error{toolErr},
			wantRecords: []toolgateway.Record{
				{RunID: runID, Event: toolgateway.EventDecision, Tool: "tickets.read", Args: args, Decision: toolgateway.Allow},
				{RunID: runID, Event: toolgateway.EventResult, Tool: "tickets.read", Args: args, Decision: toolgateway.Allow, Err: "ticket system unavailable"},
			},
		},
		{
			name:      "unrecorded decision blocks execution",
			call:      "tickets.read",
			failOn:    toolgateway.EventDecision,
			wantErrIs: []error{toolgateway.ErrAudit, gatewaytest.ErrAuditDown},
		},
		{
			name:      "unrecorded denial is still a denial",
			call:      "tickets.delete",
			failOn:    toolgateway.EventDecision,
			wantErrIs: []error{toolgateway.ErrDenied, toolgateway.ErrAudit, gatewaytest.ErrAuditDown},
		},
		{
			name:       "unrecorded result is reported with the result",
			call:       "tickets.read",
			failOn:     toolgateway.EventResult,
			wantCalls:  1,
			wantResult: json.RawMessage(`{"ok":true}`),
			wantErrIs:  []error{toolgateway.ErrAudit, gatewaytest.ErrAuditDown},
			wantRecords: []toolgateway.Record{
				{RunID: runID, Event: toolgateway.EventDecision, Tool: "tickets.read", Args: args, Decision: toolgateway.Allow},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			read := &gatewaytest.Tool{Name: "tickets.read", Result: json.RawMessage(`{"ok":true}`), Err: tt.toolErr}
			if tt.toolErr != nil {
				read.Result = nil
			}
			del := &gatewaytest.Tool{Name: "tickets.delete"}
			audit := &gatewaytest.Audit{FailOn: tt.failOn}
			gw, err := toolgateway.New(toolgateway.Config{
				RunID:   runID,
				Granted: []string{"tickets.read"},
				Tools:   []toolgateway.Tool{read, del},
				Audit:   audit,
			})
			require.NoError(t, err)

			result, err := gw.Call(context.Background(), toolgateway.ToolCall{Name: tt.call, Args: args})

			if len(tt.wantErrIs) == 0 {
				require.NoError(t, err)
			}
			for _, target := range tt.wantErrIs {
				require.ErrorIs(t, err, target)
			}
			assert.Equal(t, tt.wantResult, result)
			assert.Equal(t, tt.wantCalls, read.Calls+del.Calls)
			assert.Equal(t, tt.wantRecords, gatewaytest.WithoutCallIDs(audit.Records))
			for _, r := range audit.Records {
				assert.Equal(t, audit.Records[0].CallID, r.CallID, "records of one call share a call ID")
				assert.NotEmpty(t, r.CallID)
			}
		})
	}
}
