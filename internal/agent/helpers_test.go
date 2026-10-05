package agent_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/harness"
	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/toolgateway"
	"github.com/jangraefen/agenty/internal/toolgateway/gatewaytest"
)

const instructions = "Triage the ticket."

// fixture is one run's wiring: a harness, its gateway and the fakes behind it.
type fixture struct {
	harness *harness.Harness
	gateway *toolgateway.Gateway
	audit   *gatewaytest.Audit
	read    *gatewaytest.Tool
	label   *gatewaytest.Tool
	del     *gatewaytest.Tool
}

// newFixture grants tickets.read, tickets.label and tickets.ghost (which no
// tool resolves). tickets.delete is registered but not granted.
func newFixture(t *testing.T, maxSteps int) *fixture {
	t.Helper()
	f := &fixture{
		harness: &harness.Harness{
			Name:         "triage",
			Instructions: instructions,
			Model:        harness.Model{Provider: "scripted", Name: "scripted"},
			Tools:        []string{"tickets.read", "tickets.label", "tickets.ghost"},
			Limits:       harness.Limits{MaxSteps: maxSteps},
		},
		audit: &gatewaytest.Audit{},
		read:  &gatewaytest.Tool{Name: "tickets.read", Result: json.RawMessage(`{"title":"Printer on fire"}`)},
		label: &gatewaytest.Tool{Name: "tickets.label", Result: json.RawMessage(`{"ok":true}`)},
		del:   &gatewaytest.Tool{Name: "tickets.delete", Result: json.RawMessage(`{"ok":true}`)},
	}
	gw, err := toolgateway.New(toolgateway.Config{
		RunID:   "run-1",
		Granted: f.harness.Tools,
		Tools:   []toolgateway.Tool{f.read, f.label, f.del},
		Audit:   f.audit,
	})
	require.NoError(t, err)
	f.gateway = gw
	return f
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
