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
			f := newFixture(tt.maxSteps)
			f.read.Err = tt.readErr
			f.audit.FailOn = tt.failAuditOn
			m := model.NewScripted(tt.script...)

			res, err := f.run(t, m)

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
	f := newFixture(3)
	m := model.NewScripted(model.Reply("hello"))

	_, err := f.run(t, m)
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
	f := newFixture(3)
	f.label.Err = errors.New("label service down")
	toolCalls := []model.ToolCall{call("c1", "tickets.read"), call("c2", "tickets.delete"), call("c3", "tickets.label")}
	m := model.NewScripted(model.CallTools(toolCalls...), model.Reply("done"))

	res, err := f.run(t, m)
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
	f := newFixture(3)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.read.OnCall = func(context.Context) { cancel() }
	m := model.NewScripted(
		model.CallTools(call("c1", "tickets.read"), call("c2", "tickets.label")),
		model.Reply("unreachable"),
	)

	a, err := agent.New(f.config(m))
	require.NoError(t, err)

	res, err := a.Run(ctx, "ticket 7")

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, f.read.Calls)
	assert.Zero(t, f.label.Calls, "no tool runs after the run is cancelled")
	assert.Equal(t, 1, res.Steps)
}

func TestNew_RejectsInvalidConfig(t *testing.T) {
	f := newFixture(3)
	m := model.NewScripted(model.Reply("unused"))
	invalid := *f.harness
	invalid.Limits.MaxSteps = 0
	withConfig := func(change func(*agent.Config)) agent.Config {
		cfg := f.config(m)
		change(&cfg)
		return cfg
	}

	tests := []struct {
		name    string
		cfg     agent.Config
		wantErr string
	}{
		{"nil harness", withConfig(func(c *agent.Config) { c.Harness = nil }), "harness is required"},
		{"nil model", withConfig(func(c *agent.Config) { c.Model = nil }), "model is required"},
		{"nil audit", withConfig(func(c *agent.Config) { c.Audit = nil }), "audit is required"},
		{"invalid harness", withConfig(func(c *agent.Config) { c.Harness = &invalid }), "limits.max_steps"},
		{"nil tool", withConfig(func(c *agent.Config) { c.Tools = append(c.Tools, nil) }), "tool 3 is nil"},
		{"duplicate tool", withConfig(func(c *agent.Config) { c.Tools = append(c.Tools, f.read) }), `duplicate tool "tickets.read"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := agent.New(tt.cfg)
			require.ErrorContains(t, err, tt.wantErr)
			assert.Nil(t, a)
		})
	}
	assert.Empty(t, m.Requests(), "an invalid config never reaches the model")
}

func TestRun_RejectsEmptyInput(t *testing.T) {
	f := newFixture(3)
	m := model.NewScripted(model.Reply("unused"))
	a, err := agent.New(f.config(m))
	require.NoError(t, err)

	res, err := a.Run(context.Background(), "")

	require.ErrorContains(t, err, "input is required")
	assert.Zero(t, res)
	assert.Empty(t, m.Requests())
	assert.Empty(t, f.audit.Records)
}

func TestRun_EachRunHasItsOwnRunIDAndTranscript(t *testing.T) {
	f := newFixture(3)
	m := model.NewScripted(
		model.CallTools(call("c1", "tickets.read")), model.Reply("first done"),
		model.CallTools(call("c2", "tickets.read")), model.Reply("second done"),
	)
	a, err := agent.New(f.config(m))
	require.NoError(t, err)

	first, err := a.Run(context.Background(), "ticket 7")
	require.NoError(t, err)
	second, err := a.Run(context.Background(), "ticket 8")
	require.NoError(t, err)

	require.NotEmpty(t, first.RunID)
	require.NotEmpty(t, second.RunID)
	assert.NotEqual(t, first.RunID, second.RunID)
	require.Len(t, f.audit.Records, 4)
	for i, r := range f.audit.Records {
		want := first.RunID
		if i >= 2 {
			want = second.RunID
		}
		assert.Equal(t, want, r.RunID, "record %d", i)
	}

	reqs := m.Requests()
	require.Len(t, reqs, 4)
	assert.Equal(t, []model.Message{{Role: model.RoleUser, Text: "ticket 8"}}, reqs[2].Messages,
		"a new run starts with a fresh transcript")
	assert.Len(t, second.Messages, 4)
}

func TestNew_HarnessChangesAfterNewDoNotWidenGrants(t *testing.T) {
	f := newFixture(3)
	m := model.NewScripted(model.CallTools(call("c1", "tickets.delete")), model.Reply("done"))
	a, err := agent.New(f.config(m))
	require.NoError(t, err)

	f.harness.Tools[2] = "tickets.delete"
	f.harness.Limits.MaxSteps = 1
	_, err = a.Run(context.Background(), "ticket 7")

	require.NoError(t, err, "max_steps is still the original 3")
	assert.Zero(t, f.del.Calls)
	require.Len(t, f.audit.Records, 1)
	assert.Equal(t, toolgateway.Deny, f.audit.Records[0].Decision)
}
