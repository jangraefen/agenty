package server_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/api"
	"github.com/jangraefen/agenty/internal/config"
	"github.com/jangraefen/agenty/internal/harness"
	"github.com/jangraefen/agenty/internal/mcptool"
	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/model/modeltest"
	"github.com/jangraefen/agenty/internal/policy"
	"github.com/jangraefen/agenty/internal/secret"
	"github.com/jangraefen/agenty/internal/server"
	"github.com/jangraefen/agenty/internal/store"
	"github.com/jangraefen/agenty/internal/store/storetest"
	"github.com/jangraefen/agenty/internal/toolgateway"
	"github.com/jangraefen/agenty/internal/toolgateway/gatewaytest"
)

// token is the files server's credential; the redactor knows it.
const token = "files-token-0123456789"

// The users' bearer tokens. alice, bob and dana share the home workspace;
// only bob is in work, and carol is in none. dana is the auditor.
const (
	aliceToken = "alice-token-0123456789abcdefghijklmn"
	bobToken   = "bob-token-0123456789abcdefghijklmnopq"
	carolToken = "carol-token-0123456789abcdefghijklmn"
	danaToken  = "dana-token-0123456789abcdefghijklmnop"
)

// home is the path of alice's workspace, where the tests work.
const home = "/v1/workspaces/home"

// origin is the web portal's origin, which may call the API from a browser.
const origin = "http://localhost:5173"

// fixture is a server on a fresh database, with a fake files MCP server and
// scripted models handed out one per run.
type fixture struct {
	store  *store.Store
	dbURL  string
	server *server.Server
	http   *httptest.Server
	logs   *syncBuffer

	read  *gatewaytest.Tool
	write *gatewaytest.Tool
	files *gatewaytest.Server

	mu     sync.Mutex
	models []model.Model
}

type options struct {
	policy   []policy.Module
	newModel func(harness.Model) (model.Model, error)
	// configuredModel uses the server's own provider instead of scripted
	// models.
	configuredModel bool
	approvalTimeout time.Duration
	// workers is how many runs execute at once; unset means the default.
	workers int
	// serverIdleTimeout is the files server's idle timeout; unset means the
	// default.
	serverIdleTimeout *time.Duration
}

func newFixture(t *testing.T, opts options) *fixture {
	t.Helper()
	f := &fixture{
		logs:  &syncBuffer{},
		read:  &gatewaytest.Tool{Name: "files_read", Result: json.RawMessage(`{"content":"- milk"}`)},
		write: &gatewaytest.Tool{Name: "files_write", Result: json.RawMessage(`{"ok":true}`)},
	}
	f.store, f.dbURL = storetest.NewWithURL(t)
	f.files = &gatewaytest.Server{Tools: []toolgateway.Tool{f.read, f.write}}
	f.serve(t, opts)
	return f
}

// restart stops the fixture's server and serves its database with a new one,
// configured by opts, as an operator restarting agenty with a changed config.
func (f *fixture) restart(t *testing.T, opts options) {
	t.Helper()
	// The server first: it ends the event streams the HTTP server waits for.
	f.server.Close()
	f.http.Close()
	f.serve(t, opts)
}

// serve starts the fixture's server on its database.
func (f *fixture) serve(t *testing.T, opts options) {
	t.Helper()
	redactor, err := secret.NewRedactor([]string{token, aliceToken, bobToken, carolToken, danaToken})
	require.NoError(t, err)
	newModel := opts.newModel
	switch {
	case opts.configuredModel:
		newModel = nil
	case newModel == nil:
		newModel = f.nextModel
	}
	f.server, err = server.New(context.Background(), server.Config{
		Store: f.store,
		Operator: &config.Config{
			MCPServers: map[string]config.MCPServer{"files": {
				Command:     "unused",
				Args:        []string{"--db", "postgres://files:" + token + "@localhost/files"},
				Env:         map[string]config.Value{"FILES_TOKEN": {Env: "FILES_TOKEN"}},
				IdleTimeout: opts.serverIdleTimeout,
			}},
			Policy: opts.policy,
			Users:  map[string]config.User{"alice": {}, "bob": {}, "carol": {}, "dana": {Auditor: true}},
			Workspaces: map[string]config.Workspace{
				"home": {Members: []string{"alice", "bob", "dana"}},
				"work": {Members: []string{"bob"}},
			},
			CORS:      config.CORS{Origins: []string{origin}},
			Runs:      config.Runs{Workers: opts.workers},
			Approvals: config.Approvals{Timeout: opts.approvalTimeout},
		},
		Resolved: &config.Resolved{Redactor: redactor, UserTokens: userTokens},
		Logger:   slog.New(redactor.Handler(slog.NewTextHandler(f.logs, nil))),
		NewModel: newModel,
		Server: func(name string, _ mcptool.Server) toolgateway.ToolServer {
			assert.Equal(t, "files", name)
			return f.files
		},
	})
	require.NoError(t, err)
	f.http = newHTTP(t, f.server)
}

// userTokens are the users' tokens, as config.Resolve returns them.
var userTokens = map[string]string{"alice": aliceToken, "bob": bobToken, "carol": carolToken, "dana": danaToken}

// newHTTP serves srv until the test ends, then closes it.
func newHTTP(t *testing.T, srv *server.Server) *httptest.Server {
	t.Helper()
	h := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		h.Close()
		srv.Close()
	})
	return h
}

// storeRunning stores r as a run that a worker has claimed but no server
// runs, as a server that stopped leaves it.
func (f *fixture) storeRunning(t *testing.T, r store.NewRun) {
	t.Helper()
	ctx := context.Background()
	_, err := f.store.CreateRun(ctx, r)
	require.NoError(t, err)
	claimed, ok, err := f.store.ClaimRun(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, r.ID, claimed.ID)
}

// script queues a scripted model for the next run.
func (f *fixture) script(steps ...modeltest.Step) *modeltest.Scripted {
	m := modeltest.NewScripted(steps...)
	f.mu.Lock()
	f.models = append(f.models, m)
	f.mu.Unlock()
	return m
}

func (f *fixture) nextModel(harness.Model) (model.Model, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.models) == 0 {
		return modeltest.NewScripted(), nil
	}
	m := f.models[0]
	f.models = f.models[1:]
	return m, nil
}

// notes is a harness that may read and write files.
func notes() harness.Harness {
	return harness.Harness{
		Name:         "notes",
		Instructions: "Tidy the notes.",
		Model:        harness.Model{Provider: "anthropic", Name: "claude-test"},
		Tools:        []string{"files_read", "files_write"},
		Limits:       harness.Limits{MaxSteps: 5, MaxToolCalls: 10},
	}
}

// do sends a JSON request as alice and decodes the JSON response into out, if
// any.
func (f *fixture) do(t *testing.T, method, path string, body, out any) int {
	t.Helper()
	return f.doAs(t, aliceToken, method, path, body, out)
}

// doAs is do with the bearer token of another user, or none if it is empty.
func (f *fixture) doAs(t *testing.T, bearer, method, path string, body, out any) int {
	t.Helper()
	var r io.Reader
	if s, ok := body.(string); ok {
		r = strings.NewReader(s)
	} else if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, f.http.URL+path, r)
	require.NoError(t, err)
	if r != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { assert.NoError(t, resp.Body.Close()) }()
	got := readChecked(t, req, resp)
	if out != nil {
		require.NoError(t, json.NewDecoder(got).Decode(out))
	}
	return resp.StatusCode
}

// audit returns a run's audit records, as an auditor reads them.
func (f *fixture) audit(t *testing.T, runID string) []api.AuditRecord {
	t.Helper()
	var detail api.AuditRunDetail
	require.Equal(t, http.StatusOK, f.doAs(t, danaToken, http.MethodGet, "/v1/audit/runs/"+runID, nil, &detail))
	return detail.Records
}

// putNotes stores the notes harness.
func (f *fixture) putNotes(t *testing.T) {
	t.Helper()
	require.Equal(t, http.StatusOK, f.do(t, http.MethodPut, home+"/harnesses/notes", notes(), nil))
}

// startRun starts a run of the notes harness.
func (f *fixture) startRun(t *testing.T, input string) api.Run {
	t.Helper()
	var run api.Run
	require.Equal(t, http.StatusCreated, f.do(t, http.MethodPost, home+"/runs", api.CreateRun{Harness: "notes", Input: input}, &run))
	return run
}

// sse is one server-sent event.
type sse struct {
	name string
	data string
}

// stream reads a run's server-sent events.
type stream struct {
	t      *testing.T
	body   io.ReadCloser
	lines  *bufio.Scanner
	events chan sse
}

func (f *fixture) events(t *testing.T, runID string) *stream {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, f.http.URL+home+"/runs/"+runID+"/events", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.True(t, strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream"), resp.Header.Get("Content-Type"))
	checkContract(t, req, resp, nil)
	s := &stream{t: t, body: resp.Body, lines: bufio.NewScanner(resp.Body), events: make(chan sse)}
	t.Cleanup(func() { assert.NoError(t, resp.Body.Close()) })
	go s.read()
	return s
}

func (s *stream) read() {
	defer close(s.events)
	var e sse
	for s.lines.Scan() {
		line := s.lines.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			e.name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			e.data += strings.TrimPrefix(line, "data:")
		case line == "" && e.name != "":
			s.events <- e
			e = sse{}
		}
	}
}

// next returns the next event, checked against the spec, failing after a
// few seconds.
func (s *stream) next() sse {
	s.t.Helper()
	select {
	case e, ok := <-s.events:
		require.True(s.t, ok, "the stream ended early")
		checkEvent(s.t, e.name, e.data)
		return e
	case <-time.After(5 * time.Second):
		s.t.Fatal("no event within 5s")
		return sse{}
	}
}

// rest returns every remaining event until the stream ends.
func (s *stream) rest() []sse {
	s.t.Helper()
	var out []sse
	for {
		select {
		case e, ok := <-s.events:
			if !ok {
				return out
			}
			checkEvent(s.t, e.name, e.data)
			out = append(out, e)
		case <-time.After(5 * time.Second):
			s.t.Fatal("the stream did not end within 5s")
		}
	}
}

func decodeAs[T any](t *testing.T, e sse) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal([]byte(e.data), &v), e.data)
	return v
}

// syncBuffer is a bytes.Buffer safe for concurrent logging.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
