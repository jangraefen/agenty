package agent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/agent"
	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

func TestRun_Outcomes(t *testing.T) {
	boom := errors.New("provider overloaded")
	tests := []struct {
		name        string
		maxSteps    int
		readErr     error
		failAuditOn toolgateway.Event
		script      []model.Step
		wantOutput  string
		wantSteps   int
		wantErr     error
		wantAudited []string
		wantCalls   int
	}{
		{
			name:       "answers without tools",
			maxSteps:   3,
			script:     []model.Step{model.Reply("hello")},
			wantOutput: "hello",
			wantSteps:  1,
		},
		{
			name:        "calls a tool, then answers",
			maxSteps:    3,
			script:      []model.Step{model.CallTools(call("c1", "tickets.read")), model.Reply("done")},
			wantOutput:  "done",
			wantSteps:   2,
			wantAudited: []string{"tickets.read"},
			wantCalls:   1,
		},
		{
			name:     "several calls in one step run in order",
			maxSteps: 3,
			script: []model.Step{
				model.CallTools(call("c1", "tickets.read"), call("c2", "tickets.label")),
				model.Reply("done"),
			},
			wantOutput:  "done",
			wantSteps:   2,
			wantAudited: []string{"tickets.read", "tickets.label"},
			wantCalls:   2,
		},
		{
			name:        "tool error goes back to the model and the run continues",
			maxSteps:    3,
			readErr:     errors.New("ticket system unavailable"),
			script:      []model.Step{model.CallTools(call("c1", "tickets.read")), model.Reply("recovered")},
			wantOutput:  "recovered",
			wantSteps:   2,
			wantAudited: []string{"tickets.read"},
			wantCalls:   1,
		},
		{
			name:        "final answer on the last allowed step",
			maxSteps:    2,
			script:      []model.Step{model.CallTools(call("c1", "tickets.read")), model.Reply("just in time")},
			wantOutput:  "just in time",
			wantSteps:   2,
			wantAudited: []string{"tickets.read"},
			wantCalls:   1,
		},
		{
			name:     "max steps reached: calls of the last step are not executed",
			maxSteps: 2,
			script: []model.Step{
				model.CallTools(call("c1", "tickets.read")),
				model.CallTools(call("c2", "tickets.label")),
				model.Reply("too late"),
			},
			wantSteps:   2,
			wantErr:     agent.ErrMaxSteps,
			wantAudited: []string{"tickets.read"},
			wantCalls:   1,
		},
		{
			name:     "model error ends the run",
			maxSteps: 3,
			script:   []model.Step{model.Fail(boom)},
			wantErr:  boom,
		},
		{
			name:        "unrecorded decision ends the run",
			maxSteps:    3,
			failAuditOn: toolgateway.EventDecision,
			script:      []model.Step{model.CallTools(call("c1", "tickets.read")), model.Reply("unreachable")},
			wantSteps:   1,
			wantErr:     toolgateway.ErrAudit,
		},
		{
			name:        "unrecorded result ends the run",
			maxSteps:    3,
			failAuditOn: toolgateway.EventResult,
			script:      []model.Step{model.CallTools(call("c1", "tickets.read")), model.Reply("unreachable")},
			wantSteps:   1,
			wantErr:     toolgateway.ErrAudit,
			wantAudited: []string{"tickets.read"},
			wantCalls:   1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, tt.maxSteps)
			f.read.Err = tt.readErr
			f.audit.FailOn = tt.failAuditOn
			m := model.NewScripted(tt.script...)

			res, err := agent.Run(context.Background(), agent.Config{Harness: f.harness, Model: m, Gateway: f.gateway, Input: "ticket 7"})

			if tt.wantErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tt.wantErr)
			}
			assert.Equal(t, tt.wantOutput, res.Output)
			assert.Equal(t, tt.wantSteps, res.Steps)
			assert.Equal(t, tt.wantCalls, f.toolCalls())
			var audited []string
			for _, r := range recordsOf(f.audit.Records, toolgateway.EventDecision) {
				audited = append(audited, r.Tool)
			}
			assert.Equal(t, tt.wantAudited, audited)
		})
	}
}

func TestRun_FirstRequestCarriesInstructionsInputAndGrantedTools(t *testing.T) {
	f := newFixture(t, 3)
	m := model.NewScripted(model.Reply("hello"))

	_, err := agent.Run(context.Background(), agent.Config{Harness: f.harness, Model: m, Gateway: f.gateway, Input: "ticket 7"})
	require.NoError(t, err)

	reqs := m.Requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, instructions, reqs[0].System)
	assert.Equal(t, []model.Message{{Role: model.RoleUser, Text: "ticket 7"}}, reqs[0].Messages)
	var names []string
	for _, d := range reqs[0].Tools {
		names = append(names, d.Name)
		assert.Equal(t, "fake "+d.Name, d.Description)
		assert.JSONEq(t, `{"type":"object"}`, string(d.InputSchema))
	}
	assert.Equal(t, []string{"tickets.label", "tickets.read"}, names,
		"only granted, resolvable tools are offered to the model")
}

func TestRun_FeedsToolResultsBackAndKeepsTheTranscript(t *testing.T) {
	f := newFixture(t, 3)
	f.label.Err = errors.New("label service down")
	toolCalls := []model.ToolCall{call("c1", "tickets.read"), call("c2", "tickets.delete"), call("c3", "tickets.label")}
	m := model.NewScripted(model.CallTools(toolCalls...), model.Reply("done"))

	res, err := agent.Run(context.Background(), agent.Config{Harness: f.harness, Model: m, Gateway: f.gateway, Input: "ticket 7"})
	require.NoError(t, err)

	assert.JSONEq(t, `{"id":7}`, string(f.read.Args))
	reqs := m.Requests()
	require.Len(t, reqs, 2)
	require.Len(t, reqs[1].Messages, 3)
	results := reqs[1].Messages[2]
	assert.Equal(t, model.RoleUser, results.Role)
	require.Len(t, results.ToolResults, 3)

	assert.Equal(t, model.ToolResult{CallID: "c1", Content: `{"title":"Printer on fire"}`}, results.ToolResults[0])
	assert.Equal(t, "c2", results.ToolResults[1].CallID)
	assert.True(t, results.ToolResults[1].IsError)
	assert.Contains(t, results.ToolResults[1].Content, "tool not granted")
	assert.Equal(t, "c3", results.ToolResults[2].CallID)
	assert.True(t, results.ToolResults[2].IsError)
	assert.Contains(t, results.ToolResults[2].Content, "label service down")

	want := append(reqs[1].Messages, model.Message{Role: model.RoleAssistant, Text: "done"})
	assert.Equal(t, want, res.Messages)
}

func TestRun_CancelledContextStopsFurtherSideEffects(t *testing.T) {
	f := newFixture(t, 3)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.read.OnCall = func(context.Context) { cancel() }
	m := model.NewScripted(
		model.CallTools(call("c1", "tickets.read"), call("c2", "tickets.label")),
		model.Reply("unreachable"),
	)

	res, err := agent.Run(ctx, agent.Config{Harness: f.harness, Model: m, Gateway: f.gateway, Input: "ticket 7"})

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, f.read.Calls)
	assert.Zero(t, f.label.Calls, "no tool runs after the run is cancelled")
	assert.Equal(t, 1, res.Steps)
}

func TestRun_RejectsInvalidConfig(t *testing.T) {
	f := newFixture(t, 3)
	m := model.NewScripted(model.Reply("unused"))
	invalid := *f.harness
	invalid.Limits.MaxSteps = 0

	tests := []struct {
		name    string
		cfg     agent.Config
		wantErr string
	}{
		{"nil harness", agent.Config{Model: m, Gateway: f.gateway, Input: "x"}, "harness is required"},
		{"nil model", agent.Config{Harness: f.harness, Gateway: f.gateway, Input: "x"}, "model is required"},
		{"nil gateway", agent.Config{Harness: f.harness, Model: m, Input: "x"}, "gateway is required"},
		{"empty input", agent.Config{Harness: f.harness, Model: m, Gateway: f.gateway}, "input is required"},
		{"invalid harness", agent.Config{Harness: &invalid, Model: m, Gateway: f.gateway, Input: "x"}, "limits.max_steps"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := agent.Run(context.Background(), tt.cfg)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
			assert.Zero(t, res)
		})
	}
	assert.Empty(t, m.Requests(), "an invalid config never reaches the model")
	assert.Empty(t, f.audit.Records)
}
