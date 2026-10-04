//go:build module

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/server/internal/testinfra/pgtest"
)

type started struct {
	addr   string
	cancel context.CancelFunc
	code   <-chan int
	stderr *bytes.Buffer

	startedAt <-chan net.Addr
}

// pg is the PostgreSQL server shared by all tests in this package.
var pg *pgtest.Server

func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m, &pg))
}

// databaseConfig is the configuration section for the database at dsn.
func databaseConfig(dsn string) string {
	return "database:\n  url: " + strconv.Quote(dsn) + "\n"
}

// start runs the server in-process with the given roles against a new
// database and returns once all roles are set up; addr is set only when the
// api role listens.
func start(t *testing.T, roleList string) started {
	t.Helper()
	return startOn(t, roleList, pg.NewDatabase(t))
}

// startOn is start against the database at dsn.
func startOn(t *testing.T, roleList, dsn string) started {
	t.Helper()
	return launch(t, roleList, dsn).await(t)
}

// launch runs the server in-process without waiting for it to start.
func launch(t *testing.T, roleList, dsn string) started {
	t.Helper()
	path := writeConfig(t, "server:\n  address: 127.0.0.1:0\n  shutdownTimeout: 5s\n"+databaseConfig(dsn))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	startedAt := make(chan net.Addr, 1)
	code := make(chan int, 1)
	var stdout, stderr bytes.Buffer
	go func() {
		code <- runContext(ctx, []string{"--config", path, "--roles", roleList}, &stdout, &stderr,
			func(a net.Addr) { startedAt <- a })
	}()
	return started{cancel: cancel, code: code, stderr: &stderr, startedAt: startedAt}
}

// await waits until the launched server has set up all roles.
func (s started) await(t *testing.T) started {
	t.Helper()
	select {
	case a := <-s.startedAt:
		if a != nil {
			s.addr = a.String()
		}
	case c := <-s.code:
		require.Fail(t, "server exited early", "code %d: %s", c, s.stderr.String())
	case <-time.After(30 * time.Second):
		s.cancel()
		require.Fail(t, "server did not start")
	}
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

func readyStatus(t *testing.T, addr string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+addr+"/readyz", http.NoBody)
	require.NoError(t, err, "request")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "GET /readyz")
	_ = resp.Body.Close()
	return resp.StatusCode
}

func execSQL(t *testing.T, dsn, sql string) {
	t.Helper()
	// Not t.Context(): execSQL also runs in cleanups, after it is canceled.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err, "connect")
	defer func() { _ = conn.Close(ctx) }()
	_, err = conn.Exec(ctx, sql)
	require.NoError(t, err, "exec %s", sql)
}

func TestReadyzRequiresAppliedMigrations(t *testing.T) {
	dsn := pg.NewDatabase(t)
	s := startOn(t, "api", dsn)
	require.NotEmpty(t, s.addr, "api role did not listen")

	assert.Equal(t, http.StatusOK, readyStatus(t, s.addr), "/readyz after startup migrations")
	// Forget the latest migration: it counts as pending again.
	execSQL(t, dsn, "DELETE FROM goose_db_version WHERE version_id = (SELECT max(version_id) FROM goose_db_version)")
	assert.Equal(t, http.StatusServiceUnavailable, readyStatus(t, s.addr), "/readyz with a pending migration")
	s.stop(t)
}

func TestReadyzRequiresReachableDatabase(t *testing.T) {
	dsn := pg.NewDatabase(t)
	s := startOn(t, "api", dsn)
	require.NotEmpty(t, s.addr, "api role did not listen")
	require.Equal(t, http.StatusOK, readyStatus(t, s.addr), "/readyz while the database is reachable")

	// Revoke the server's access: new connections to the database fail.
	db := dsn[strings.LastIndex(dsn, "/")+1 : strings.Index(dsn, "?")]
	execSQL(t, pg.DSN(), "ALTER DATABASE "+pgx.Identifier{db}.Sanitize()+" ALLOW_CONNECTIONS false")
	execSQL(t, pg.DSN(), "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '"+db+"'")
	t.Cleanup(func() {
		execSQL(t, pg.DSN(), "ALTER DATABASE "+pgx.Identifier{db}.Sanitize()+" ALLOW_CONNECTIONS true")
	})

	assert.Equal(t, http.StatusServiceUnavailable, readyStatus(t, s.addr), "/readyz with the database unreachable")
	s.stop(t)
}

func TestConcurrentStartsApplyEachMigrationOnce(t *testing.T) {
	dsn := pg.NewDatabase(t)
	const instances = 3
	launched := make([]started, 0, instances)
	for range instances {
		launched = append(launched, launch(t, "api", dsn))
	}
	servers := make([]started, 0, instances)
	for _, s := range launched {
		servers = append(servers, s.await(t))
	}

	conn, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err, "connect")
	defer func() { _ = conn.Close(context.Background()) }()
	var total, distinct int
	require.NoError(t, conn.QueryRow(t.Context(),
		"SELECT count(*), count(DISTINCT version_id) FROM goose_db_version WHERE version_id > 0").Scan(&total, &distinct))
	assert.Positive(t, distinct, "no migration recorded")
	assert.Equal(t, distinct, total, "a migration was recorded more than once")
	for _, s := range servers {
		assert.Equal(t, http.StatusOK, readyStatus(t, s.addr), "/readyz")
		s.stop(t)
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
	path := writeConfig(t, "server:\n  address: "+addr+"\n"+serverConfig+databaseConfig(pg.NewDatabase(t)))
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
