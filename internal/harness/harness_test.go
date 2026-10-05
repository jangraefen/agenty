package harness_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/harness"
)

func TestLoad_Valid(t *testing.T) {
	tests := []struct {
		file string
		want harness.Harness
	}{
		{
			file: "minimal.yaml",
			want: harness.Harness{
				Name:         "minimal",
				Instructions: "Answer the question.",
				Model:        harness.Model{Provider: "anthropic", Name: "claude-sonnet-5-5"},
				Limits:       harness.Limits{MaxSteps: 1, MaxToolCalls: 5},
			},
		},
		{
			file: "full.yaml",
			want: harness.Harness{
				Name:         "ticket-triage",
				Instructions: "Read the ticket and label it.\n",
				Model:        harness.Model{Provider: "anthropic", Name: "claude-sonnet-5-5"},
				Tools:        []string{"tickets_read", "tickets_label"},
				Limits:       harness.Limits{MaxSteps: 20, MaxToolCalls: 50},
				Policy: &harness.Policy{
					Files: []string{"triage.rego", "helpers.rego"},
					Rules: "deny contains \"too many writes\" if input.calls.by_tool[\"tickets_label\"] >= 10\n",
					FileSources: []string{
						"package agenty.tool\n\nrequire_approval contains \"labels need a human\" if input.tool == \"tickets_label\"\n",
						"package agenty.tool\n\nwrites := {\"tickets_label\", \"tickets_close\"}\n",
					},
				},
			},
		},
		{
			file: "inline_policy.yaml",
			want: harness.Harness{
				Name:         "inline",
				Instructions: "Label tickets.",
				Model:        harness.Model{Provider: "anthropic", Name: "claude-sonnet-5-5"},
				Tools:        []string{"tickets_label"},
				Limits:       harness.Limits{MaxSteps: 3, MaxToolCalls: 5},
				Policy:       &harness.Policy{Rules: "require_approval contains \"writes need a human\" if input.tool == \"tickets_label\"\n"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			got, err := harness.Load(filepath.Join("testdata", "valid", tt.file))
			require.NoError(t, err)
			assert.Equal(t, tt.want, *got)
		})
	}
}

func TestLoad_InvalidFields(t *testing.T) {
	tests := []struct {
		file       string
		wantFields []string
	}{
		{"missing_name.yaml", []string{"name"}},
		{"bad_name.yaml", []string{"name"}},
		{"missing_instructions.yaml", []string{"instructions"}},
		{"missing_model_provider.yaml", []string{"model.provider"}},
		{"missing_model_name.yaml", []string{"model.name"}},
		{"missing_max_steps.yaml", []string{"limits.max_steps"}},
		{"negative_max_steps.yaml", []string{"limits.max_steps"}},
		{"missing_max_tool_calls.yaml", []string{"limits.max_tool_calls"}},
		{"zero_max_tool_calls.yaml", []string{"limits.max_tool_calls"}},
		{"missing_policy_file.yaml", []string{"policy.files[1]"}},
		{"policy_files_invalid.yaml", []string{"policy.files[1]", "policy.files[2]"}},
		{"policy_empty.yaml", []string{"policy"}},
		{"policy_rules_with_package.yaml", []string{"policy.rules"}},
		{"duplicate_tool.yaml", []string{"tools[1]"}},
		{"empty_tool.yaml", []string{"tools[1]"}},
		{"bad_tool_name.yaml", []string{"tools[1]"}},
		{"many_errors.yaml", []string{"name", "instructions", "model.provider", "model.name", "limits.max_steps", "limits.max_tool_calls"}},
		{"empty.yaml", []string{"name", "instructions", "model.provider", "model.name", "limits.max_steps", "limits.max_tool_calls"}},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			got, err := harness.Load(filepath.Join("testdata", "invalid", tt.file))
			require.Error(t, err)
			assert.Nil(t, got)
			assert.Equal(t, tt.wantFields, fieldsOf(err))
			for _, f := range tt.wantFields {
				assert.ErrorContains(t, err, f)
			}
		})
	}
}

func TestLoad_InvalidDocument(t *testing.T) {
	tests := []struct {
		file         string
		wantContains string
	}{
		{"unknown_field.yaml", "max_step"},
		{"malformed.yaml", "malformed.yaml"},
		{"does_not_exist.yaml", "does_not_exist.yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			got, err := harness.Load(filepath.Join("testdata", "invalid", tt.file))
			require.ErrorContains(t, err, tt.wantContains)
			assert.Nil(t, got)
			assert.Empty(t, fieldsOf(err), "document errors are not field errors")
		})
	}
}

func TestParse_MultipleDocumentsRejected(t *testing.T) {
	doc := "name: a\ninstructions: x\nmodel: {provider: p, name: m}\nlimits: {max_steps: 1, max_tool_calls: 1}\n---\nname: b\n"
	got, err := harness.Parse([]byte(doc))
	require.Error(t, err)
	assert.Nil(t, got)
	assert.ErrorContains(t, err, "single document")
}

func TestFieldError_Error(t *testing.T) {
	err := &harness.FieldError{Field: "limits.max_steps", Msg: "must be greater than 0"}
	assert.Equal(t, `field "limits.max_steps": must be greater than 0`, err.Error())
}

// fieldsOf returns the fields named by every FieldError joined into err, in order.
func fieldsOf(err error) []string {
	var fields []string
	var walk func(error)
	walk = func(e error) {
		if fe, ok := e.(*harness.FieldError); ok { //nolint:errorlint // the walk needs the exact node; errors.As would match wrappers too
			fields = append(fields, fe.Field)
			return
		}
		if j, ok := e.(interface{ Unwrap() []error }); ok {
			for _, inner := range j.Unwrap() {
				walk(inner)
			}
			return
		}
		if inner := errors.Unwrap(e); inner != nil {
			walk(inner)
		}
	}
	if err != nil {
		walk(err)
	}
	return fields
}

func TestParse_KeepsPolicyPathWithoutReadingIt(t *testing.T) {
	doc := "name: a\ninstructions: x\nmodel: {provider: p, name: m}\nlimits: {max_steps: 1, max_tool_calls: 1}\npolicy: {files: [extra.rego]}\n"
	got, err := harness.Parse([]byte(doc))
	require.NoError(t, err)
	assert.Equal(t, []string{"extra.rego"}, got.Policy.Files)
	assert.Empty(t, got.Policy.FileSources, "only Load knows where the harness file lives")
}

func TestParse_PolicyFileSourcesCannotComeFromYAML(t *testing.T) {
	doc := "name: a\ninstructions: x\nmodel: {provider: p, name: m}\nlimits: {max_steps: 1, max_tool_calls: 1}\npolicy: {files: [x.rego], filesources: [y]}\n"
	_, err := harness.Parse([]byte(doc))
	require.ErrorContains(t, err, "filesources")
}

func TestLoad_AbsolutePolicyPath(t *testing.T) {
	dir := t.TempDir()
	policyPath := filepath.Join(dir, "elsewhere.rego")
	require.NoError(t, os.WriteFile(policyPath, []byte("package agenty.tool\n"), 0o600))
	harnessPath := filepath.Join(t.TempDir(), "h.yaml")
	doc := "name: a\ninstructions: x\nmodel: {provider: p, name: m}\nlimits: {max_steps: 1, max_tool_calls: 1}\npolicy: {files: [" + policyPath + "]}\n"
	require.NoError(t, os.WriteFile(harnessPath, []byte(doc), 0o600))

	got, err := harness.Load(harnessPath)

	require.NoError(t, err)
	assert.Equal(t, []string{"package agenty.tool\n"}, got.Policy.FileSources)
}
