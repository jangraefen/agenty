package agent_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/agent"
	"github.com/jangraefen/agenty/internal/harness"
	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/toolgateway"
	"github.com/jangraefen/agenty/internal/toolgateway/gatewaytest"
)

const instructions = "Triage the ticket."

// fixture is an agent's wiring: a harness, an audit recorder and fake tools.
type fixture struct {
	harness *harness.Harness
	audit   *gatewaytest.Audit
	read    *gatewaytest.Tool
	label   *gatewaytest.Tool
	del     *gatewaytest.Tool
}

// newFixture grants tickets_read and tickets_label. tickets_delete is served
// but not granted.
func newFixture(maxSteps int) *fixture {
	return &fixture{
		harness: &harness.Harness{
			Name:         "triage",
			Instructions: instructions,
			Model:        harness.Model{Provider: "scripted", Name: "scripted"},
			Tools:        []string{"tickets_read", "tickets_label"},
			Limits:       harness.Limits{MaxSteps: maxSteps, MaxToolCalls: 50},
		},
		audit: &gatewaytest.Audit{},
		read:  &gatewaytest.Tool{Name: "tickets_read", Result: json.RawMessage(`{"title":"Printer on fire"}`)},
		label: &gatewaytest.Tool{Name: "tickets_label", Result: json.RawMessage(`{"ok":true}`)},
		del:   &gatewaytest.Tool{Name: "tickets_delete", Result: json.RawMessage(`{"ok":true}`)},
	}
}

// config wires the fixture into an agent config for m.
func (f *fixture) config(m model.Model) agent.Config {
	return agent.Config{
		RunID:    "r1",
		Redactor: gatewaytest.NoSecrets,
		Harness:  f.harness,
		Model:    m,
		Servers:  gatewaytest.Servers(f.read, f.label, f.del),
		Audit:    f.audit,
	}
}

// run builds an agent for m and runs it on a fixed input.
func (f *fixture) run(t *testing.T, m model.Model) (agent.Result, error) {
	t.Helper()
	a, err := agent.New(context.Background(), f.config(m))
	require.NoError(t, err)
	return a.Continue(context.Background(), nil, "ticket 7")
}

func (f *fixture) toolCalls() int {
	return f.read.Calls + f.label.Calls + f.del.Calls
}

func call(id, name string) model.ToolCall {
	return model.ToolCall{ID: id, Name: name, Args: json.RawMessage(`{"id":7}`)}
}

func recordsOf(records []toolgateway.Record, event toolgateway.Event) []toolgateway.Record {
	var out []toolgateway.Record
	for _, r := range records {
		if r.Event == event {
			out = append(out, r)
		}
	}
	return out
}
