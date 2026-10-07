package agent_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/agent"
	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/model/modeltest"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

// earlier is the conversation of an earlier run: an input, a tool call, its
// result, and the model's answer.
func earlier() []model.Message {
	return []model.Message{
		{Role: model.RoleUser, Text: "ticket 7"},
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{call("c1", "tickets_read")}},
		{Role: model.RoleUser, ToolResults: []model.ToolResult{{CallID: "c1", Content: `{"title":"Printer on fire"}`}}},
		{Role: model.RoleAssistant, Text: "The printer is on fire."},
	}
}

func TestRun_ContinuesAConversation(t *testing.T) {
	f := newFixture(3)
	tr := &transcript{failAt: -1}
	m := modeltest.NewScripted(
		modeltest.CallTools(call("c2", "tickets_label")),
		modeltest.Reply("Labelled it urgent."),
	)
	cfg := f.config(m)
	cfg.Transcript = tr
	a, err := agent.New(context.Background(), cfg)
	require.NoError(t, err)

	res, err := a.Start().Continue(context.Background(), earlier(), "label it urgent")

	require.NoError(t, err)
	assert.Equal(t, "Labelled it urgent.", res.Output)
	assert.Equal(t, 2, res.Steps, "steps count this run's model calls only")
	requests := m.Requests()
	require.Len(t, requests, 2)
	want := append(earlier(), model.Message{Role: model.RoleUser, Text: "label it urgent"})
	assert.Equal(t, want, requests[0].Messages, "the model sees the earlier conversation, then the new input")
	assert.Equal(t, instructions, requests[0].System)
	require.Len(t, res.Messages, 4, "the result holds this run's messages only")
	assert.Equal(t, "label it urgent", res.Messages[0].Text)
	assert.Equal(t, []int{0, 1, 2, 3}, tr.indexes, "this run's transcript starts with its input")
	assert.Equal(t, res.Messages, tr.messages, "earlier messages are not recorded again")
	assert.Zero(t, f.read.Calls, "earlier tool calls are not run again")
	assert.Equal(t, 1, f.label.Calls)
	decisions := recordsOf(f.audit.Records, toolgateway.EventDecision)
	require.Len(t, decisions, 1, "the new call goes through the gateway")
	assert.Equal(t, res.RunID, decisions[0].RunID)
}

func TestRun_ContinuesOnlyAfterAnAnswer(t *testing.T) {
	answered := earlier()
	tests := []struct {
		name    string
		history []model.Message
	}{
		{name: "ends with tool calls", history: answered[:2]},
		{name: "ends with tool results", history: answered[:3]},
		{name: "ends with the input", history: answered[:1]},
		{name: "starts with a reply", history: answered[3:]},
		{name: "starts with tool results", history: answered[2:]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(3)
			tr := &transcript{failAt: -1}
			m := modeltest.NewScripted(modeltest.Reply("hello"))
			cfg := f.config(m)
			cfg.Transcript = tr
			a, err := agent.New(context.Background(), cfg)
			require.NoError(t, err)

			_, err = a.Start().Continue(context.Background(), tt.history, "and now?")

			require.ErrorContains(t, err, "conversation")
			assert.Empty(t, m.Requests(), "the model is not called")
			assert.Empty(t, tr.messages, "nothing is recorded")
		})
	}
}

func TestAgent_PromptDigest(t *testing.T) {
	digest := func(change func(*fixture)) string {
		t.Helper()
		f := newFixture(3)
		change(f)
		a, err := agent.New(context.Background(), f.config(modeltest.NewScripted()))
		require.NoError(t, err)
		return a.PromptDigest()
	}
	same := digest(func(*fixture) {})

	assert.Equal(t, same, digest(func(*fixture) {}), "the same harness and tools give the same digest")
	assert.Equal(t, same, digest(func(f *fixture) { f.harness.Limits.MaxSteps = 9 }), "limits are not sent to the model")
	changes := map[string]func(*fixture){
		"instructions":     func(f *fixture) { f.harness.Instructions = "Shout." },
		"model":            func(f *fixture) { f.harness.Model.Name = "other" },
		"grants":           func(f *fixture) { f.harness.Tools = []string{"tickets_read"} },
		"tool description": func(f *fixture) { f.read.Description = "Reads a ticket." },
		"tool schema":      func(f *fixture) { f.read.InputSchema = json.RawMessage(`{"type":"object","required":["id"]}`) },
	}
	for name, change := range changes {
		assert.NotEqual(t, same, digest(change), "a change of %s changes the digest", name)
	}
}

// TestRun_ShowsCallsOfAToollessHistoryAsText: a harness that grants no tools
// any more is offered none, and providers refuse a conversation with tool
// calls but no tools, so the earlier calls and results reach the model as
// text, in place of the reply's provider form.
func TestRun_ShowsCallsOfAToollessHistoryAsText(t *testing.T) {
	f := newFixture(3)
	f.harness.Tools = nil
	m := modeltest.NewScripted(modeltest.Reply("It was on fire."))
	a, err := agent.New(context.Background(), f.config(m))
	require.NoError(t, err)
	history := earlier()
	history[1].Text = "Let me look."
	history[1].Provider = &model.ProviderPart{Name: "scripted", Data: json.RawMessage(`{"thinking":"look it up"}`)}
	history[2].ToolResults[0].IsError = true
	history[1].ToolCalls = append(history[1].ToolCalls, model.ToolCall{ID: "c2", Name: "tickets_label"})
	history[2].ToolResults = append(history[2].ToolResults, model.ToolResult{CallID: "c9", Content: "lost"})

	_, err = a.Start().Continue(context.Background(), history, "what was it?")

	require.NoError(t, err)
	require.Len(t, m.Requests(), 1)
	want := []model.Message{
		history[0],
		{Role: model.RoleAssistant, Text: "Let me look.\n\n[called tickets_read with {\"id\":7}]\n\n[called tickets_label with {}]"},
		{Role: model.RoleUser, Text: "[tickets_read failed: {\"title\":\"Printer on fire\"}]\n\n[a tool returned: lost]"},
		history[3],
		{Role: model.RoleUser, Text: "what was it?"},
	}
	assert.Equal(t, want, m.Requests()[0].Messages)
	assert.NotNil(t, history[1].Provider, "the caller's history is not changed")
}
