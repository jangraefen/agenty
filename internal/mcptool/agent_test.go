package mcptool_test

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/agent"
	"github.com/jangraefen/agenty/internal/harness"
	"github.com/jangraefen/agenty/internal/mcptool"
	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/model/modeltest"
	"github.com/jangraefen/agenty/internal/toolgateway"
	"github.com/jangraefen/agenty/internal/toolgateway/gatewaytest"
)

// TestAgent_RunsMCPToolsThroughTheGateway runs the agent loop against the
// test MCP server, which the gateway starts and stops: granted tools reach the server, ungranted tools on the
// same server never do, and both are audited.
func TestAgent_RunsMCPToolsThroughTheGateway(t *testing.T) {
	log := &callLog{}
	clientT, serverT := mcp.NewInMemoryTransports()
	ss, err := newTestServer(log).Connect(t.Context(), serverT, nil)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, ss.Close()) })
	audit := &gatewaytest.Audit{}
	m := modeltest.NewScripted(
		modeltest.CallTools(
			model.ToolCall{ID: "c1", Name: "test_echo", Args: json.RawMessage(`{"id":7}`)},
			model.ToolCall{ID: "c2", Name: "test_mixed"},
			model.ToolCall{ID: "c3", Name: "test_fail"},
		),
		modeltest.Reply("done"),
	)
	a, err := agent.New(t.Context(), agent.Config{
		Harness: &harness.Harness{
			Name:         "triage",
			Instructions: "Triage the ticket.",
			Model:        harness.Model{Provider: "scripted", Name: "scripted"},
			Tools:        []string{"test_echo", "test_fail"},
			Limits:       harness.Limits{MaxSteps: 3, MaxToolCalls: 10},
		},
		Model:   m,
		Servers: map[string]toolgateway.ToolServer{"test": mcptool.TransportServer{Transport: clientT}},
		Audit:   audit,
	})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, a.Close()) })

	res, err := a.Run(t.Context(), "ticket 7")
	require.NoError(t, err)

	assert.Equal(t, []string{"echo", "fail"}, log.calls(), "the ungranted tool never reached the server")
	names := []string{}
	for _, d := range m.Requests()[0].Tools {
		names = append(names, d.Name)
	}
	assert.Equal(t, []string{"test_echo", "test_fail"}, names, "the model is offered granted tools only")

	results := res.Messages[2].ToolResults
	require.Len(t, results, 3)
	assert.JSONEq(t, `"{\"id\":7}"`, results[0].Content)
	assert.True(t, results[1].IsError)
	assert.Contains(t, results[1].Content, "tool not granted")
	assert.True(t, results[2].IsError)
	assert.Contains(t, results[2].Content, "ticket system unavailable")

	var decisions []toolgateway.Decision
	for _, r := range audit.Records {
		if r.Event == toolgateway.EventDecision {
			decisions = append(decisions, r.Decision)
		}
	}
	assert.Equal(t, []toolgateway.Decision{toolgateway.Allow, toolgateway.Deny, toolgateway.Allow}, decisions)
}
