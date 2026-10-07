package agent_test

import (
	"context"
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
