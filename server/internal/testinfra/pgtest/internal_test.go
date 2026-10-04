package pgtest

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"

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

	if code != 3 {
		t.Errorf("exit code = %d, want 3", code)
	}
	if srv == nil || srv.DSN() != "postgres://fake" {
		t.Errorf("srv = %+v, want the started server", srv)
	}
	if !m.ran || !c.terminated {
		t.Errorf("tests ran = %v, container terminated = %v, want both", m.ran, c.terminated)
	}
}

func TestRunFailsWithoutRunningTestsWhenStartFails(t *testing.T) {
	m := &fakeM{}
	var srv *Server

	code := run(m, &srv, startWith(nil, errors.New("no docker")))

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if m.ran || srv != nil {
		t.Errorf("tests ran = %v, srv = %v, want neither", m.ran, srv)
	}
}

func TestRunFailsWhenTerminateFails(t *testing.T) {
	for _, tt := range []struct{ testCode, want int }{{0, 1}, {3, 3}} {
		var srv *Server
		c := &fakeContainer{err: errors.New("stuck")}

		if code := run(&fakeM{code: tt.testCode}, &srv, startWith(c, nil)); code != tt.want {
			t.Errorf("tests exit %d: exit code = %d, want %d", tt.testCode, code, tt.want)
		}
	}
}

func TestTerminateReportsError(t *testing.T) {
	s := &Server{container: &fakeContainer{err: errors.New("stuck")}}

	err := s.Terminate(t.Context())

	if err == nil || !strings.Contains(err.Error(), "stuck") {
		t.Errorf("Terminate error = %v, want it to wrap the container error", err)
	}
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

	if !strings.Contains(msg, "parse dsn") {
		t.Errorf("fatal = %q, want a DSN parse error", msg)
	}
}

func TestExecFailsWhenServerIsUnreachable(t *testing.T) {
	// Port 1 on the loopback interface refuses connections immediately.
	s := &Server{dsn: "postgres://agenty@127.0.0.1:1/agenty?sslmode=disable&connect_timeout=5"}

	msg := fatalOf(func(tb testing.TB) { s.exec(tb, "SELECT 1") })

	if !strings.Contains(msg, "pgtest: connect") {
		t.Errorf("fatal = %q, want a connect error", msg)
	}
}

func TestWithDatabaseReplacesDatabaseName(t *testing.T) {
	got, err := withDatabase("postgres://u@localhost:5432/agenty?sslmode=disable", "other")

	if err != nil || got != "postgres://u@localhost:5432/other?sslmode=disable" {
		t.Errorf("withDatabase = %q, %v, want the DSN with database other", got, err)
	}
}
