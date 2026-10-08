package toolgateway_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/toolgateway"
	"github.com/jangraefen/agenty/internal/toolgateway/gatewaytest"
)

func TestNew_RejectsInvalidConfig(t *testing.T) {
	read := &gatewaytest.Tool{Name: "tickets_read"}
	tests := []struct {
		name    string
		cfg     toolgateway.Config
		wantErr string
	}{
		{"missing run ID", toolgateway.Config{MaxToolCalls: 100, Policy: &gatewaytest.Policy{}, Audit: &gatewaytest.Audit{}, Redactor: gatewaytest.NoSecrets}, "run ID is required"},
		{"missing policy", toolgateway.Config{RunID: "r1", MaxToolCalls: 100, Audit: &gatewaytest.Audit{}, Redactor: gatewaytest.NoSecrets}, "policy is required"},
		{"missing redactor", toolgateway.Config{RunID: "r1", MaxToolCalls: 100, Policy: &gatewaytest.Policy{}, Audit: &gatewaytest.Audit{}}, "redactor is required"},
		{"zero tool call limit", toolgateway.Config{RunID: "r1", Policy: &gatewaytest.Policy{}, Audit: &gatewaytest.Audit{}, Redactor: gatewaytest.NoSecrets}, "max tool calls must be greater than 0"},
		{"nil audit", toolgateway.Config{RunID: "r1", MaxToolCalls: 100, Policy: &gatewaytest.Policy{}, Servers: gatewaytest.Servers(read), Redactor: gatewaytest.NoSecrets}, "audit is required"},
		{"empty grant", toolgateway.Config{RunID: "r1", MaxToolCalls: 100, Policy: &gatewaytest.Policy{}, Granted: []string{""}, Audit: &gatewaytest.Audit{}, Redactor: gatewaytest.NoSecrets}, `grant 0: tool name ""`},
		{"invalid grant", toolgateway.Config{RunID: "r1", MaxToolCalls: 100, Policy: &gatewaytest.Policy{}, Granted: []string{"tickets.read"}, Audit: &gatewaytest.Audit{}, Redactor: gatewaytest.NoSecrets}, `grant 0: tool name "tickets.read"`},
		{"grant of an unconfigured server", toolgateway.Config{RunID: "r1", MaxToolCalls: 100, Policy: &gatewaytest.Policy{}, Granted: []string{"tickets_read"}, Audit: &gatewaytest.Audit{}, Redactor: gatewaytest.NoSecrets}, `grant tickets_read: no server "tickets" is configured`},
		{"grant without a server part", toolgateway.Config{RunID: "r1", MaxToolCalls: 100, Policy: &gatewaytest.Policy{}, Granted: []string{"read"}, Servers: gatewaytest.Servers(read), Audit: &gatewaytest.Audit{}, Redactor: gatewaytest.NoSecrets}, `grant read: no server "read" is configured`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gw, err := toolgateway.New(context.Background(), tt.cfg)
			require.Error(t, err)
			assert.Nil(t, gw)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestCall_PassesArgsAndReturnsResult(t *testing.T) {
	read := &gatewaytest.Tool{Name: "tickets_read", Result: json.RawMessage(`{"title":"Printer on fire"}`)}
	gw, err := toolgateway.New(context.Background(), toolgateway.Config{
		RunID:        "r1",
		Redactor:     gatewaytest.NoSecrets,
		MaxToolCalls: 100,
		Policy:       &gatewaytest.Policy{},
		Granted:      []string{"tickets_read"},
		Servers:      gatewaytest.Servers(read),
		Audit:        &gatewaytest.Audit{},
	})
	require.NoError(t, err)

	result, err := gw.Call(context.Background(), toolgateway.ToolCall{Name: "tickets_read", Args: json.RawMessage(`{"id":7}`)})

	require.NoError(t, err)
	assert.JSONEq(t, `{"title":"Printer on fire"}`, string(result))
	assert.JSONEq(t, `{"id":7}`, string(read.Args))
	assert.Equal(t, 1, read.Calls)
}

func TestCall_GrantsAreFixedAtConstruction(t *testing.T) {
	read := &gatewaytest.Tool{Name: "tickets_read"}
	del := &gatewaytest.Tool{Name: "tickets_delete"}
	granted := []string{"tickets_read"}
	gw, err := toolgateway.New(context.Background(), toolgateway.Config{
		RunID:        "r1",
		Redactor:     gatewaytest.NoSecrets,
		MaxToolCalls: 100,
		Policy:       &gatewaytest.Policy{},
		Granted:      granted,
		Servers:      gatewaytest.Servers(read, del),
		Audit:        &gatewaytest.Audit{},
	})
	require.NoError(t, err)

	granted[0] = "tickets_delete"
	_, err = gw.Call(context.Background(), toolgateway.ToolCall{Name: "tickets_delete"})

	require.ErrorIs(t, err, toolgateway.ErrDenied)
	assert.Zero(t, del.Calls)
}

func TestCall_EachCallGetsItsOwnCallID(t *testing.T) {
	audit := &gatewaytest.Audit{}
	gw, err := toolgateway.New(context.Background(), toolgateway.Config{
		RunID:        "r1",
		Redactor:     gatewaytest.NoSecrets,
		MaxToolCalls: 100,
		Policy:       &gatewaytest.Policy{},
		Granted:      []string{"tickets_read"},
		Servers:      gatewaytest.Servers(&gatewaytest.Tool{Name: "tickets_read"}),
		Audit:        audit,
	})
	require.NoError(t, err)

	_, err = gw.Call(context.Background(), toolgateway.ToolCall{Name: "tickets_read"})
	require.NoError(t, err)
	_, err = gw.Call(context.Background(), toolgateway.ToolCall{Name: "tickets_read"})
	require.NoError(t, err)

	require.Len(t, audit.Records, 4)
	assert.NotEqual(t, audit.Records[0].CallID, audit.Records[2].CallID)
}

func TestDefinitions_OnlyGrantedToolsSortedByName(t *testing.T) {
	gw, err := toolgateway.New(context.Background(), toolgateway.Config{
		RunID:        "r1",
		Redactor:     gatewaytest.NoSecrets,
		MaxToolCalls: 100,
		Policy:       &gatewaytest.Policy{},
		Granted:      []string{"tickets_read", "tickets_label"},
		Servers: gatewaytest.Servers(
			&gatewaytest.Tool{Name: "tickets_read"},
			&gatewaytest.Tool{Name: "tickets_delete"},
			&gatewaytest.Tool{Name: "tickets_label"},
		),
		Audit: &gatewaytest.Audit{},
	})
	require.NoError(t, err)

	defs := gw.Definitions()

	require.Len(t, defs, 2)
	assert.Equal(t, "tickets_label", defs[0].Name)
	assert.Equal(t, "tickets_read", defs[1].Name)
	assert.Equal(t, "fake tickets_read", defs[1].Description)
	assert.JSONEq(t, `{"type":"object"}`, string(defs[1].InputSchema))
}

// TestNew_RunsUnderTheGivenID: a run stored before it executes, such as a
// queued one, keeps its stored ID in the gateway, and starts with no calls.
func TestNew_RunsUnderTheGivenID(t *testing.T) {
	audit := &gatewaytest.Audit{}
	policy := &gatewaytest.Policy{}
	gw, err := toolgateway.New(context.Background(), toolgateway.Config{
		RunID:        "r1",
		Redactor:     gatewaytest.NoSecrets,
		MaxToolCalls: 100,
		Policy:       policy,
		Granted:      []string{"tickets_read"},
		Servers:      gatewaytest.Servers(&gatewaytest.Tool{Name: "tickets_read"}),
		Audit:        audit,
	})
	require.NoError(t, err)

	_, err = gw.Call(context.Background(), toolgateway.ToolCall{Name: "tickets_read"})

	require.NoError(t, err)
	assert.Equal(t, "r1", gw.ID())
	require.NotEmpty(t, audit.Records)
	for _, r := range audit.Records {
		assert.Equal(t, "r1", r.RunID)
	}
	require.Len(t, policy.Inputs, 1)
	assert.Equal(t, "r1", policy.Inputs[0].RunID)
	assert.Zero(t, policy.Inputs[0].Calls.Total)
}

// TestNew_GatewaysNeverShareCounts: each run has a gateway of its own, so one
// run's calls cannot affect another's limit or policy input, even when both
// call the same tool.
func TestNew_GatewaysNeverShareCounts(t *testing.T) {
	policy := &gatewaytest.Policy{}
	read := &gatewaytest.Tool{Name: "tickets_read"}
	newGateway := func(id string) *toolgateway.Gateway {
		gw, err := toolgateway.New(context.Background(), toolgateway.Config{
			RunID:        id,
			Redactor:     gatewaytest.NoSecrets,
			MaxToolCalls: 1,
			Policy:       policy,
			Granted:      []string{"tickets_read"},
			Servers:      gatewaytest.Servers(read),
			Audit:        &gatewaytest.Audit{},
		})
		require.NoError(t, err)
		return gw
	}
	first, second := newGateway("r1"), newGateway("r2")

	_, err := first.Call(context.Background(), toolgateway.ToolCall{Name: "tickets_read"})
	require.NoError(t, err)
	_, err = first.Call(context.Background(), toolgateway.ToolCall{Name: "tickets_read"})
	require.ErrorContains(t, err, "tool call limit reached")
	_, err = second.Call(context.Background(), toolgateway.ToolCall{Name: "tickets_read"})
	require.NoError(t, err, "the first run's calls do not count against the second")

	assert.Equal(t, 2, read.Calls)
	require.Len(t, policy.Inputs, 2)
	assert.Zero(t, policy.Inputs[1].Calls.Total, "the second run starts with no executed calls")
}

func TestNew_ValidatesServerTools(t *testing.T) {
	read := &gatewaytest.Tool{Name: "tickets_read"}
	tests := []struct {
		name    string
		tools   []toolgateway.Tool
		wantErr string
	}{
		{"valid", []toolgateway.Tool{read, &gatewaytest.Tool{Name: "tickets_label"}}, ""},
		{"nil tool", []toolgateway.Tool{read, nil}, `tool "<nil>" is not named tickets_<tool>`},
		{"unnamed tool", []toolgateway.Tool{read, &gatewaytest.Tool{}}, `tool "" is not named tickets_<tool>`},
		{"tool name models cannot use", []toolgateway.Tool{read, &gatewaytest.Tool{Name: "tickets_re.ad"}}, `tool name "tickets_re.ad" must be`},
		{"duplicate tool", []toolgateway.Tool{read, &gatewaytest.Tool{Name: "tickets_read"}}, `duplicate tool "tickets_read"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tickets := &gatewaytest.Server{Tools: tt.tools}
			gw, err := toolgateway.New(context.Background(), toolgateway.Config{
				RunID:        "r1",
				Redactor:     gatewaytest.NoSecrets,
				Granted:      []string{"tickets_read"},
				Servers:      map[string]toolgateway.ToolServer{"tickets": tickets},
				MaxToolCalls: 1,
				Policy:       &gatewaytest.Policy{},
				Audit:        &gatewaytest.Audit{},
			})
			if tt.wantErr == "" {
				require.NoError(t, err)
				assert.NoError(t, gw.Close())
				return
			}
			require.ErrorContains(t, err, "server tickets: "+tt.wantErr)
			assert.Equal(t, 1, tickets.Closed, "a server with invalid tools is stopped")
		})
	}
}

func TestNew_FailsOnGrantsNoServerServes(t *testing.T) {
	tickets := &gatewaytest.Server{Tools: []toolgateway.Tool{&gatewaytest.Tool{Name: "tickets_read"}}}

	gw, err := toolgateway.New(context.Background(), toolgateway.Config{

		RunID:        "r1",
		Redactor:     gatewaytest.NoSecrets,
		Granted:      []string{"tickets_read", "tickets_ghost"},
		Servers:      map[string]toolgateway.ToolServer{"tickets": tickets},
		MaxToolCalls: 1,
		Policy:       &gatewaytest.Policy{},
		Audit:        &gatewaytest.Audit{},
	})

	require.ErrorContains(t, err, "grant tickets_ghost: server tickets has no such tool")
	assert.Nil(t, gw)
	assert.Equal(t, 1, tickets.Closed, "the started server is stopped")
}

func TestValidateToolName(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{"tickets_read", true},
		{"a", true},
		{"Tickets-Read_2", true},
		{strings.Repeat("a", 64), true},
		{"", false},
		{"tickets.read", false},
		{"tickets read", false},
		{"tickets/read", false},
		{"tickets_läs", false},
		{strings.Repeat("a", 65), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := toolgateway.ValidateToolName(tt.name)
			if tt.valid {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, "must be 1 to 64 letters, digits, underscores or hyphens")
		})
	}
}

func TestRecord_JSONIsTheAuditFormat(t *testing.T) {
	rec := toolgateway.Record{
		RunID: "r1", CallID: "c1", Event: toolgateway.EventResult, Tool: "tickets_read",
		Args: json.RawMessage(`{"id":7}`), Decision: toolgateway.Allow, Reason: "ok", Approver: "alice",
		Result: json.RawMessage(`{"title":"x"}`), Err: "boom",
	}

	b, err := json.Marshal(rec)

	require.NoError(t, err)
	assert.JSONEq(t, `{"run_id":"r1","call_id":"c1","event":"result","tool":"tickets_read","args":{"id":7},"decision":"allow","reason":"ok","approver":"alice","result":{"title":"x"},"error":"boom"}`, string(b))
	minimal, err := json.Marshal(toolgateway.Record{RunID: "r1", CallID: "c1", Event: toolgateway.EventDecision, Tool: "t", Decision: toolgateway.Deny})
	require.NoError(t, err)
	assert.JSONEq(t, `{"run_id":"r1","call_id":"c1","event":"decision","tool":"t","decision":"deny"}`, string(minimal), "empty optional fields are left out")
}

// TestCall_CallsAfterCloseAreDenied: once the gateway has stopped its
// servers, which a pool may keep running for a later run, no call of this
// run reaches them.
func TestCall_CallsAfterCloseAreDenied(t *testing.T) {
	read := &gatewaytest.Tool{Name: "tickets_read"}
	audit := &gatewaytest.Audit{}
	gw, err := toolgateway.New(context.Background(), toolgateway.Config{
		RunID:        "r1",
		Harness:      "triage",
		Granted:      []string{"tickets_read"},
		Servers:      gatewaytest.Servers(read),
		MaxToolCalls: 5,
		Policy:       &gatewaytest.Policy{},
		Audit:        audit,
		Redactor:     gatewaytest.NoSecrets,
	})
	require.NoError(t, err)
	require.NoError(t, gw.Close())

	_, err = gw.Call(context.Background(), toolgateway.ToolCall{Name: "tickets_read"})

	require.ErrorIs(t, err, toolgateway.ErrDenied)
	assert.Zero(t, read.Calls)
	require.Len(t, audit.Records, 1, "the denial is recorded")
	assert.Equal(t, toolgateway.Deny, audit.Records[0].Decision)
}
