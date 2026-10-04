package pgtest

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

// fakeContainer stands in for the PostgreSQL container.
type fakeContainer struct {
	err        error
	terminated bool
}

func (c *fakeContainer) Terminate(context.Context, ...testcontainers.TerminateOption) error {
	c.terminated = true
	return c.err
}

// fakeM stands in for *testing.M.
type fakeM struct {
	code int
	ran  bool
}

func (m *fakeM) Run() int {
	m.ran = true
	return m.code
}

func startWith(c *fakeContainer, err error) func(context.Context) (*Server, error) {
	return func(context.Context) (*Server, error) {
		if err != nil {
			return nil, err
		}
		return &Server{container: c, dsn: "postgres://fake"}, nil
	}
}

func TestRunReturnsExitCodeOfTestsAndTerminates(t *testing.T) {
	c := &fakeContainer{}
	m := &fakeM{code: 3}
	var srv *Server

	code := run(m, &srv, startWith(c, nil))

	assert.Equal(t, 3, code, "exit code")
	if assert.NotNil(t, srv, "srv, want the started server") {
		assert.Equal(t, "postgres://fake", srv.DSN(), "srv, want the started server")
	}
	assert.True(t, m.ran, "tests ran")
	assert.True(t, c.terminated, "container terminated")
}

func TestRunFailsWithoutRunningTestsWhenStartFails(t *testing.T) {
	m := &fakeM{}
	var srv *Server

	code := run(m, &srv, startWith(nil, errors.New("no docker")))

	assert.Equal(t, 1, code, "exit code")
	assert.False(t, m.ran, "tests ran")
	assert.Nil(t, srv, "srv")
}

func TestRunFailsWhenTerminateFails(t *testing.T) {
	for _, tt := range []struct{ testCode, want int }{{0, 1}, {3, 3}} {
		var srv *Server
		c := &fakeContainer{err: errors.New("stuck")}

		assert.Equal(t, tt.want, run(&fakeM{code: tt.testCode}, &srv, startWith(c, nil)), "tests exit %d: exit code", tt.testCode)
	}
}

func TestTerminateReportsError(t *testing.T) {
	s := &Server{container: &fakeContainer{err: errors.New("stuck")}}

	err := s.Terminate(t.Context())

	assert.ErrorContains(t, err, "stuck", "Terminate error, want it to wrap the container error")
}

// fakeTB records the first fatal failure and stops the calling goroutine like
// testing.T does.
type fakeTB struct {
	testing.TB
	fatal string
}

func (*fakeTB) Helper()        {}
func (*fakeTB) Cleanup(func()) {}

func (f *fakeTB) Fatalf(format string, args ...any) {
	f.fatal = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

// fatalOf runs fn with a fakeTB and returns its fatal failure message.
func fatalOf(fn func(testing.TB)) string {
	tb := &fakeTB{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(tb)
	}()
	<-done
	return tb.fatal
}

func TestNewDatabaseFailsOnInvalidDSN(t *testing.T) {
	s := &Server{dsn: "postgres://%zz"}

	msg := fatalOf(func(tb testing.TB) { s.NewDatabase(tb) })

	assert.Contains(t, msg, "parse dsn", "fatal, want a DSN parse error")
}

func TestExecFailsWhenServerIsUnreachable(t *testing.T) {
	// A port that was just listened on and closed refuses connections immediately.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "listen")
	addr := ln.Addr().String()
	require.NoError(t, ln.Close(), "close listener")
	s := &Server{dsn: "postgres://agenty@" + addr + "/agenty?sslmode=disable&connect_timeout=5"}

	msg := fatalOf(func(tb testing.TB) { s.exec(tb, "SELECT 1") })

	assert.Contains(t, msg, "pgtest: connect", "fatal, want a connect error")
}

func TestWithDatabaseReplacesDatabaseName(t *testing.T) {
	got, err := withDatabase("postgres://u@localhost:5432/agenty?sslmode=disable", "other")

	assert.NoError(t, err, "withDatabase")
	assert.Equal(t, "postgres://u@localhost:5432/other?sslmode=disable", got, "withDatabase, want the DSN with database other")
}
