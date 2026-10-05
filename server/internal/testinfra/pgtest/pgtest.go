// Package pgtest starts PostgreSQL with pgvector in a container for tests.
//
// A test package shares one server, started in TestMain and removed when the
// package's tests finish; each test that needs a clean state creates its own
// database on it:
//
//	var pg *pgtest.Server
//
//	func TestMain(m *testing.M) { os.Exit(pgtest.Run(m, &pg)) }
//
//	func TestSomething(t *testing.T) {
//		dsn := pg.NewDatabase(t) // dropped when t finishes
//		...
//	}
//
// The package is test infrastructure: only _test.go files import it.
package pgtest

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const (
	// Image is the pgvector image the tests and deploy/compose.yaml run.
	Image = "pgvector/pgvector:0.8.7-pg18-trixie"
	// PostgresMajor is the PostgreSQL major version of Image.
	PostgresMajor = 18

	user         = "agenty"
	password     = "agenty-test-password"
	adminDB      = "agenty"
	startTimeout = 2 * time.Minute
	queryTimeout = 30 * time.Second
)

// Server is a running PostgreSQL container.
type Server struct {
	container container
	dsn       string
}

// container is the part of the PostgreSQL container a Server uses after start.
type container interface {
	Terminate(ctx context.Context, opts ...testcontainers.TerminateOption) error
}

// Start starts a PostgreSQL container with pgvector and waits until it accepts
// connections. The caller must call Terminate.
func Start(ctx context.Context) (*Server, error) {
	ctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()

	container, err := postgres.Run(ctx, Image,
		postgres.WithUsername(user),
		postgres.WithPassword(password),
		postgres.WithDatabase(adminDB),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		if container != nil {
			_ = testcontainers.TerminateContainer(container)
		}
		return nil, fmt.Errorf("start postgres container: %w", err)
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = testcontainers.TerminateContainer(container)
		return nil, fmt.Errorf("postgres connection string: %w", err)
	}
	return &Server{container: container, dsn: dsn}, nil
}

// Run is the body of a TestMain: it starts a server, stores it in *srv, runs
// the tests, and terminates the server. It returns the exit code for os.Exit.
func Run(m *testing.M, srv **Server) int {
	return run(m, srv, Start)
}

// run is Run with the test runner and the server start replaceable in tests.
func run(m interface{ Run() int }, srv **Server, start func(context.Context) (*Server, error)) int {
	ctx := context.Background()
	s, err := start(ctx)
	if err != nil {
		slog.Error("pgtest: start server", "error", err)
		return 1
	}
	*srv = s

	code := m.Run()

	if err := s.Terminate(ctx); err != nil {
		slog.Error("pgtest: terminate server", "error", err)
		if code == 0 {
			code = 1
		}
	}
	return code
}

// DSN returns the connection string of the server's administrative database.
func (s *Server) DSN() string {
	return s.dsn
}

// Terminate stops and removes the container.
func (s *Server) Terminate(ctx context.Context) error {
	if err := s.container.Terminate(ctx); err != nil {
		return fmt.Errorf("terminate postgres container: %w", err)
	}
	return nil
}

// NewDatabase creates an empty database with the vector extension, returns
// its connection string, and drops it when t and its subtests finish.
func (s *Server) NewDatabase(t testing.TB) string {
	t.Helper()
	name := "test_" + strings.ToLower(rand.Text()[:16])
	ident := pgx.Identifier{name}.Sanitize()
	dsn, err := withDatabase(s.dsn, name)
	if err != nil {
		t.Fatalf("pgtest: %v", err)
	}

	s.exec(t, "CREATE DATABASE "+ident)
	t.Cleanup(func() {
		s.exec(t, "DROP DATABASE "+ident+" WITH (FORCE)")
	})
	execOn(t, dsn, "CREATE EXTENSION vector")
	return dsn
}

func (s *Server) exec(t testing.TB, sql string) {
	t.Helper()
	execOn(t, s.dsn, sql)
}

func execOn(t testing.TB, dsn, sql string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("pgtest: connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	if _, err := conn.Exec(ctx, sql); err != nil {
		t.Fatalf("pgtest: %s: %v", sql, err)
	}
}

func withDatabase(dsn, name string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("parse dsn: %w", err)
	}
	u.Path = "/" + name
	return u.String(), nil
}
