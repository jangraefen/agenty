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
		{"missing run id", toolgateway.Config{Audit: &gatewaytest.Audit{}}, "run id is required"},
		{"missing policy", toolgateway.Config{RunID: runID, MaxToolCalls: 100, Audit: &gatewaytest.Audit{}}, "policy is required"},
		{"zero tool call limit", toolgateway.Config{RunID: runID, Policy: &gatewaytest.Policy{}, Audit: &gatewaytest.Audit{}}, "max tool calls must be greater than 0"},
		{"nil audit", toolgateway.Config{RunID: runID, MaxToolCalls: 100, Policy: &gatewaytest.Policy{}, Tools: []toolgateway.Tool{read}}, "audit is required"},
		{"nil tool", toolgateway.Config{RunID: runID, MaxToolCalls: 100, Policy: &gatewaytest.Policy{}, Tools: []toolgateway.Tool{nil}, Audit: &gatewaytest.Audit{}}, "tool 0 is nil"},
		{"unnamed tool", toolgateway.Config{RunID: runID, MaxToolCalls: 100, Policy: &gatewaytest.Policy{}, Tools: []toolgateway.Tool{&gatewaytest.Tool{}}, Audit: &gatewaytest.Audit{}}, `tool 0: tool name "" must be`},
		{"duplicate tool", toolgateway.Config{RunID: runID, MaxToolCalls: 100, Policy: &gatewaytest.Policy{}, Tools: []toolgateway.Tool{read, &gatewaytest.Tool{Name: "tickets_read"}}, Audit: &gatewaytest.Audit{}}, `duplicate tool "tickets_read"`},
		{"empty grant", toolgateway.Config{RunID: runID, MaxToolCalls: 100, Policy: &gatewaytest.Policy{}, Granted: []string{""}, Audit: &gatewaytest.Audit{}}, `grant 0: tool name ""`},
		{"invalid grant", toolgateway.Config{RunID: runID, MaxToolCalls: 100, Policy: &gatewaytest.Policy{}, Granted: []string{"tickets.read"}, Audit: &gatewaytest.Audit{}}, `grant 0: tool name "tickets.read"`},
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
		RunID:        runID,
		MaxToolCalls: 100,
		Policy:       &gatewaytest.Policy{},
		Granted:      []string{"tickets_read"},
		Tools:        []toolgateway.Tool{read},
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
	gw, err := toolgateway.New(toolgateway.Config{
		RunID:        runID,
		MaxToolCalls: 100,
		Policy:       &gatewaytest.Policy{},
		Granted:      granted,
		Tools:        []toolgateway.Tool{read, del},
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
	gw, err := toolgateway.New(toolgateway.Config{
		RunID:        runID,
		MaxToolCalls: 100,
		Policy:       &gatewaytest.Policy{},
		Granted:      []string{"tickets_read"},
		Tools:        []toolgateway.Tool{&gatewaytest.Tool{Name: "tickets_read"}},
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

func TestDefinitions_OnlyGrantedAndResolvedToolsSortedByName(t *testing.T) {
	gw, err := toolgateway.New(toolgateway.Config{
		RunID:        runID,
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

func TestCall_RecordsCarryTheRunID(t *testing.T) {
	audit := &gatewaytest.Audit{}
	gw, err := toolgateway.New(toolgateway.Config{
		RunID:        "run-42",
		MaxToolCalls: 100,
		Policy:       &gatewaytest.Policy{},
		Granted:      []string{"tickets_read"},
		Tools:        []toolgateway.Tool{&gatewaytest.Tool{Name: "tickets_read"}},
		Audit:        audit,
	})
	require.NoError(t, err)

	_, err = gw.Call(context.Background(), toolgateway.ToolCall{Name: "tickets_read"})
	require.NoError(t, err)
	_, err = gw.Call(context.Background(), toolgateway.ToolCall{Name: "tickets_delete"})
	require.ErrorIs(t, err, toolgateway.ErrDenied)

	require.Len(t, audit.Records, 3)
	for _, r := range audit.Records {
		assert.Equal(t, "run-42", r.RunID)
	}
}

func TestValidateTools(t *testing.T) {
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
			err := toolgateway.ValidateTools(tt.tools)
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
