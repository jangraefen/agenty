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
		{"missing policy", toolgateway.Config{MaxToolCalls: 100, Audit: &gatewaytest.Audit{}}, "policy is required"},
		{"zero tool call limit", toolgateway.Config{Policy: &gatewaytest.Policy{}, Audit: &gatewaytest.Audit{}}, "max tool calls must be greater than 0"},
		{"nil audit", toolgateway.Config{MaxToolCalls: 100, Policy: &gatewaytest.Policy{}, Tools: []toolgateway.Tool{read}}, "audit is required"},
		{"nil tool", toolgateway.Config{MaxToolCalls: 100, Policy: &gatewaytest.Policy{}, Tools: []toolgateway.Tool{nil}, Audit: &gatewaytest.Audit{}}, "tool 0 is nil"},
		{"unnamed tool", toolgateway.Config{MaxToolCalls: 100, Policy: &gatewaytest.Policy{}, Tools: []toolgateway.Tool{&gatewaytest.Tool{}}, Audit: &gatewaytest.Audit{}}, `tool 0: tool name "" must be`},
		{"duplicate tool", toolgateway.Config{MaxToolCalls: 100, Policy: &gatewaytest.Policy{}, Tools: []toolgateway.Tool{read, &gatewaytest.Tool{Name: "tickets_read"}}, Audit: &gatewaytest.Audit{}}, `duplicate tool "tickets_read"`},
		{"empty grant", toolgateway.Config{MaxToolCalls: 100, Policy: &gatewaytest.Policy{}, Granted: []string{""}, Audit: &gatewaytest.Audit{}}, `grant 0: tool name ""`},
		{"invalid grant", toolgateway.Config{MaxToolCalls: 100, Policy: &gatewaytest.Policy{}, Granted: []string{"tickets.read"}, Audit: &gatewaytest.Audit{}}, `grant 0: tool name "tickets.read"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gw, err := toolgateway.New(tt.cfg)
			require.Error(t, err)
			assert.Nil(t, gw)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestCall_PassesArgsAndReturnsResult(t *testing.T) {
	read := &gatewaytest.Tool{Name: "tickets_read", Result: json.RawMessage(`{"title":"Printer on fire"}`)}
	gw, err := toolgateway.New(toolgateway.Config{
		MaxToolCalls: 100,
		Policy:       &gatewaytest.Policy{},
		Granted:      []string{"tickets_read"},
		Tools:        []toolgateway.Tool{read},
		Audit:        &gatewaytest.Audit{},
	})
	require.NoError(t, err)
	run := gw.Start()

	result, err := run.Call(context.Background(), toolgateway.ToolCall{Name: "tickets_read", Args: json.RawMessage(`{"id":7}`)})

	require.NoError(t, err)
	assert.JSONEq(t, `{"title":"Printer on fire"}`, string(result))
	assert.JSONEq(t, `{"id":7}`, string(read.Args))
	assert.Equal(t, 1, read.Calls)
}

func TestCall_GrantsAreFixedAtConstruction(t *testing.T) {
	read := &gatewaytest.Tool{Name: "tickets_read"}
	del := &gatewaytest.Tool{Name: "tickets_delete"}
	granted := []string{"tickets_read"}
	gw, err := toolgateway.New(toolgateway.Config{
		MaxToolCalls: 100,
		Policy:       &gatewaytest.Policy{},
		Granted:      granted,
		Tools:        []toolgateway.Tool{read, del},
		Audit:        &gatewaytest.Audit{},
	})
	require.NoError(t, err)
	run := gw.Start()

	granted[0] = "tickets_delete"
	_, err = run.Call(context.Background(), toolgateway.ToolCall{Name: "tickets_delete"})

	require.ErrorIs(t, err, toolgateway.ErrDenied)
	assert.Zero(t, del.Calls)
}

func TestCall_EachCallGetsItsOwnCallID(t *testing.T) {
	audit := &gatewaytest.Audit{}
	gw, err := toolgateway.New(toolgateway.Config{
		MaxToolCalls: 100,
		Policy:       &gatewaytest.Policy{},
		Granted:      []string{"tickets_read"},
		Tools:        []toolgateway.Tool{&gatewaytest.Tool{Name: "tickets_read"}},
		Audit:        audit,
	})
	require.NoError(t, err)
	run := gw.Start()

	_, err = run.Call(context.Background(), toolgateway.ToolCall{Name: "tickets_read"})
	require.NoError(t, err)
	_, err = run.Call(context.Background(), toolgateway.ToolCall{Name: "tickets_read"})
	require.NoError(t, err)

	require.Len(t, audit.Records, 4)
	assert.NotEqual(t, audit.Records[0].CallID, audit.Records[2].CallID)
}

func TestDefinitions_OnlyGrantedAndResolvedToolsSortedByName(t *testing.T) {
	gw, err := toolgateway.New(toolgateway.Config{
		MaxToolCalls: 100,
		Policy:       &gatewaytest.Policy{},
		Granted:      []string{"tickets_read", "tickets_label", "tickets_ghost"},
		Tools: []toolgateway.Tool{
			&gatewaytest.Tool{Name: "tickets_read"},
			&gatewaytest.Tool{Name: "tickets_delete"},
			&gatewaytest.Tool{Name: "tickets_label"},
		},
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

func TestStart_EachRunHasItsOwnID(t *testing.T) {
	audit := &gatewaytest.Audit{}
	gw, err := toolgateway.New(toolgateway.Config{
		MaxToolCalls: 100,
		Policy:       &gatewaytest.Policy{},
		Granted:      []string{"tickets_read"},
		Tools:        []toolgateway.Tool{&gatewaytest.Tool{Name: "tickets_read"}},
		Audit:        audit,
	})
	require.NoError(t, err)
	first, second := gw.Start(), gw.Start()

	_, err = first.Call(context.Background(), toolgateway.ToolCall{Name: "tickets_read"})
	require.NoError(t, err)
	_, err = first.Call(context.Background(), toolgateway.ToolCall{Name: "tickets_delete"})
	require.ErrorIs(t, err, toolgateway.ErrDenied)
	_, err = second.Call(context.Background(), toolgateway.ToolCall{Name: "tickets_read"})
	require.NoError(t, err)

	require.NotEmpty(t, first.ID())
	require.NotEqual(t, first.ID(), second.ID())
	require.Len(t, audit.Records, 5)
	for i, r := range audit.Records {
		want := first.ID()
		if i >= 3 {
			want = second.ID()
		}
		assert.Equal(t, want, r.RunID, "record %d", i)
	}
}

func TestStart_EachRunHasItsOwnLimitAndCounts(t *testing.T) {
	policy := &gatewaytest.Policy{}
	read := &gatewaytest.Tool{Name: "tickets_read"}
	gw, err := toolgateway.New(toolgateway.Config{
		MaxToolCalls: 1,
		Policy:       policy,
		Granted:      []string{"tickets_read"},
		Tools:        []toolgateway.Tool{read},
		Audit:        &gatewaytest.Audit{},
	})
	require.NoError(t, err)
	first, second := gw.Start(), gw.Start()

	_, err = first.Call(context.Background(), toolgateway.ToolCall{Name: "tickets_read"})
	require.NoError(t, err)
	_, err = first.Call(context.Background(), toolgateway.ToolCall{Name: "tickets_read"})
	require.ErrorContains(t, err, "tool call limit reached")
	_, err = second.Call(context.Background(), toolgateway.ToolCall{Name: "tickets_read"})
	require.NoError(t, err, "the first run's calls do not count against the second")

	assert.Equal(t, 2, read.Calls)
	require.Len(t, policy.Inputs, 2)
	assert.Zero(t, policy.Inputs[1].Calls.Total, "the second run starts with no executed calls")
}

func TestNew_ValidatesTools(t *testing.T) {
	read := &gatewaytest.Tool{Name: "tickets_read"}
	tests := []struct {
		name    string
		tools   []toolgateway.Tool
		wantErr string
	}{
		{"valid", []toolgateway.Tool{read, &gatewaytest.Tool{Name: "tickets_label"}}, ""},
		{"none", nil, ""},
		{"nil tool", []toolgateway.Tool{read, nil}, "tool 1 is nil"},
		{"unnamed tool", []toolgateway.Tool{&gatewaytest.Tool{}}, `tool 0: tool name "" must be`},
		{"tool name models cannot use", []toolgateway.Tool{&gatewaytest.Tool{Name: "tickets.read"}}, `tool 0: tool name "tickets.read"`},
		{"duplicate tool", []toolgateway.Tool{read, &gatewaytest.Tool{Name: "tickets_read"}}, `duplicate tool "tickets_read"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := toolgateway.New(toolgateway.Config{Tools: tt.tools, MaxToolCalls: 1, Policy: &gatewaytest.Policy{}, Audit: &gatewaytest.Audit{}})
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
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
