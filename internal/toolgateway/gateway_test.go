package toolgateway_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/toolgateway"
)

func TestNew_RejectsInvalidConfig(t *testing.T) {
	read := &fakeTool{name: "tickets.read"}
	tests := []struct {
		name    string
		cfg     toolgateway.Config
		wantErr string
	}{
		{"nil audit", toolgateway.Config{Tools: []toolgateway.Tool{read}}, "audit is required"},
		{"nil tool", toolgateway.Config{Tools: []toolgateway.Tool{nil}, Audit: &recordingAudit{}}, "tool 0 is nil"},
		{"unnamed tool", toolgateway.Config{Tools: []toolgateway.Tool{&fakeTool{}}, Audit: &recordingAudit{}}, "tool 0 has no name"},
		{"duplicate tool", toolgateway.Config{Tools: []toolgateway.Tool{read, &fakeTool{name: "tickets.read"}}, Audit: &recordingAudit{}}, `duplicate tool "tickets.read"`},
		{"empty grant", toolgateway.Config{Granted: []string{""}, Audit: &recordingAudit{}}, "grant 0 is empty"},
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
	read := &fakeTool{name: "tickets.read", result: json.RawMessage(`{"title":"Printer on fire"}`)}
	gw, err := toolgateway.New(toolgateway.Config{
		Granted: []string{"tickets.read"},
		Tools:   []toolgateway.Tool{read},
		Audit:   &recordingAudit{},
	})
	require.NoError(t, err)

	result, err := gw.Call(context.Background(), toolgateway.ToolCall{Name: "tickets.read", Args: json.RawMessage(`{"id":7}`)})

	require.NoError(t, err)
	assert.JSONEq(t, `{"title":"Printer on fire"}`, string(result))
	assert.JSONEq(t, `{"id":7}`, string(read.gotArgs))
	assert.Equal(t, 1, read.calls)
}

func TestCall_GrantsAreFixedAtConstruction(t *testing.T) {
	read := &fakeTool{name: "tickets.read"}
	del := &fakeTool{name: "tickets.delete"}
	granted := []string{"tickets.read"}
	gw, err := toolgateway.New(toolgateway.Config{
		Granted: granted,
		Tools:   []toolgateway.Tool{read, del},
		Audit:   &recordingAudit{},
	})
	require.NoError(t, err)

	granted[0] = "tickets.delete"
	_, err = gw.Call(context.Background(), toolgateway.ToolCall{Name: "tickets.delete"})

	assert.ErrorIs(t, err, toolgateway.ErrDenied)
	assert.Zero(t, del.calls)
}

func TestCall_EachCallGetsItsOwnCallID(t *testing.T) {
	audit := &recordingAudit{}
	gw, err := toolgateway.New(toolgateway.Config{
		Granted: []string{"tickets.read"},
		Tools:   []toolgateway.Tool{&fakeTool{name: "tickets.read"}},
		Audit:   audit,
	})
	require.NoError(t, err)

	_, err = gw.Call(context.Background(), toolgateway.ToolCall{Name: "tickets.read"})
	require.NoError(t, err)
	_, err = gw.Call(context.Background(), toolgateway.ToolCall{Name: "tickets.read"})
	require.NoError(t, err)

	require.Len(t, audit.records, 4)
	assert.NotEqual(t, audit.records[0].CallID, audit.records[2].CallID)
}
