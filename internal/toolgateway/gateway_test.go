package toolgateway_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/toolgateway"
	"github.com/jangraefen/agenty/internal/toolgateway/gatewaytest"
)

func TestNew_RejectsInvalidConfig(t *testing.T) {
	read := &gatewaytest.Tool{Name: "tickets.read"}
	tests := []struct {
		name    string
		cfg     toolgateway.Config
		wantErr string
	}{
		{"missing run id", toolgateway.Config{Audit: &gatewaytest.Audit{}}, "run id is required"},
		{"nil audit", toolgateway.Config{RunID: runID, Tools: []toolgateway.Tool{read}}, "audit is required"},
		{"nil tool", toolgateway.Config{RunID: runID, Tools: []toolgateway.Tool{nil}, Audit: &gatewaytest.Audit{}}, "tool 0 is nil"},
		{"unnamed tool", toolgateway.Config{RunID: runID, Tools: []toolgateway.Tool{&gatewaytest.Tool{}}, Audit: &gatewaytest.Audit{}}, "tool 0 has no name"},
		{"duplicate tool", toolgateway.Config{RunID: runID, Tools: []toolgateway.Tool{read, &gatewaytest.Tool{Name: "tickets.read"}}, Audit: &gatewaytest.Audit{}}, `duplicate tool "tickets.read"`},
		{"empty grant", toolgateway.Config{RunID: runID, Granted: []string{""}, Audit: &gatewaytest.Audit{}}, "grant 0 is empty"},
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
	read := &gatewaytest.Tool{Name: "tickets.read", Result: json.RawMessage(`{"title":"Printer on fire"}`)}
	gw, err := toolgateway.New(toolgateway.Config{
		RunID:   runID,
		Granted: []string{"tickets.read"},
		Tools:   []toolgateway.Tool{read},
		Audit:   &gatewaytest.Audit{},
	})
	require.NoError(t, err)

	result, err := gw.Call(context.Background(), toolgateway.ToolCall{Name: "tickets.read", Args: json.RawMessage(`{"id":7}`)})

	require.NoError(t, err)
	assert.JSONEq(t, `{"title":"Printer on fire"}`, string(result))
	assert.JSONEq(t, `{"id":7}`, string(read.Args))
	assert.Equal(t, 1, read.Calls)
}

func TestCall_GrantsAreFixedAtConstruction(t *testing.T) {
	read := &gatewaytest.Tool{Name: "tickets.read"}
	del := &gatewaytest.Tool{Name: "tickets.delete"}
	granted := []string{"tickets.read"}
	gw, err := toolgateway.New(toolgateway.Config{
		RunID:   runID,
		Granted: granted,
		Tools:   []toolgateway.Tool{read, del},
		Audit:   &gatewaytest.Audit{},
	})
	require.NoError(t, err)

	granted[0] = "tickets.delete"
	_, err = gw.Call(context.Background(), toolgateway.ToolCall{Name: "tickets.delete"})

	assert.ErrorIs(t, err, toolgateway.ErrDenied)
	assert.Zero(t, del.Calls)
}

func TestCall_EachCallGetsItsOwnCallID(t *testing.T) {
	audit := &gatewaytest.Audit{}
	gw, err := toolgateway.New(toolgateway.Config{
		RunID:   runID,
		Granted: []string{"tickets.read"},
		Tools:   []toolgateway.Tool{&gatewaytest.Tool{Name: "tickets.read"}},
		Audit:   audit,
	})
	require.NoError(t, err)

	_, err = gw.Call(context.Background(), toolgateway.ToolCall{Name: "tickets.read"})
	require.NoError(t, err)
	_, err = gw.Call(context.Background(), toolgateway.ToolCall{Name: "tickets.read"})
	require.NoError(t, err)

	require.Len(t, audit.Records, 4)
	assert.NotEqual(t, audit.Records[0].CallID, audit.Records[2].CallID)
}

func TestDefinitions_OnlyGrantedAndResolvedToolsSortedByName(t *testing.T) {
	gw, err := toolgateway.New(toolgateway.Config{
		RunID:   runID,
		Granted: []string{"tickets.read", "tickets.label", "tickets.ghost"},
		Tools: []toolgateway.Tool{
			&gatewaytest.Tool{Name: "tickets.read"},
			&gatewaytest.Tool{Name: "tickets.delete"},
			&gatewaytest.Tool{Name: "tickets.label"},
		},
		Audit: &gatewaytest.Audit{},
	})
	require.NoError(t, err)

	defs := gw.Definitions()

	require.Len(t, defs, 2)
	assert.Equal(t, "tickets.label", defs[0].Name)
	assert.Equal(t, "tickets.read", defs[1].Name)
	assert.Equal(t, "fake tickets.read", defs[1].Description)
	assert.JSONEq(t, `{"type":"object"}`, string(defs[1].InputSchema))
}

func TestCall_RecordsCarryTheRunID(t *testing.T) {
	audit := &gatewaytest.Audit{}
	gw, err := toolgateway.New(toolgateway.Config{
		RunID:   "run-42",
		Granted: []string{"tickets.read"},
		Tools:   []toolgateway.Tool{&gatewaytest.Tool{Name: "tickets.read"}},
		Audit:   audit,
	})
	require.NoError(t, err)

	_, err = gw.Call(context.Background(), toolgateway.ToolCall{Name: "tickets.read"})
	require.NoError(t, err)
	_, err = gw.Call(context.Background(), toolgateway.ToolCall{Name: "tickets.delete"})
	require.ErrorIs(t, err, toolgateway.ErrDenied)

	require.Len(t, audit.Records, 3)
	for _, r := range audit.Records {
		assert.Equal(t, "run-42", r.RunID)
	}
}
