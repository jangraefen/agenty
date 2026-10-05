package agent_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

// TestInvariant_SideEffectsOnlyViaGateway guards trust-model guarantee 2:
// every side effect goes through the gateway; nothing else executes tools.
// This test checks it at run time; the forbidigo rule in .golangci.yml checks
// it statically: outside internal/toolgateway, no code may call Tool.Call.
func TestInvariant_SideEffectsOnlyViaGateway(t *testing.T) {
	t.Run("every tool execution is a gateway call", func(t *testing.T) {
		f := newFixture(5)
		m := model.NewScripted(
			model.CallTools(call("c1", "tickets_read"), call("c2", "tickets_delete"), call("c3", "tickets_label")),
			model.CallTools(call("c4", "tickets_read")),
			model.Reply("done"),
		)

		_, err := f.run(t, m)
		require.NoError(t, err)

		decisions := recordsOf(f.audit.Records, toolgateway.EventDecision)
		assert.Len(t, decisions, 4, "every tool call the model makes reaches the gateway")
		executed := map[string]int{}
		for _, r := range recordsOf(f.audit.Records, toolgateway.EventResult) {
			executed[r.Tool]++
		}
		assert.Equal(t, executed["tickets_read"], f.read.Calls)
		assert.Equal(t, executed["tickets_label"], f.label.Calls)
		assert.Zero(t, f.del.Calls)
		assert.Equal(t, 3, f.toolCalls())
	})
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
		{"registered but not granted", "tickets_delete", "tool not granted"},
		{"granted but not resolvable", "tickets_ghost", "tool not resolved"},
		{"empty name", "", "tool not granted"},
		{"case variant of a granted tool", "TICKETS_READ", "tool not granted"},
		{"path-like name", "../tickets_read", "tool not granted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(3)
			m := model.NewScripted(model.CallTools(call("c1", tt.call)), model.Reply("understood"))

			res, err := f.run(t, m)

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
