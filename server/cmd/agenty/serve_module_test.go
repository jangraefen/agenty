//go:build module

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"testing/fstest"
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
	stderr *syncBuffer

	startedAt <-chan net.Addr
}

// syncBuffer is a bytes.Buffer safe for concurrent use, for log output.
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

// client sends the test requests. Without keep-alive no idle connection the
// client opened in advance delays a graceful shutdown: the server waits up to
// 5 seconds for a new connection to send its first request.
var client = &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}

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
// api role listens. Migrations may still be running.
func start(t *testing.T, roleList string) started {
	t.Helper()
	return startOn(t, roleList, pg.NewDatabase(t))
}

// startOn is start against the database at dsn.
func startOn(t *testing.T, roleList, dsn string) started {
	t.Helper()
	return launch(t, roleList, dsn, nil).await(t)
}

// startWithConfig is start with extra top-level configuration appended.
func startWithConfig(t *testing.T, roleList, extraConfig string) started {
	t.Helper()
	return launchWithConfig(t, roleList, pg.NewDatabase(t), nil, extraConfig).await(t)
}

// launch runs the server in-process without waiting for it to start. A
// non-nil migrations replaces the migrations embedded in the binary.
func launch(t *testing.T, roleList, dsn string, migrations fs.FS) started {
	t.Helper()
	return launchWithConfig(t, roleList, dsn, migrations, "")
}

// launchWithConfig is launch with extra top-level configuration appended.
func launchWithConfig(t *testing.T, roleList, dsn string, migrations fs.FS, extraConfig string) started {
	t.Helper()
	path := writeConfig(t, "server:\n  address: 127.0.0.1:0\n  shutdownTimeout: 5s\n"+databaseConfig(dsn)+extraConfig)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	startedAt := make(chan net.Addr, 1)
	code := make(chan int, 1)
	var stdout bytes.Buffer
	var stderr syncBuffer
	go func() {
		code <- runContext(ctx, []string{"--config", path, "--roles", roleList}, &stdout, &stderr, options{
			onStarted:  func(a net.Addr) { startedAt <- a },
			migrations: migrations,
		})
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

// awaitReady waits until /readyz reports ready, which requires the startup
// migrations to have completed.
func (s started) awaitReady(t *testing.T) {
	t.Helper()
	deadline := time.After(30 * time.Second)
	for {
		select {
		case c := <-s.code:
			require.Fail(t, "server exited before it was ready", "code %d: %s", c, s.stderr.String())
		case <-deadline:
			require.Fail(t, "server did not become ready")
		default:
		}
		if readyStatus(t, s.addr) == http.StatusOK {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// exitCode waits for the server to exit on its own and returns its exit code.
func (s started) exitCode(t *testing.T) int {
	t.Helper()
	select {
	case c := <-s.code:
		return c
	case <-time.After(30 * time.Second):
		require.Fail(t, "server did not exit")
		return 0
	}
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
	resp, err := client.Do(req)
	require.NoError(t, err, "GET /healthz")
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode, "GET /healthz")
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
			s.awaitReady(t)
			s.stop(t)
		})
	}
}

func readyStatus(t *testing.T, addr string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+addr+"/readyz", http.NoBody)
	require.NoError(t, err, "request")
	resp, err := client.Do(req)
	require.NoError(t, err, "GET /readyz")
	_ = resp.Body.Close()
	return resp.StatusCode
}

// readyBody returns the body of /readyz.
func readyBody(t *testing.T, addr string) string {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+addr+"/readyz", http.NoBody)
	require.NoError(t, err, "request")
	resp, err := client.Do(req)
	require.NoError(t, err, "GET /readyz")
	defer func() { _ = resp.Body.Close() }()
	var body bytes.Buffer
	_, err = body.ReadFrom(resp.Body)
	require.NoError(t, err, "read /readyz")
	return body.String()
}

func execSQL(t *testing.T, dsn, sql string, args ...any) {
	t.Helper()
	// Not t.Context(): execSQL also runs in cleanups, after it is canceled.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err, "connect")
	defer func() { _ = conn.Close(ctx) }()
	_, err = conn.Exec(ctx, sql, args...)
	require.NoError(t, err, "exec %s", sql)
}

func queryInt(t *testing.T, dsn, sql string) int {
	t.Helper()
	conn, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err, "connect")
	defer func() { _ = conn.Close(context.Background()) }()
	var n int
	require.NoError(t, conn.QueryRow(t.Context(), sql).Scan(&n), "query %s", sql)
	return n
}

// gatedMigrations wait for the table gate, which blockMigrations locks.
var gatedMigrations = fstest.MapFS{
	"00001_gated.sql": {Data: []byte("-- +goose Up\nSELECT count(*) FROM gate;\n")},
}

// blockMigrations makes gatedMigrations wait in the database at dsn until
// release is called (or the test ends).
func blockMigrations(t *testing.T, dsn string) (release func()) {
	t.Helper()
	execSQL(t, dsn, "CREATE TABLE gate (n int)")
	conn, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err, "connect")
	tx, err := conn.Begin(t.Context())
	require.NoError(t, err, "begin")
	_, err = tx.Exec(t.Context(), "LOCK TABLE gate IN ACCESS EXCLUSIVE MODE")
	require.NoError(t, err, "lock gate")
	release = func() {
		_ = tx.Rollback(context.Background())
		_ = conn.Close(context.Background())
	}
	t.Cleanup(release)
	return release
}

// awaitMigrationWaiting waits until a migration waits for the gate lock.
func awaitMigrationWaiting(t *testing.T, dsn string) {
	t.Helper()
	require.Eventually(t, func() bool {
		return queryInt(t, dsn, "SELECT count(*) FROM pg_locks WHERE relation = 'gate'::regclass AND NOT granted") > 0
	}, 30*time.Second, 50*time.Millisecond, "no migration waits for the gate")
}

func TestServeAnswersBeforeMigrationsComplete(t *testing.T) {
	dsn := pg.NewDatabase(t)
	release := blockMigrations(t, dsn)
	s := launch(t, "api,worker", dsn, gatedMigrations).await(t)
	require.NotEmpty(t, s.addr, "api role did not listen")
	awaitMigrationWaiting(t, dsn)

	assert.Equal(t, []string{"api", "worker"}, healthRoles(t, s.addr), "/healthz while migrating")
	assert.Equal(t, http.StatusServiceUnavailable, readyStatus(t, s.addr), "/readyz while migrating")
	body := readyBody(t, s.addr)
	assert.Contains(t, body, "The server is not ready to serve traffic.", "/readyz detail")
	assert.NotContains(t, body, "migration", "/readyz must not reveal details")
	release()
	s.awaitReady(t)
	assert.Equal(t, 1, queryInt(t, dsn, "SELECT count(*) FROM goose_db_version WHERE version_id = 1"), "applied migration")
	s.stop(t)
}

func TestServeStopsGracefullyWhileMigrating(t *testing.T) {
	dsn := pg.NewDatabase(t)
	blockMigrations(t, dsn)
	s := launch(t, "api,worker,scheduler", dsn, gatedMigrations).await(t)
	awaitMigrationWaiting(t, dsn)

	s.stop(t)
}

func TestServeExitsWhenMigrationsFail(t *testing.T) {
	dsn := pg.NewDatabase(t)
	release := blockMigrations(t, dsn)
	s := launch(t, "api", dsn, fstest.MapFS{
		"00001_broken.sql": {Data: []byte("-- +goose Up\nSELECT count(*) FROM gate;\nSELECT * FROM no_such_table;\n")},
	}).await(t)
	awaitMigrationWaiting(t, dsn)
	require.Equal(t, []string{"api"}, healthRoles(t, s.addr), "/healthz while migrating")

	release()

	assert.Equal(t, 1, s.exitCode(t), "exit code")
	assert.Contains(t, s.stderr.String(), "database migrations failed", "the configured logger reports the failure")
	assert.Contains(t, s.stderr.String(), "no_such_table", "the log names the failure")
}

func TestReadyzRequiresAppliedMigrations(t *testing.T) {
	dsn := pg.NewDatabase(t)
	s := startOn(t, "api", dsn)
	require.NotEmpty(t, s.addr, "api role did not listen")
	s.awaitReady(t)

	// Forget the latest migration: it counts as pending again.
	execSQL(t, dsn, "DELETE FROM goose_db_version WHERE version_id = (SELECT max(version_id) FROM goose_db_version)")
	assert.Equal(t, http.StatusServiceUnavailable, readyStatus(t, s.addr), "/readyz with a pending migration")
	s.stop(t)
}

func TestReadyzRequiresReachableDatabase(t *testing.T) {
	dsn := pg.NewDatabase(t)
	s := startOn(t, "api", dsn)
	require.NotEmpty(t, s.addr, "api role did not listen")
	s.awaitReady(t)

	// Revoke the server's access: new connections to the database fail.
	parsed, err := pgx.ParseConfig(dsn)
	require.NoError(t, err, "parse DSN")
	db := parsed.Database
	execSQL(t, pg.DSN(), "ALTER DATABASE "+pgx.Identifier{db}.Sanitize()+" ALLOW_CONNECTIONS false")
	execSQL(t, pg.DSN(), "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1", db)
	t.Cleanup(func() {
		execSQL(t, pg.DSN(), "ALTER DATABASE "+pgx.Identifier{db}.Sanitize()+" ALLOW_CONNECTIONS true")
	})

	assert.Equal(t, http.StatusServiceUnavailable, readyStatus(t, s.addr), "/readyz with the database unreachable")
	s.stop(t)
}

// slowMigrations fail, or record duplicates, when two instances apply them at
// the same time: CREATE TABLE is not idempotent and every application inserts
// a row. The sleep keeps the first instance busy while the second starts.
var slowMigrations = fstest.MapFS{
	"00001_slow.sql": {Data: []byte(`-- +goose Up
CREATE TABLE slow (n int NOT NULL);
INSERT INTO slow VALUES (1);
SELECT pg_sleep(2);
`)},
	"00002_more.sql": {Data: []byte(`-- +goose Up
INSERT INTO slow VALUES (2);
`)},
}

func TestConcurrentStartsApplyEachMigrationOnce(t *testing.T) {
	dsn := pg.NewDatabase(t)
	const instances = 2
	launched := make([]started, 0, instances)
	for range instances {
		launched = append(launched, launch(t, "api", dsn, slowMigrations))
	}
	servers := make([]started, 0, instances)
	for _, s := range launched {
		servers = append(servers, s.await(t))
	}

	for _, s := range servers {
		s.awaitReady(t)
	}
	assert.Equal(t, 1, queryInt(t, dsn, "SELECT count(*) FROM slow WHERE n = 1"), "applications of migration 1")
	assert.Equal(t, 1, queryInt(t, dsn, "SELECT count(*) FROM slow WHERE n = 2"), "applications of migration 2")
	assert.Equal(t, 2, queryInt(t, dsn, "SELECT count(*) FROM goose_db_version WHERE version_id > 0"), "recorded migrations")
	for _, s := range servers {
		s.stop(t)
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
			// Probes are logged at debug level; an unknown path at info.
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+s.addr+"/no-such-path", http.NoBody)
			require.NoError(t, err, "request")
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err, "GET /no-such-path")
			_ = resp.Body.Close()
			s.stop(t)

			lines := strings.Split(strings.TrimSpace(s.stderr.String()), "\n")
			var requestLogged bool
			for _, line := range lines {
				tt.check(t, line)
				requestLogged = requestLogged || strings.Contains(line, "/no-such-path")
			}
			assert.True(t, requestLogged, "no request log for /no-such-path in %q", s.stderr.String())
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
// configuration (address is appended) and returns once the api role serves
// HTTP requests.
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
	// Listening precedes serving: a signal in between would stop the server
	// before it accepts connections.
	require.Eventually(t, func() bool {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+addr+"/healthz", http.NoBody)
		if err != nil {
			return false
		}
		resp, err := client.Do(req)
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 30*time.Second, 20*time.Millisecond, "api role does not serve")
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
