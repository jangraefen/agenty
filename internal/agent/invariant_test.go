package agent_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/agent"
	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

// TestInvariant_SideEffectsOnlyViaGateway guards trust-model guarantee 2:
// every side effect goes through the gateway; nothing else executes tools.
func TestInvariant_SideEffectsOnlyViaGateway(t *testing.T) {
	t.Run("every tool execution is a gateway call", func(t *testing.T) {
		f := newFixture(t, 5)
		m := model.NewScripted(
			model.CallTools(call("c1", "tickets.read"), call("c2", "tickets.delete"), call("c3", "tickets.label")),
			model.CallTools(call("c4", "tickets.read")),
			model.Reply("done"),
		)

		_, err := agent.Run(context.Background(), agent.Config{Harness: f.harness, Model: m, Gateway: f.gateway, Input: "ticket 7"})
		require.NoError(t, err)

		decisions := recordsOf(f.audit.Records, toolgateway.EventDecision)
		assert.Len(t, decisions, 4, "every tool call the model makes reaches the gateway")
		executed := map[string]int{}
		for _, r := range recordsOf(f.audit.Records, toolgateway.EventResult) {
			executed[r.Tool]++
		}
		assert.Equal(t, executed["tickets.read"], f.read.Calls)
		assert.Equal(t, executed["tickets.label"], f.label.Calls)
		assert.Zero(t, f.del.Calls)
		assert.Equal(t, 3, f.toolCalls())
	})

	t.Run("agent config cannot carry a tool", func(t *testing.T) {
		assert.Empty(t, toolHoldingFields(reflect.TypeFor[agent.Config]()),
			"the agent must reach tools only through the gateway")
	})

	t.Run("checker detects tool-holding fields", func(t *testing.T) {
		type leaky struct {
			Direct  toolgateway.Tool
			List    []toolgateway.Tool
			Any     any
			Nested  struct{ ByName map[string]toolgateway.Tool }
			Safe    model.Model
			private toolgateway.Tool
		}
		_ = leaky{}.private
		assert.Equal(t, []string{"Direct", "List", "Any", "Nested.ByName"}, toolHoldingFields(reflect.TypeFor[leaky]()))
	})
}

// toolHoldingFields lists the exported fields of t, recursing into exported
// struct fields, whose values could hold a toolgateway.Tool.
func toolHoldingFields(t reflect.Type) []string {
	toolType := reflect.TypeFor[toolgateway.Tool]()
	var found []string
	var walk func(t reflect.Type, prefix string)
	walk = func(t reflect.Type, prefix string) {
		for i := range t.NumField() {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}
			ft := field.Type
			for ft.Kind() == reflect.Pointer || ft.Kind() == reflect.Slice || ft.Kind() == reflect.Array || ft.Kind() == reflect.Map {
				ft = ft.Elem()
			}
			name := prefix + field.Name
			switch {
			case ft.Kind() == reflect.Interface && toolType.Implements(ft):
				found = append(found, name)
			case ft.Kind() != reflect.Interface && (ft.Implements(toolType) || reflect.PointerTo(ft).Implements(toolType)):
				found = append(found, name)
			case ft.Kind() == reflect.Struct:
				walk(ft, name+".")
			}
		}
	}
	walk(t, "")
	return found
}

// TestInvariant_ModelIsNotTrusted guards trust-model guarantee 1: whatever
// tool the model asks for, deterministic controls decide, and a refused
// call is reported back to the model instead of executing.
func TestInvariant_ModelIsNotTrusted(t *testing.T) {
	tests := []struct {
		name   string
		call   string
		reason string
	}{
		{"registered but not granted", "tickets.delete", "tool not granted"},
		{"granted but not resolvable", "tickets.ghost", "tool not resolved"},
		{"empty name", "", "tool not granted"},
		{"case variant of a granted tool", "TICKETS.READ", "tool not granted"},
		{"path-like name", "../tickets.read", "tool not granted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, 3)
			m := model.NewScripted(model.CallTools(call("c1", tt.call)), model.Reply("understood"))

			res, err := agent.Run(context.Background(), agent.Config{Harness: f.harness, Model: m, Gateway: f.gateway, Input: "ticket 7"})

			require.NoError(t, err, "a refused call does not end the run")
			assert.Equal(t, "understood", res.Output)
			assert.Zero(t, f.toolCalls(), "a refused call executes nothing")
			require.Len(t, f.audit.Records, 1)
			assert.Equal(t, toolgateway.Deny, f.audit.Records[0].Decision)
			assert.Equal(t, tt.reason, f.audit.Records[0].Reason)

			reqs := m.Requests()
			require.Len(t, reqs, 2)
			last := reqs[1].Messages[len(reqs[1].Messages)-1]
			require.Len(t, last.ToolResults, 1)
			assert.Equal(t, "c1", last.ToolResults[0].CallID)
			assert.True(t, last.ToolResults[0].IsError)
			assert.Contains(t, last.ToolResults[0].Content, tt.reason)
		})
	}
}
