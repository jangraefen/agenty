package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/api"
	"github.com/jangraefen/agenty/internal/cli"
	"github.com/jangraefen/agenty/internal/mcptool"
	"github.com/jangraefen/agenty/internal/store/storetest"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

// lockedBuffer is a bytes.Buffer safe for a server logging while a test
// reads it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (f *fixture) serve(ctx context.Context, stderr *lockedBuffer, args ...string) int {
	return cli.Main(ctx, append([]string{"serve"}, args...), cli.Env{
		Stdout: &f.stdout,
		Stderr: stderr,
		LookupEnv: func(k string) (string, bool) {
			v, ok := f.vars[k]
			return v, ok
		},
		Server: func(name string, _ mcptool.Server) toolgateway.ToolServer { return f.servers[name] },
	})
}

var servingAddr = regexp.MustCompile(`addr=(http://\S+)`)

func TestServe_ServesTheAPIUntilStopped(t *testing.T) {
	_, url := storetest.NewWithURL(t)
	f := newFixture(t)
	f.writeFile(t, "agenty.yaml", strings.Replace(configYAML, "%s", f.api.URL, 1))
	f.vars["DATABASE_URL"] = url
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stderr := &lockedBuffer{}
	exit := make(chan int, 1)

	go func() { exit <- f.serve(ctx, stderr, "--config", f.path("agenty.yaml"), "--addr", "127.0.0.1:0") }()
	var base string
	require.Eventually(t, func() bool {
		m := servingAddr.FindStringSubmatch(stderr.String())
		if m != nil {
			base = m[1]
		}
		return m != nil
	}, 10*time.Second, 20*time.Millisecond, "the server logs where it listens")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/me", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	var me api.Me
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&me))
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, api.Me{User: "alice", Workspaces: []string{"home"}, Auditor: true}, me)

	cancel()
	select {
	case code := <-exit:
		assert.Equal(t, 0, code, stderr.String())
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not stop")
	}
	assert.NotContains(t, stderr.String(), url, "the database URL holds a password and is redacted")
	assert.Contains(t, stderr.String(), "user=alice", "requests log who made them")
}

func TestServe_Failures(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		setup    func(*fixture)
		wantCode int
		wantLog  string
	}{
		{"address another machine can reach", []string{"--addr", "0.0.0.0:8080"}, nil, 1, "listens on a loopback address only"},
		{"all interfaces", []string{"--addr", ":8080"}, nil, 1, "listens on a loopback address only"},
		{"host name", []string{"--addr", "example.com:80"}, nil, 1, "listens on a loopback address only"},
		{"malformed address", []string{"--addr", "127.0.0.1"}, nil, 1, "--addr"},
		{"no database", nil, func(f *fixture) {
			f.writeFile(t, "agenty.yaml", strings.Replace(strings.Replace(configYAML, "%s", f.api.URL, 1), "database:\n  url: {env: DATABASE_URL}\n", "", 1))
		}, 1, "database.url"},
		{"no users", nil, func(f *fixture) {
			cfg := strings.Replace(configYAML, "%s", f.api.URL, 1)
			f.writeFile(t, "agenty.yaml", cfg[:strings.Index(cfg, "users:")]+cfg[strings.Index(cfg, "policy:"):])
			f.vars["DATABASE_URL"] = "postgres://nobody:secret-password-1@127.0.0.1:1/none?connect_timeout=1"
		}, 1, "no one could sign in"},
		{"malformed config", nil, func(f *fixture) { f.writeFile(t, "agenty.yaml", "{") }, 1, "agenty.yaml"},
		{"unset database variable", nil, func(f *fixture) {
			f.writeFile(t, "agenty.yaml", strings.Replace(configYAML, "%s", f.api.URL, 1))
			delete(f.vars, "DATABASE_URL")
		}, 1, "DATABASE_URL is not set"},
		{"unreachable database", nil, func(f *fixture) {
			f.writeFile(t, "agenty.yaml", strings.Replace(configYAML, "%s", f.api.URL, 1))
			f.vars["DATABASE_URL"] = "postgres://nobody:secret-password-1@127.0.0.1:1/none?connect_timeout=1"
		}, 1, "store"},
		{"invalid central policy", nil, func(f *fixture) {
			_, url := storetest.NewWithURL(t)
			f.writeFile(t, "agenty.yaml", strings.Replace(configYAML, "%s", f.api.URL, 1))
			f.writeFile(t, "central.rego", "package agenty.tool\n\ndeny contains if {")
			f.vars["DATABASE_URL"] = url
		}, 1, "central"},
		{"arguments", []string{"extra"}, nil, 2, "no arguments expected"},
		{"unknown flag", []string{"--bogus"}, nil, 2, "bogus"},
		{"help", []string{"-h"}, nil, 0, "Serves the HTTP API on localhost"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			if tt.setup != nil {
				tt.setup(f)
			}
			stderr := &lockedBuffer{}

			code := f.serve(context.Background(), stderr, append([]string{"--config", f.path("agenty.yaml")}, tt.args...)...)

			assert.Equal(t, tt.wantCode, code)
			assert.Contains(t, stderr.String(), tt.wantLog)
			assert.NotContains(t, stderr.String(), "secret-password-1")
		})
	}
}
