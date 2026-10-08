package cli_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/agent"
	"github.com/jangraefen/agenty/internal/config"
	"github.com/jangraefen/agenty/internal/harness"
	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/model/modeltest"
	"github.com/jangraefen/agenty/internal/toolgateway"
	"github.com/jangraefen/agenty/internal/toolgateway/gatewaytest"
)

// TestExample_Notes checks that the example in examples/notes loads and that
// its policy does what its comments say.
func TestExample_Notes(t *testing.T) {
	dir := filepath.Join("..", "..", "examples", "notes")
	cfg, err := config.Load(filepath.Join(dir, "agenty.yaml"))
	require.NoError(t, err)
	h, err := harness.Load(filepath.Join(dir, "notes.yaml"))
	require.NoError(t, err)
	require.Contains(t, cfg.MCPServers, "files")
	assert.Equal(t, []string{"demo"}, cfg.Workspaces["notes"].Members, "task demo signs in as demo, in notes")

	var tools []toolgateway.Tool
	for _, name := range h.Tools {
		tools = append(tools, &gatewaytest.Tool{Name: name, Result: json.RawMessage(`{}`)})
	}
	audit := &gatewaytest.Audit{}
	call := func(id, name, args string) model.ToolCall {
		return model.ToolCall{ID: id, Name: name, Args: json.RawMessage(args)}
	}
	newAgent := func(m model.Model) *agent.Agent {
		a, err := agent.New(context.Background(), agent.Config{
			RunID:    "r1",
			Records:  audit.Records,
			Redactor: gatewaytest.NoSecrets,
			Harness:  h,
			Model:    m,
			Servers:  gatewaytest.Servers(tools...),
			Policy:   cfg.Policy,
			Audit:    audit,
		})
		require.NoError(t, err)
		t.Cleanup(func() { assert.NoError(t, a.Close()) })
		return a
	}

	first, err := newAgent(modeltest.NewScripted(
		modeltest.CallTools(
			call("1", "files_read_text_file", `{"path":"notes.md"}`),
			call("2", "files_read_text_file", `{"path":"./sub/.env.example"}`),
			call("3", "files_write_file", `{"path":"notes.md","content":"- x"}`),
			call("4", "files_write_file", `{"path":"notes.md","content":"- y"}`),
		),
	)).Continue(context.Background(), nil, "tidy")
	var suspended *agent.Suspended
	require.ErrorAs(t, err, &suspended, "the first write waits for approval")
	_, err = newAgent(modeltest.NewScripted(modeltest.Reply("done"))).Resume(context.Background(), nil, first.Messages, agent.Resumption{
		CallID:  suspended.CallID,
		Call:    suspended.Call,
		Results: suspended.Results,
		Answer:  toolgateway.Approval{Approved: true, Approver: "alice"},
	})
	require.NoError(t, err)

	var got []string
	for _, r := range audit.Records {
		got = append(got, string(r.Event)+" "+r.Tool+" "+string(r.Decision)+" "+r.Reason)
	}
	assert.Equal(t, []string{
		"decision files_read_text_file allow ",
		"result files_read_text_file allow ",
		"decision files_read_text_file deny policy: dotfiles may hold credentials",
		"decision files_write_file require_approval policy: changes to files need a human",
		"decision files_write_file require_approval policy: changes to files need a human",
		"approval files_write_file allow ",
		"result files_write_file allow ",
		"decision files_write_file deny policy: one rewrite per run is enough",
	}, got)
}
