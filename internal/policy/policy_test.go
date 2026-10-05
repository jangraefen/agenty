package policy_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/policy"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

// layer is a one-module layer with the given rules in package agenty.tool.
func layer(name, rules string) policy.Layer {
	return policy.Layer{Name: name, Modules: []policy.Module{{Name: name + ".rego", Source: "package agenty.tool\n\n" + rules}}}
}

func input() toolgateway.Request {
	return toolgateway.Request{
		RunID:   "run-1",
		Harness: "triage",
		Tool:    "tickets.label",
		Args:    json.RawMessage(`{"id":7,"label":"urgent"}`),
		Calls: toolgateway.CallCounts{
			Total:  3,
			ByTool: map[string]int{"tickets.read": 2, "tickets.label": 1},
		},
	}
}

func TestNew_RejectsInvalidLayers(t *testing.T) {
	tests := []struct {
		name    string
		layer   policy.Layer
		wantErr string
	}{
		{"unnamed layer", policy.Layer{Modules: []policy.Module{{Name: "a.rego", Source: "package agenty.tool\n"}}}, "layer 0 has no name"},
		{"syntax error", layer("central", "deny contains"), "central.rego"},
		{"wrong package", policy.Layer{Name: "central", Modules: []policy.Module{{Name: "other.rego", Source: "package something.else\n"}}}, `other.rego: package must be agenty.tool, not something.else`},
		{"rego v0 syntax", layer("central", `deny[msg] { msg := "x" }`), "central.rego"},
		{"compile error", layer("central", `deny contains msg if { msg := no_such_function(1) }`), "no_such_function"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := policy.New(context.Background(), tt.layer)
			require.ErrorContains(t, err, tt.wantErr)
			assert.Nil(t, e)
		})
	}
}

func TestEvaluate_Decisions(t *testing.T) {
	tests := []struct {
		name        string
		rules       string
		want        toolgateway.Decision
		wantReasons []string
	}{
		{"no rules allows", "", toolgateway.Allow, nil},
		{"unmatched deny allows", `deny contains "never" if input.tool == "tickets.delete"`, toolgateway.Allow, nil},
		{"matched deny denies", `deny contains "no labels" if input.tool == "tickets.label"`, toolgateway.Deny, []string{"no labels"}},
		{"matched approval requires approval", `require_approval contains "writes need a human" if input.tool == "tickets.label"`, toolgateway.RequireApproval, []string{"writes need a human"}},
		{
			name: "deny beats approval and keeps only deny reasons",
			rules: `require_approval contains "writes need a human" if input.tool == "tickets.label"
deny contains "b: too many writes" if input.calls.by_tool["tickets.label"] >= 1
deny contains "a: urgent is reserved" if input.args.label == "urgent"`,
			want:        toolgateway.Deny,
			wantReasons: []string{"a: urgent is reserved", "b: too many writes"},
		},
		{
			name:        "sees run, harness and counts",
			rules:       `deny contains sprintf("%s/%s/%d/%d", [input.run_id, input.harness, input.calls.total, input.calls.by_tool["tickets.read"]]) if true`,
			want:        toolgateway.Deny,
			wantReasons: []string{"run-1/triage/3/2"},
		},
		{"non-string reasons are rendered as JSON", `deny contains {"code": 7} if true`, toolgateway.Deny, []string{`{"code":7}`}},
		{"allow rules have no effect", `allow := true`, toolgateway.Allow, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := policy.New(context.Background(), layer("central", tt.rules))
			require.NoError(t, err)

			v, err := e.Evaluate(context.Background(), input())

			require.NoError(t, err)
			assert.Equal(t, tt.want, v.Decision)
			assert.Equal(t, tt.wantReasons, v.Reasons)
		})
	}
}

func TestEvaluate_NoLayersAllows(t *testing.T) {
	e, err := policy.New(context.Background())
	require.NoError(t, err)

	v, err := e.Evaluate(context.Background(), input())

	require.NoError(t, err)
	assert.Equal(t, toolgateway.Allow, v.Decision)
}

func TestEvaluate_MissingArgsAreNull(t *testing.T) {
	e, err := policy.New(context.Background(), layer("central", `deny contains "no args" if input.args == null`))
	require.NoError(t, err)
	in := input()
	in.Args = nil

	v, err := e.Evaluate(context.Background(), in)

	require.NoError(t, err)
	assert.Equal(t, toolgateway.Deny, v.Decision)
}

func TestEvaluate_Errors(t *testing.T) {
	tests := []struct {
		name    string
		rules   string
		args    json.RawMessage
		wantErr []string
	}{
		{"invalid args JSON", ``, json.RawMessage(`{not json`), []string{"policy: input: "}},
		{"builtin error is an error, not a silent no-match", `deny contains "too big" if to_number(input.args.label) > 3`, nil, []string{"central: ", "to_number"}},
		{"conflicting values", `x := 1 if true
x := 2 if true
deny contains "x" if x == 1`, nil, []string{"central: ", "conflict"}},
		{"deny is not a set", `deny := "nope"`, nil, []string{"central: ", "deny: must be a set"}},
		{"require_approval is not a set", `require_approval := true`, nil, []string{"central: ", "require_approval: must be a set"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := policy.New(context.Background(), layer("central", tt.rules))
			require.NoError(t, err)
			in := input()
			if tt.args != nil {
				in.Args = tt.args
			}

			v, err := e.Evaluate(context.Background(), in)

			for _, want := range tt.wantErr {
				require.ErrorContains(t, err, want)
			}
			assert.Zero(t, v)
		})
	}
}

func TestEvaluate_InputDocument(t *testing.T) {
	e, err := policy.New(context.Background(), layer("central", `deny contains json.marshal(input) if true`))
	require.NoError(t, err)

	v, err := e.Evaluate(context.Background(), input())

	require.NoError(t, err)
	require.Len(t, v.Reasons, 1)
	assert.JSONEq(t, `{
		"run_id": "run-1",
		"harness": "triage",
		"tool": "tickets.label",
		"args": {"id": 7, "label": "urgent"},
		"calls": {"total": 3, "by_tool": {"tickets.read": 2, "tickets.label": 1}}
	}`, v.Reasons[0], "this is the input contract policy authors write against")
}

func TestRulesModule_AddsThePackage(t *testing.T) {
	m := policy.RulesModule("triage (inline)", `require_approval contains "writes need a human" if input.tool == "tickets.label"`)
	assert.Equal(t, "triage (inline)", m.Name)

	e, err := policy.New(context.Background(), policy.Layer{Name: "harness", Modules: []policy.Module{m}})
	require.NoError(t, err)
	v, err := e.Evaluate(context.Background(), input())

	require.NoError(t, err)
	assert.Equal(t, toolgateway.RequireApproval, v.Decision)
}
