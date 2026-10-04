//go:build module

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type started struct {
	addr   string
	cancel context.CancelFunc
	code   <-chan int
	stderr *bytes.Buffer
}

// start runs the server in-process with the given roles and returns once all
// roles are set up; addr is set only when the api role listens.
func start(t *testing.T, roleList string) started {
	t.Helper()
	return startWithConfig(t, roleList, "")
}

// startWithConfig is start with extra top-level configuration appended.
func startWithConfig(t *testing.T, roleList, extraConfig string) started {
	t.Helper()
	path := writeConfig(t, "server:\n  address: 127.0.0.1:0\n  shutdownTimeout: 5s\n"+extraConfig)
	ctx, cancel := context.WithCancel(context.Background())
	startedAt := make(chan net.Addr, 1)
	code := make(chan int, 1)
	var stdout, stderr bytes.Buffer
	go func() {
		code <- runContext(ctx, []string{"--config", path, "--roles", roleList}, &stdout, &stderr,
			func(a net.Addr) { startedAt <- a })
	}()
	s := started{cancel: cancel, code: code, stderr: &stderr}
	select {
	case a := <-startedAt:
		if a != nil {
			s.addr = a.String()
		}
	case c := <-code:
		require.Fail(t, "server exited early", "code %d: %s", c, stderr.String())
	case <-time.After(10 * time.Second):
		cancel()
		require.Fail(t, "server did not start")
	}
	t.Cleanup(cancel)
	return s
}

func (s started) stop(t *testing.T) {
	t.Helper()
	s.cancel()
	select {
	case c := <-s.code:
		assert.Equal(t, 0, c, "exit code (stderr: %s)", s.stderr.String())
	case <-time.After(10 * time.Second):
		require.Fail(t, "server did not stop")
	}
}

func healthRoles(t *testing.T, addr string) []string {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+addr+"/healthz", http.NoBody)
	require.NoError(t, err, "request")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "GET /healthz")
	defer func() { _ = resp.Body.Close() }()
	var body struct {
		Roles []string `json:"roles"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body), "decode /healthz")
	return body.Roles
}

func TestServeReportsSelectedRoles(t *testing.T) {
	tests := []struct {
		roles string
		want  []string
	}{
		{"api", []string{"api"}},
		{"api,worker", []string{"api", "worker"}},
		{"scheduler,api", []string{"api", "scheduler"}},
		{"api,worker,scheduler", []string{"api", "worker", "scheduler"}},
	}
	for _, tt := range tests {
		t.Run(tt.roles, func(t *testing.T) {
			s := start(t, tt.roles)
			require.NotEmpty(t, s.addr, "api role did not listen")

			got := healthRoles(t, s.addr)

			assert.Equal(t, tt.want, got, "roles")
			s.stop(t)
		})
	}
}

func TestServeLogsInConfiguredFormat(t *testing.T) {
	tests := []struct {
		format string
		check  func(t *testing.T, line string)
	}{
		{"json", func(t *testing.T, line string) {
			var rec map[string]any
			require.NoError(t, json.Unmarshal([]byte(line), &rec), "line %q", line)
			assert.Equal(t, "info", rec["level"])
		}},
		{"logfmt", func(t *testing.T, line string) {
			assert.True(t, strings.HasPrefix(line, "time="), "line %q", line)
			assert.Contains(t, line, "level=info")
		}},
		{"text", func(t *testing.T, line string) {
			assert.Contains(t, line, "INFO")
			assert.False(t, json.Valid([]byte(line)), "line %q", line)
			assert.NotContains(t, line, "level=")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			s := startWithConfig(t, "api", "log:\n  format: "+tt.format+"\n")
			healthRoles(t, s.addr)
			s.stop(t)

			lines := strings.Split(strings.TrimSpace(s.stderr.String()), "\n")
			var requestLogged bool
			for _, line := range lines {
				tt.check(t, line)
				requestLogged = requestLogged || strings.Contains(line, "/healthz")
			}
			assert.True(t, requestLogged, "no request log for /healthz in %q", s.stderr.String())
			assert.Contains(t, s.stderr.String(), "api role listening")
		})
	}
}

func TestServeWithoutAPIRoleDoesNotListen(t *testing.T) {
	for _, roleList := range []string{"worker", "scheduler", "worker,scheduler"} {
		t.Run(roleList, func(t *testing.T) {
			s := start(t, roleList)

			assert.Empty(t, s.addr, "listening on %s without the api role", s.addr)
			s.stop(t)
		})
	}
}

// startBinary builds and starts the agenty binary with the given server
// configuration (address is appended) and returns once the api role listens.
func startBinary(t *testing.T, serverConfig string) (cmd *exec.Cmd, addr string, logs *bufio.Scanner) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "agenty")
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	require.NoError(t, err, "go build:\n%s", out)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "pick port")
	addr = ln.Addr().String()
	_ = ln.Close()
	path := writeConfig(t, "server:\n  address: "+addr+"\n"+serverConfig)
	cmd = exec.Command(bin, "--config", path)
	stderr, err := cmd.StderrPipe()
	require.NoError(t, err, "stderr pipe")
	require.NoError(t, cmd.Start(), "start")
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	logs = bufio.NewScanner(stderr)
	waitForLog(t, logs, "listening")
	return cmd, addr, logs
}

func waitForLog(t *testing.T, logs *bufio.Scanner, fragment string) {
	t.Helper()
	for logs.Scan() {
		if strings.Contains(logs.Text(), fragment) {
			return
		}
	}
	require.Fail(t, "log output ended", "no line containing %q", fragment)
}

func waitExit(t *testing.T, cmd *exec.Cmd, within time.Duration) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(within):
		require.Fail(t, "binary did not exit", "within %s", within)
		return nil
	}
}

func TestBinaryShutsDownOnSIGTERM(t *testing.T) {
	cmd, addr, logs := startBinary(t, "")

	assert.Equal(t, []string{"api", "worker", "scheduler"}, healthRoles(t, addr), "default roles, want all")
	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM), "signal")
	for logs.Scan() {
	}
	assert.NoError(t, waitExit(t, cmd, 10*time.Second), "exit after SIGTERM")
}

func TestBinaryExitsImmediatelyOnSecondSignal(t *testing.T) {
	cmd, addr, logs := startBinary(t, "  shutdownTimeout: 60s\n")
	// A request with incomplete headers keeps a connection active, so the
	// graceful shutdown waits for it (until the read-header timeout).
	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err, "dial")
	t.Cleanup(func() { _ = conn.Close() })
	_, err = conn.Write([]byte("GET /healthz HTTP/1.1\r\n"))
	require.NoError(t, err, "write partial request")

	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM), "first signal")
	waitForLog(t, logs, "send the signal again")
	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM), "second signal")
	go func() {
		for logs.Scan() {
		}
	}()

	_ = waitExit(t, cmd, 5*time.Second)
	ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	assert.True(t, ok && ws.Signaled() && ws.Signal() == syscall.SIGTERM,
		"process state = %v, want terminated by SIGTERM", cmd.ProcessState)
}
