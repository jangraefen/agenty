package cli_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/cli"
	"github.com/jangraefen/agenty/internal/mcptool"
	"github.com/jangraefen/agenty/internal/model/anthropic/anthropictest"
	"github.com/jangraefen/agenty/internal/toolgateway"
	"github.com/jangraefen/agenty/internal/toolgateway/gatewaytest"
)

const (
	apiKey     = "sk-ant-api-key-0123456789"
	filesToken = "files-token-abcdef0123"
)

const configYAML = `provider:
  anthropic:
    api_key: {env: ANTHROPIC_API_KEY}
    max_tokens: 1024
    base_url: %s
mcp_servers:
  files:
    command: files-mcp
    args: [--root, sandbox]
    env:
      FILES_TOKEN: {env: FILES_TOKEN}
  mail:
    command: mail-mcp
policy:
  files: [central.rego]
`

const centralRego = `package agenty.tool

require_approval contains "writes need a human" if input.tool == "files_write"
`

const harnessYAML = `name: notes
instructions: Keep the notes tidy.
model:
  provider: anthropic
  name: claude-test
tools: [files_read, files_write]
limits:
  max_steps: 5
  max_tool_calls: 10
policy:
  rules: |
    deny contains "no dotfiles" if {
      input.tool == "files_read"
      startswith(input.args.path, ".")
    }
`

// fakeServer is an MCP server as the CLI sees it.
type fakeServer struct {
	tools    []toolgateway.Tool
	toolsErr error
	closeErr error
	closed   bool
}

func (s *fakeServer) Tools(context.Context) ([]toolgateway.Tool, error) { return s.tools, s.toolsErr }

func (s *fakeServer) Close() error {
	s.closed = true
	return s.closeErr
}

// fixture is a working directory with a config, central policy and a
// harness, a fake Anthropic API, and fake MCP servers.
type fixture struct {
	dir     string
	api     *anthropictest.API
	vars    map[string]string
	stdin   string
	tty     bool
	servers map[string]*fakeServer
	// started records the servers the CLI started, in order.
	started    []mcptool.Server
	connectErr error

	read, write, del *gatewaytest.Tool

	stdout, stderr bytes.Buffer
}

func newFixture(t *testing.T, responses ...anthropictest.Response) *fixture {
	t.Helper()
	f := &fixture{
		dir:   t.TempDir(),
		api:   anthropictest.New(t, responses...),
		vars:  map[string]string{"ANTHROPIC_API_KEY": apiKey, "FILES_TOKEN": filesToken},
		stdin: "y\n",
		tty:   true,
		read:  &gatewaytest.Tool{Name: "files_read", Result: json.RawMessage(`{"content":"buy milk"}`)},
		write: &gatewaytest.Tool{Name: "files_write", Result: json.RawMessage(`{"ok":true}`)},
		del:   &gatewaytest.Tool{Name: "files_delete", Result: json.RawMessage(`{"ok":true}`)},
	}
	f.servers = map[string]*fakeServer{
		"files": {tools: []toolgateway.Tool{f.read, f.write, f.del}},
		"mail":  {},
	}
	f.writeFile(t, "agenty.yaml", strings.Replace(configYAML, "%s", f.api.URL, 1))
	f.writeFile(t, "central.rego", centralRego)
	f.writeFile(t, "harness.yaml", harnessYAML)
	return f
}

func (f *fixture) writeFile(t *testing.T, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(f.dir, name), []byte(content), 0o600))
}

func (f *fixture) path(name string) string { return filepath.Join(f.dir, name) }

// main runs the CLI with args as they are.
func (f *fixture) main(args ...string) int {
	return cli.Main(context.Background(), args, cli.Env{
		Stdin:       strings.NewReader(f.stdin),
		Stdout:      &f.stdout,
		Stderr:      &f.stderr,
		Interactive: f.tty,
		User:        "alice",
		LookupEnv: func(k string) (string, bool) {
			v, ok := f.vars[k]
			return v, ok
		},
		Connect: func(_ context.Context, srv mcptool.Server) (cli.ToolServer, error) {
			f.started = append(f.started, srv)
			if f.connectErr != nil {
				return nil, f.connectErr
			}
			return f.servers[srv.Name], nil
		},
	})
}

// run runs "agenty run" on the fixture's files, logging everything.
func (f *fixture) run(input string) int {
	return f.main("run", "--config", f.path("agenty.yaml"), "--harness", f.path("harness.yaml"), "--log-level", "debug", "--audit", f.path("audit.jsonl"), input)
}

// audit returns the audit log, one map per record.
func (f *fixture) audit(t *testing.T) []map[string]any {
	t.Helper()
	file, err := os.Open(f.path("audit.jsonl"))
	require.NoError(t, err)
	defer func() { assert.NoError(t, file.Close()) }()
	var records []map[string]any
	s := bufio.NewScanner(file)
	for s.Scan() {
		var m map[string]any
		require.NoError(t, json.Unmarshal(s.Bytes(), &m))
		records = append(records, m)
	}
	require.NoError(t, s.Err())
	return records
}

// events returns "<event> <tool> <decision>" for each audit record.
func events(records []map[string]any) []string {
	out := make([]string, len(records))
	for i, r := range records {
		out[i] = r["event"].(string) + " " + r["tool"].(string) + " " + r["decision"].(string)
	}
	return out
}

func toolUse(t *testing.T, id, name string, input any) anthropictest.Response {
	t.Helper()
	return anthropictest.Reply(t, "tool_use", anthropictest.ToolUseBlock(id, name, input))
}

func done(t *testing.T) anthropictest.Response {
	t.Helper()
	return anthropictest.Reply(t, "end_turn", anthropictest.TextBlock("Notes are tidy."))
}

func TestRun_EndToEnd(t *testing.T) {
	f := newFixture(t,
		toolUse(t, "toolu_1", "files_read", map[string]any{"path": "notes.md"}),
		toolUse(t, "toolu_2", "files_write", map[string]any{"path": "notes.md", "content": "- buy milk"}),
		done(t),
	)

	code := f.run("tidy my notes")

	require.Equal(t, 0, code, f.stderr.String())
	assert.Equal(t, "Notes are tidy.\n", f.stdout.String(), "stdout carries the answer only")
	assert.Equal(t, 1, f.read.Calls)
	assert.Equal(t, 1, f.write.Calls)
	assert.Contains(t, f.stderr.String(), "Approval needed: files_write")
	records := f.audit(t)
	assert.Equal(t, []string{
		"decision files_read allow",
		"result files_read allow",
		"decision files_write require_approval",
		"approval files_write allow",
		"result files_write allow",
	}, events(records))
	assert.Equal(t, "alice", records[3]["approver"])
	runID := records[0]["run_id"].(string)
	for _, r := range records {
		assert.Equal(t, runID, r["run_id"], "one run, one ID")
	}
	assert.Contains(t, f.stderr.String(), runID, "the log names the run, to find it in the audit log")
	assert.Contains(t, f.stderr.String(), "tools=\"[files_read files_write files_delete]\"", "debug logs list each server's tools")
	require.Len(t, f.started, 1, "only servers whose tools the harness grants are started")
	assert.Equal(t, mcptool.Server{
		Name:    "files",
		Command: "files-mcp",
		Args:    []string{"--root", "sandbox"},
		Env:     map[string]string{"FILES_TOKEN": filesToken},
	}, f.started[0])
	assert.True(t, f.servers["files"].closed, "servers are stopped when the run ends")
}

func TestRun_Denials(t *testing.T) {
	tests := []struct {
		name       string
		call       anthropictest.Response
		stdin      string
		tty        bool
		wantEvents []string
		wantReason string
	}{
		{
			name:       "approval rejected",
			call:       toolUse(t, "toolu_1", "files_write", map[string]any{"path": "notes.md"}),
			stdin:      "n\n",
			tty:        true,
			wantEvents: []string{"decision files_write require_approval", "approval files_write deny"},
			wantReason: "approval rejected: rejected at the terminal",
		},
		{
			name:       "no terminal to approve on",
			call:       toolUse(t, "toolu_1", "files_write", map[string]any{"path": "notes.md"}),
			stdin:      "y\n",
			tty:        false,
			wantEvents: []string{"decision files_write require_approval", "approval files_write deny"},
			wantReason: "approval rejected: stdin is not a terminal, so no one can approve",
		},
		{
			name:       "harness policy",
			call:       toolUse(t, "toolu_1", "files_read", map[string]any{"path": ".env"}),
			tty:        true,
			wantEvents: []string{"decision files_read deny"},
			wantReason: "no dotfiles",
		},
		{
			name:       "not granted",
			call:       toolUse(t, "toolu_1", "files_delete", map[string]any{"path": "notes.md"}),
			tty:        true,
			wantEvents: []string{"decision files_delete deny"},
			wantReason: "not granted",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, tt.call, done(t))
			f.stdin, f.tty = tt.stdin, tt.tty

			code := f.run("tidy my notes")

			require.Equal(t, 0, code, "a denial is reported to the model, the run goes on: %s", f.stderr.String())
			assert.Equal(t, "Notes are tidy.\n", f.stdout.String())
			assert.Zero(t, f.read.Calls+f.write.Calls+f.del.Calls, "a denied call never executes")
			records := f.audit(t)
			assert.Equal(t, tt.wantEvents, events(records))
			assert.Contains(t, records[len(records)-1]["reason"], tt.wantReason)
		})
	}
}

// TestInvariant_CLICredentialsNeverLeak guards trust-model guarantee 5 at the
// edge of the process: credentials reach only the model API header and the
// MCP server's environment, never stdout, stderr or the audit log, even when
// a tool and the model echo them back.
func TestInvariant_CLICredentialsNeverLeak(t *testing.T) {
	f := newFixture(t,
		toolUse(t, "toolu_1", "files_write", map[string]any{"token": filesToken}),
		anthropictest.Reply(t, "end_turn", anthropictest.TextBlock("the key is "+apiKey)),
	)
	f.write.Result = json.RawMessage(`{"echo":"` + filesToken + `","key":"` + apiKey + `"}`)

	code := f.run("leak it")

	require.Equal(t, 0, code, f.stderr.String())
	audit, err := os.ReadFile(f.path("audit.jsonl"))
	require.NoError(t, err)
	for _, secret := range []string{apiKey, filesToken} {
		assert.NotContains(t, f.stdout.String(), secret, "stdout")
		assert.NotContains(t, f.stderr.String(), secret, "stderr, approval prompt included")
		assert.NotContains(t, string(audit), secret, "audit log")
	}
	assert.Equal(t, "the key is [redacted]\n", f.stdout.String())
	assert.Equal(t, filesToken, f.started[0].Env["FILES_TOKEN"], "the server still gets its token")
	assert.Equal(t, apiKey, f.api.Requests()[0].Header.Get("X-Api-Key"))
}

func TestRun_Failures(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*fixture)
		wantLog string
	}{
		{"missing config", func(f *fixture) { require.NoError(t, os.Remove(f.path("agenty.yaml"))) }, "agenty.yaml"},
		{"missing harness", func(f *fixture) { require.NoError(t, os.Remove(f.path("harness.yaml"))) }, "harness.yaml"},
		{"missing variable", func(f *fixture) { delete(f.vars, "FILES_TOKEN") }, "FILES_TOKEN is not set"},
		{"other provider", func(f *fixture) {
			f.writeFile(t, "harness.yaml", strings.Replace(harnessYAML, "provider: anthropic", "provider: openai", 1))
		}, "is not supported; use anthropic"},
		{"granted server not configured", func(f *fixture) {
			f.writeFile(t, "harness.yaml", strings.Replace(harnessYAML, "files_write]", "files_write, chat_post]", 1))
		}, "harness grants chat_post, but no MCP server"},
		{"server does not start", func(f *fixture) { f.connectErr = assert.AnError }, assert.AnError.Error()},
		{"server lists no tools", func(f *fixture) { f.servers["files"].toolsErr = assert.AnError }, assert.AnError.Error()},
		{"invalid policy", func(f *fixture) { f.writeFile(t, "central.rego", "package agenty.tool\n\ndeny contains if {") }, "policy"},
		{"audit log cannot be opened", func(f *fixture) {
			require.NoError(t, os.Mkdir(f.path("audit.jsonl"), 0o700))
		}, "audit"},
		{"model fails", func(*fixture) {}, "no response queued"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			tt.setup(f)

			code := f.run("tidy my notes")

			assert.Equal(t, 1, code)
			assert.Empty(t, f.stdout.String())
			assert.Contains(t, f.stderr.String(), tt.wantLog)
			assert.NotContains(t, f.stderr.String(), filesToken)
		})
	}
}

func TestRun_ServerCloseFailure(t *testing.T) {
	f := newFixture(t, done(t))
	f.servers["files"].closeErr = assert.AnError

	code := f.run("tidy my notes")

	assert.Equal(t, 1, code, "a server that does not stop cleanly fails the run")
	assert.Equal(t, "Notes are tidy.\n", f.stdout.String(), "the answer is still printed")
	assert.Contains(t, f.stderr.String(), assert.AnError.Error())
}

func TestRun_StopsServersStartedBeforeAFailure(t *testing.T) {
	f := newFixture(t)
	f.writeFile(t, "harness.yaml", strings.Replace(harnessYAML, "files_write]", "files_write, mail_send]", 1))
	f.servers["mail"].toolsErr = assert.AnError

	code := f.run("tidy my notes")

	assert.Equal(t, 1, code)
	assert.True(t, f.servers["files"].closed)
	assert.True(t, f.servers["mail"].closed)
}

func TestRun_StdoutFailure(t *testing.T) {
	f := newFixture(t, done(t))
	code := cli.Main(context.Background(),
		[]string{"run", "--config", f.path("agenty.yaml"), "--harness", f.path("harness.yaml"), "--audit", f.path("audit.jsonl"), "tidy"},
		cli.Env{
			Stdin:     strings.NewReader(""),
			Stdout:    failingWriter{},
			Stderr:    &f.stderr,
			LookupEnv: func(k string) (string, bool) { v, ok := f.vars[k]; return v, ok },
			Connect: func(_ context.Context, srv mcptool.Server) (cli.ToolServer, error) {
				return f.servers[srv.Name], nil
			},
		})

	assert.Equal(t, 1, code)
	assert.Contains(t, f.stderr.String(), assert.AnError.Error())
}

func TestMain_Usage(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantOut  string
	}{
		{"no command", nil, 2, "usage: agenty run"},
		{"unknown command", []string{"walk"}, 2, `unknown command "walk"`},
		{"help", []string{"help"}, 0, "usage: agenty run"},
		{"run help", []string{"run", "-h"}, 0, "-harness"},
		{"unknown flag", []string{"run", "--nope"}, 2, "flag provided but not defined"},
		{"no harness", []string{"run", "tidy"}, 2, "--harness is required"},
		{"no input", []string{"run", "--harness", "h.yaml"}, 2, "exactly one input"},
		{"two inputs", []string{"run", "--harness", "h.yaml", "a", "b"}, 2, "exactly one input"},
		{"bad log level", []string{"run", "--harness", "h.yaml", "--log-level", "loud", "tidy"}, 2, `invalid value "loud" for flag -log-level`},
		{"run usage after a problem", []string{"run", "tidy"}, 2, "-audit file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)

			code := f.main(tt.args...)

			assert.Equal(t, tt.wantCode, code)
			assert.Contains(t, f.stderr.String(), tt.wantOut)
			assert.Empty(t, f.stdout.String())
			assert.Empty(t, f.started)
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, assert.AnError }

func TestMain_UsageWriteFailure(t *testing.T) {
	for _, args := range [][]string{nil, {"run", "tidy"}} {
		code := cli.Main(context.Background(), args, cli.Env{Stderr: failingWriter{}})

		assert.Equal(t, 1, code, "args %q", args)
	}
}
