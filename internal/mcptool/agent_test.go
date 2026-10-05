package mcptool_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/agent"
	"github.com/jangraefen/agenty/internal/harness"
	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/toolgateway"
	"github.com/jangraefen/agenty/internal/toolgateway/gatewaytest"
)

// TestAgent_RunsMCPToolsThroughTheGateway runs the agent loop against the
// test MCP server: granted tools reach the server, ungranted tools on the
// same server never do, and both are audited.
func TestAgent_RunsMCPToolsThroughTheGateway(t *testing.T) {
	s, _, log := connectInMemory(t)
	tools, err := s.Tools(t.Context())
	require.NoError(t, err)
	audit := &gatewaytest.Audit{}
	m := model.NewScripted(
		model.CallTools(
			model.ToolCall{ID: "c1", Name: "test.echo", Args: json.RawMessage(`{"id":7}`)},
			model.ToolCall{ID: "c2", Name: "test.mixed"},
			model.ToolCall{ID: "c3", Name: "test.fail"},
		),
		model.Reply("done"),
	)
	a, err := agent.New(t.Context(), agent.Config{
		Harness: &harness.Harness{
			Name:         "triage",
			Instructions: "Triage the ticket.",
			Model:        harness.Model{Provider: "scripted", Name: "scripted"},
			Tools:        []string{"test.echo", "test.fail"},
			Limits:       harness.Limits{MaxSteps: 3, MaxToolCalls: 10},
		},
		Model: m,
		Tools: tools,
		Audit: audit,
	})
	require.NoError(t, err)

	res, err := a.Run(t.Context(), "ticket 7")
	require.NoError(t, err)

	assert.Equal(t, []string{"echo", "fail"}, log.calls(), "the ungranted tool never reached the server")
	names := []string{}
	for _, d := range m.Requests()[0].Tools {
		names = append(names, d.Name)
	}
	assert.Equal(t, []string{"test.echo", "test.fail"}, names, "the model is offered granted tools only")

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
