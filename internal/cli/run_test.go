package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/cli"
	"github.com/jangraefen/agenty/internal/config"
	"github.com/jangraefen/agenty/internal/mcptool"
	"github.com/jangraefen/agenty/internal/model/anthropic/anthropictest"
	"github.com/jangraefen/agenty/internal/server"
	"github.com/jangraefen/agenty/internal/store"
	"github.com/jangraefen/agenty/internal/store/storetest"
	"github.com/jangraefen/agenty/internal/toolgateway"
	"github.com/jangraefen/agenty/internal/toolgateway/gatewaytest"
)

const (
	apiKey     = "sk-ant-api-key-0123456789"
	filesToken = "files-token-abcdef0123"
	aliceToken = "alice-token-0123456789abcdefghijklmn"
	bobToken   = "bob-token-0123456789abcdefghijklmnopq"
)

const configYAML = `provider:
  anthropic:
    api_key: {env: ANTHROPIC_API_KEY}
    max_tokens: 1024
    base_url: %s
mcp_servers:
  # The CLI runs no follow-ups: its servers stop with each run.
  files:
    command: files-mcp
    args: [--root, sandbox]
    env:
      FILES_TOKEN: {env: FILES_TOKEN}
    idle_timeout: 0s
  mail:
    command: mail-mcp
    idle_timeout: 0s
users:
  alice:
    token: {env: ALICE_TOKEN}
  bob:
    token: {env: BOB_TOKEN}
workspaces:
  home:
    members: [alice]
  work:
    members: [bob]
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

// fixture is a working directory with a config, central policy and a
// harness, a fake Anthropic API, fake MCP servers, and an agenty server on a
// fresh database, started on first use from the config as it is then.
type fixture struct {
	t       *testing.T
	dir     string
	api     *anthropictest.API
	vars    map[string]string
	stdin   string
	tty     bool
	servers map[string]*gatewaytest.Server

	read, write, del *gatewaytest.Tool

	stdout, stderr bytes.Buffer

	url        string
	store      *store.Store
	serverLogs *lockedBuffer

	mu sync.Mutex
	// configs records the MCP server config the server built for each
	// server.
	configs map[string]mcptool.Server
}

func newFixture(t *testing.T, responses ...anthropictest.Response) *fixture {
	t.Helper()
	f := &fixture{
		t:   t,
		dir: t.TempDir(),
		api: anthropictest.New(t, responses...),
		vars: map[string]string{
			"ANTHROPIC_API_KEY": apiKey, "FILES_TOKEN": filesToken, "ALICE_TOKEN": aliceToken, "BOB_TOKEN": bobToken,
			// The CLI signs in as alice, in her workspace.
			"AGENTY_TOKEN": aliceToken, "AGENTY_WORKSPACE": "home",
		},
		stdin:      "y\n",
		tty:        true,
		read:       &gatewaytest.Tool{Name: "files_read", Result: json.RawMessage(`{"content":"buy milk"}`)},
		write:      &gatewaytest.Tool{Name: "files_write", Result: json.RawMessage(`{"ok":true}`)},
		del:        &gatewaytest.Tool{Name: "files_delete", Result: json.RawMessage(`{"ok":true}`)},
		configs:    map[string]mcptool.Server{},
		serverLogs: &lockedBuffer{},
	}
	f.servers = map[string]*gatewaytest.Server{
		"files": {Tools: []toolgateway.Tool{f.read, f.write, f.del}},
		"mail":  {Tools: []toolgateway.Tool{&gatewaytest.Tool{Name: "mail_send"}}},
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

func (f *fixture) lookupEnv(k string) (string, bool) {
	v, ok := f.vars[k]
	return v, ok
}

// serverURL starts the agenty server on first use and returns its URL.
func (f *fixture) serverURL() string {
	t := f.t
	t.Helper()
	if f.url != "" {
		return f.url
	}
	cfg, err := config.Load(f.path("agenty.yaml"))
	require.NoError(t, err)
	resolved, err := cfg.Resolve(f.lookupEnv)
	require.NoError(t, err)
	f.store = storetest.New(t)
	srv, err := server.New(context.Background(), server.Config{
		Store:    f.store,
		Operator: cfg,
		Resolved: resolved,
		Logger:   slog.New(resolved.Redactor.Handler(slog.NewTextHandler(f.serverLogs, nil))),
		Server: func(name string, srv mcptool.Server) toolgateway.ToolServer {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.configs[name] = srv
			return f.servers[name]
		},
	})
	require.NoError(t, err)
	h := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		h.Close()
		srv.Close()
	})
	f.url = h.URL
	return f.url
}

// main runs the CLI with args as they are.
func (f *fixture) main(args ...string) int {
	return f.mainContext(context.Background(), strings.NewReader(f.stdin), &f.stderr, args...)
}

// mainContext runs the CLI with args until ctx ends.
func (f *fixture) mainContext(ctx context.Context, stdin io.Reader, stderr io.Writer, args ...string) int {
	return cli.Main(ctx, args, cli.Env{
		Stdin:       stdin,
		Stdout:      &f.stdout,
		Stderr:      stderr,
		Interactive: f.tty,
		LookupEnv:   f.lookupEnv,
	})
}

// apply stores the fixture's harness on the server.
func (f *fixture) apply() {
	f.t.Helper()
	require.Equal(f.t, 0, f.main("apply", "--server", f.serverURL(), f.path("harness.yaml")), f.stderr.String())
	f.stdout.Reset()
}

// run applies the harness and runs it on input, logging everything.
func (f *fixture) run(input string) int {
	f.apply()
	return f.main("run", "--server", f.serverURL(), "--log-level", "debug", "notes", input)
}

var runID = regexp.MustCompile(`run_id=(\S+)`)

// audit returns the audit records of the run the CLI started.
func (f *fixture) audit(t *testing.T) []store.AuditRecord {
	t.Helper()
	m := runID.FindStringSubmatch(f.stderr.String())
	require.NotNil(t, m, "the CLI logs the run ID")
	records, err := f.store.AuditRecords(context.Background(), m[1])
	require.NoError(t, err)
	return records
}

// events returns "<event> <tool> <decision>" for each audit record.
func events(records []store.AuditRecord) []string {
	out := make([]string, len(records))
	for i, r := range records {
		out[i] = string(r.Event) + " " + r.Tool + " " + string(r.Decision)
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
	assert.Contains(t, f.stderr.String(), "writes need a human")
	records := f.audit(t)
	assert.Equal(t, []string{
		"decision files_read allow",
		"result files_read allow",
		"decision files_write require_approval",
		"approval files_write allow",
		"result files_write allow",
	}, events(records))
	assert.Equal(t, "alice", records[3].Approver, "the signed-in user approved")
	transcript, err := f.store.Transcript(context.Background(), records[0].RunID)
	require.NoError(t, err)
	require.Len(t, transcript, 6)
	require.NotNil(t, transcript[1].Provider, "the model's reply is stored in the provider's own form too")
	assert.Equal(t, "anthropic", transcript[1].Provider.Name)
	assert.Contains(t, string(transcript[1].Provider.Data), `"toolu_1"`)
	assert.Equal(t, "approved at the terminal", records[3].Reason)
	assert.Contains(t, f.stderr.String(), "tool=files_write decision=require_approval", "debug logs show each tool call")
	assert.Contains(t, f.stderr.String(), "run finished")
	f.mu.Lock()
	assert.Equal(t, mcptool.Server{
		Command: "files-mcp",
		Args:    []string{"--root", "sandbox"},
		Env:     map[string]string{"FILES_TOKEN": filesToken},
	}, f.configs["files"])
	f.mu.Unlock()
	assert.Equal(t, []string{"files"}, f.servers["files"].StartedAs)
	assert.Empty(t, f.servers["mail"].StartedAs, "only servers whose tools the harness grants are started")
	assert.Equal(t, 1, f.servers["files"].Closed, "servers are stopped when the run ends")
	assert.NotContains(t, f.stderr.String(), "WARN")
}

func TestRun_LargeToolResults(t *testing.T) {
	f := newFixture(t,
		toolUse(t, "toolu_1", "files_read", map[string]any{"path": "big.md"}),
		done(t),
	)
	f.read.Result = json.RawMessage(`{"content":"` + strings.Repeat("x", 256<<10) + `"}`)

	code := f.run("tidy my notes")

	require.Equal(t, 0, code, "an event larger than a line buffer is still read: %s", f.stderr.String())
	assert.Equal(t, "Notes are tidy.\n", f.stdout.String())
}

// TestRun_EscapesTheAnswer: the model writes the answer, so it must not be
// able to send terminal escape sequences.
func TestRun_EscapesTheAnswer(t *testing.T) {
	f := newFixture(t, anthropictest.Reply(t, "end_turn", anthropictest.TextBlock("done\x1b[2J\nnext line\tok")))

	code := f.run("tidy my notes")

	require.Equal(t, 0, code, f.stderr.String())
	assert.Equal(t, "done\\u001b[2J\nnext line\tok\n", f.stdout.String())
}

func TestApply(t *testing.T) {
	f := newFixture(t)
	url := f.serverURL()

	require.Equal(t, 0, f.main("apply", "--server", url, f.path("harness.yaml")), f.stderr.String())
	require.Equal(t, 0, f.main("apply", "--server", url, f.path("harness.yaml")), f.stderr.String())
	f.writeFile(t, "harness.yaml", strings.Replace(harnessYAML, "Keep the notes tidy.", "Keep the notes tidy and short.", 1))
	require.Equal(t, 0, f.main("apply", "--server", url, f.path("harness.yaml")), f.stderr.String())

	assert.Equal(t, "notes version 1\nnotes version 1\nnotes version 2\n", f.stdout.String())
	v, err := f.store.Harness(context.Background(), "home", "notes")
	require.NoError(t, err)
	require.Len(t, v.Harness.Policy, 1, "the harness's policy is sent with it")
	assert.Contains(t, v.Harness.Policy[0].Source, "no dotfiles")
}

func TestApply_Failures(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*fixture)
		args    func(*fixture) []string
		wantLog string
	}{
		{"missing file", nil, func(f *fixture) []string { return []string{"--server", f.serverURL(), f.path("nope.yaml")} }, "nope.yaml"},
		{"invalid harness", func(f *fixture) {
			f.writeFile(t, "harness.yaml", strings.Replace(harnessYAML, "max_steps: 5", "max_steps: 0", 1))
		}, func(f *fixture) []string { return []string{"--server", f.serverURL(), f.path("harness.yaml")} }, "limits.max_steps"},
		{"policy that does not compile", func(f *fixture) {
			f.writeFile(t, "harness.yaml", strings.Replace(harnessYAML, `startswith(input.args.path, ".")`, `startswith(`, 1))
		}, func(f *fixture) []string { return []string{"--server", f.serverURL(), f.path("harness.yaml")} }, "server: invalid harness"},
		{"unreachable server", nil, func(f *fixture) []string { return []string{"--server", "http://127.0.0.1:1", f.path("harness.yaml")} }, "connection refused"},
		{"server that is not a URL", nil, func(f *fixture) []string { return []string{"--server", "localhost:8080", f.path("harness.yaml")} }, "must be an http or https URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			if tt.setup != nil {
				tt.setup(f)
			}

			code := f.main(append([]string{"apply"}, tt.args(f)...)...)

			assert.Equal(t, 1, code)
			assert.Empty(t, f.stdout.String())
			assert.Contains(t, f.stderr.String(), tt.wantLog)
		})
	}
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
			assert.Contains(t, records[len(records)-1].Reason, tt.wantReason)
		})
	}
}

// TestInvariant_CLICredentialsNeverLeak guards trust-model guarantee 5 from
// end to end: credentials reach only the model API header and the MCP
// server's environment, never the CLI's stdout or stderr, the server's log
// or the audit log, even when a tool and the model echo them back.
func TestInvariant_CLICredentialsNeverLeak(t *testing.T) {
	f := newFixture(t,
		toolUse(t, "toolu_1", "files_write", map[string]any{"token": filesToken}),
		anthropictest.Reply(t, "end_turn", anthropictest.TextBlock("the key is "+apiKey)),
	)
	f.write.Result = json.RawMessage(`{"echo":"` + filesToken + `","key":"` + apiKey + `"}`)

	code := f.run("leak it")

	require.Equal(t, 0, code, f.stderr.String())
	audit, err := json.Marshal(f.audit(t))
	require.NoError(t, err)
	for _, secret := range []string{apiKey, filesToken, aliceToken} {
		assert.NotContains(t, f.stdout.String(), secret, "stdout")
		assert.NotContains(t, f.stderr.String(), secret, "stderr, approval prompt included")
		assert.NotContains(t, f.serverLogs.String(), secret, "server log")
		assert.NotContains(t, string(audit), secret, "audit log")
	}
	assert.Equal(t, "the key is [redacted]\n", f.stdout.String())
	f.mu.Lock()
	assert.Equal(t, filesToken, f.configs["files"].Env["FILES_TOKEN"], "the server still gets its token")
	f.mu.Unlock()
	assert.Equal(t, apiKey, f.api.Requests()[0].Header.Get("X-Api-Key"))
}

func TestRun_Failures(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*fixture)
		wantLog string
	}{
		{"other provider", func(f *fixture) {
			f.writeFile(t, "harness.yaml", strings.Replace(harnessYAML, "provider: anthropic", "provider: openai", 1))
		}, "is not supported; use anthropic"},
		{"granted server not configured", func(f *fixture) {
			f.writeFile(t, "harness.yaml", strings.Replace(harnessYAML, "files_write]", "files_write, chat_post]", 1))
		}, "grant chat_post: no server"},
		{"granted tool not served", func(f *fixture) {
			f.servers["files"].Tools = []toolgateway.Tool{f.read}
		}, "grant files_write: server files has no such tool"},
		{"server does not start", func(f *fixture) { f.servers["files"].StartErr = assert.AnError }, assert.AnError.Error()},
		{"failure that carries a secret is reported redacted", func(f *fixture) {
			f.servers["files"].StartErr = errors.New("login with " + filesToken + " refused")
		}, "login with [redacted] refused"},
		{"server lists no tools", func(f *fixture) { f.servers["files"].ToolsErr = assert.AnError }, assert.AnError.Error()},
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

func TestClients_SignIn(t *testing.T) {
	tests := []struct {
		name     string
		vars     map[string]string
		args     []string
		wantCode int
		wantLog  string
	}{
		{"no token", map[string]string{"AGENTY_TOKEN": ""}, nil, 2, "AGENTY_TOKEN is not set"},
		{"no workspace", map[string]string{"AGENTY_WORKSPACE": ""}, nil, 2, "--workspace or AGENTY_WORKSPACE is required"},
		{"unknown token", map[string]string{"AGENTY_TOKEN": "mallory-token-0123456789abcdefghij"}, nil, 1, "server: sign in with a bearer token"},
		{"token too short to redact", map[string]string{"AGENTY_TOKEN": "short"}, nil, 1, "AGENTY_TOKEN"},
		{"workspace the user is not in", nil, []string{"--workspace", "work"}, 1, "workspace work: not found"},
		{"another user's workspace from the environment", map[string]string{"AGENTY_WORKSPACE": "work"}, nil, 1, "workspace work: not found"},
		{"the flag wins over the environment", map[string]string{"AGENTY_WORKSPACE": "work"}, []string{"--workspace", "home"}, 0, ""},
	}
	for _, tt := range tests {
		for _, cmd := range []string{"apply", "run"} {
			t.Run(cmd+" with "+tt.name, func(t *testing.T) {
				f := newFixture(t, done(t))
				url := f.serverURL()
				if cmd == "run" {
					f.apply()
				}
				for k, v := range tt.vars {
					f.vars[k] = v
				}
				args := append([]string{cmd, "--server", url}, tt.args...)
				if cmd == "apply" {
					args = append(args, f.path("harness.yaml"))
				} else {
					args = append(args, "notes", "tidy")
				}

				code := f.main(args...)

				assert.Equal(t, tt.wantCode, code, f.stderr.String())
				assert.Contains(t, f.stderr.String(), tt.wantLog)
				for _, token := range []string{aliceToken, "mallory-token-0123456789abcdefghij"} {
					assert.NotContains(t, f.stderr.String(), token)
				}
			})
		}
	}
}

func TestRun_StartedByTheSignedInUser(t *testing.T) {
	f := newFixture(t, done(t))

	require.Equal(t, 0, f.run("tidy my notes"), f.stderr.String())

	m := runID.FindStringSubmatch(f.stderr.String())
	require.NotNil(t, m)
	run, err := f.store.Run(context.Background(), "home", m[1])
	require.NoError(t, err)
	assert.Equal(t, "alice", run.StartedBy)
}

func TestRun_InterruptCancelsTheRun(t *testing.T) {
	f := newFixture(t, toolUse(t, "toolu_1", "files_write", map[string]any{"path": "notes.md"}), done(t))
	f.apply()
	stdin, answers := io.Pipe() // no one ever answers
	t.Cleanup(func() { assert.NoError(t, answers.Close()) })
	stderr := &lockedBuffer{}
	ctx, interrupt := context.WithCancel(context.Background())
	defer interrupt()
	exit := make(chan int, 1)

	go func() { exit <- f.mainContext(ctx, stdin, stderr, "run", "--server", f.serverURL(), "notes", "tidy") }()
	require.Eventually(t, func() bool { return strings.Contains(stderr.String(), "Approve?") }, 10*time.Second, 20*time.Millisecond)
	interrupt()

	select {
	case code := <-exit:
		assert.Equal(t, 1, code)
	case <-time.After(10 * time.Second):
		t.Fatal("run did not stop")
	}
	m := runID.FindStringSubmatch(stderr.String())
	require.NotNil(t, m)
	require.Eventually(t, func() bool {
		r, err := f.store.Run(context.Background(), "home", m[1])
		return err == nil && r.Status == store.RunCancelled
	}, 10*time.Second, 20*time.Millisecond, "the server cancels the run")
	r, err := f.store.Run(context.Background(), "home", m[1])
	require.NoError(t, err)
	assert.Equal(t, "cancelled by alice", r.Error)
	assert.Zero(t, f.write.Calls)
}

func TestRun_UnknownHarnessOrServer(t *testing.T) {
	f := newFixture(t)

	assert.Equal(t, 1, f.main("run", "--server", f.serverURL(), "ghost", "tidy"))
	assert.Contains(t, f.stderr.String(), "harness ghost: not found")
	assert.Equal(t, 1, f.main("run", "--server", "http://127.0.0.1:1", "notes", "tidy"))
	assert.Contains(t, f.stderr.String(), "connection refused")
}

func TestRun_ServerCloseFailure(t *testing.T) {
	f := newFixture(t, done(t))
	f.servers["files"].CloseErr = assert.AnError

	code := f.run("tidy my notes")

	assert.Equal(t, 1, code, "a server that does not stop cleanly fails the run")
	assert.Equal(t, "Notes are tidy.\n", f.stdout.String(), "the answer is still printed")
	assert.Contains(t, f.stderr.String(), assert.AnError.Error())
}

func TestRun_StopsServersStartedBeforeAFailure(t *testing.T) {
	f := newFixture(t)
	f.writeFile(t, "harness.yaml", strings.Replace(harnessYAML, "files_write]", "files_write, mail_send]", 1))
	f.servers["mail"].ToolsErr = assert.AnError

	code := f.run("tidy my notes")

	assert.Equal(t, 1, code)
	assert.Equal(t, 1, f.servers["files"].Closed)
	assert.Equal(t, 1, f.servers["mail"].Closed)
}

func TestRun_StdoutFailure(t *testing.T) {
	f := newFixture(t, done(t))
	f.apply()

	code := cli.Main(context.Background(), []string{"run", "--server", f.serverURL(), "notes", "tidy"}, cli.Env{
		Stdin:     strings.NewReader(""),
		Stdout:    failingWriter{},
		Stderr:    &f.stderr,
		LookupEnv: f.lookupEnv,
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
		{"no command", nil, 2, "usage: agenty <command>"},
		{"unknown command", []string{"walk"}, 2, `unknown command "walk"`},
		{"help", []string{"help"}, 0, "serve   serves the HTTP API"},
		{"run help", []string{"run", "-h"}, 0, "usage: agenty run [flags] HARNESS INPUT"},
		{"apply help", []string{"apply", "-h"}, 0, "usage: agenty apply [flags] FILE"},
		{"unknown flag", []string{"run", "--nope"}, 2, "flag provided but not defined"},
		{"no input", []string{"run", "notes"}, 2, "expected 2 arguments, got 1"},
		{"three arguments", []string{"run", "notes", "a", "b"}, 2, "expected 2 arguments, got 3"},
		{"no file", []string{"apply"}, 2, "expected 1 arguments, got 0"},
		{"bad log level", []string{"run", "--log-level", "loud", "notes", "tidy"}, 2, `invalid value "loud" for flag -log-level`},
		{"run usage after a problem", []string{"run", "tidy"}, 2, "-server URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)

			code := f.main(tt.args...)

			assert.Equal(t, tt.wantCode, code)
			assert.Contains(t, f.stderr.String(), tt.wantOut)
			assert.Empty(t, f.stdout.String())
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, assert.AnError }

func TestMain_UsageWriteFailure(t *testing.T) {
	for _, args := range [][]string{nil, {"run", "tidy"}, {"serve", "x"}} {
		code := cli.Main(context.Background(), args, cli.Env{Stderr: failingWriter{}})

		assert.Equal(t, 1, code, "args %q", args)
	}
}
