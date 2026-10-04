//go:build module

package pgtest_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jangraefen/agenty/server/internal/testinfra/pgtest"
)

// pg is the PostgreSQL server shared by all tests in this package.
var pg *pgtest.Server

func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m, &pg))
}

func connect(t *testing.T, dsn string) *pgx.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

func TestServerRunsPinnedPostgresMajor(t *testing.T) {
	conn := connect(t, pg.DSN())

	var major int
	if err := conn.QueryRow(t.Context(), "SELECT current_setting('server_version_num')::int / 10000").Scan(&major); err != nil {
		t.Fatalf("query server version: %v", err)
	}
	if major != pgtest.PostgresMajor {
		t.Errorf("PostgreSQL major = %d, want %d", major, pgtest.PostgresMajor)
	}
}

func TestNewDatabaseHasPgvector(t *testing.T) {
	conn := connect(t, pg.NewDatabase(t))

	var distance float64
	err := conn.QueryRow(t.Context(), "SELECT '[1,2,3]'::vector <-> '[1,2,5]'::vector").Scan(&distance)
	if err != nil {
		t.Fatalf("vector query: %v", err)
	}
	if distance != 2 {
		t.Errorf("L2 distance = %v, want 2", distance)
	}
}

func TestNewDatabaseIsIsolated(t *testing.T) {
	first := connect(t, pg.NewDatabase(t))
	second := connect(t, pg.NewDatabase(t))

	if _, err := first.Exec(t.Context(), "CREATE TABLE only_in_first (id int)"); err != nil {
		t.Fatalf("create table: %v", err)
	}

	var exists bool
	if err := second.QueryRow(t.Context(), "SELECT to_regclass('only_in_first') IS NOT NULL").Scan(&exists); err != nil {
		t.Fatalf("lookup table: %v", err)
	}
	if exists {
		t.Error("table created in the first database is visible in the second")
	}
}

func TestNewDatabaseIsDroppedOnCleanup(t *testing.T) {
	var name string
	t.Run("create", func(t *testing.T) {
		conn := connect(t, pg.NewDatabase(t))
		if err := conn.QueryRow(t.Context(), "SELECT current_database()").Scan(&name); err != nil {
			t.Fatalf("current database: %v", err)
		}
	})

	conn := connect(t, pg.DSN())
	var exists bool
	if err := conn.QueryRow(t.Context(), "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", name).Scan(&exists); err != nil {
		t.Fatalf("lookup database: %v", err)
	}
	if exists {
		t.Errorf("database %q still exists after the test finished", name)
	}
}

func TestTerminateStopsServer(t *testing.T) {
	srv, err := pgtest.Start(t.Context())
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	dsn := srv.DSN()
	connect(t, dsn).Close(t.Context())

	if err := srv.Terminate(t.Context()); err != nil {
		t.Fatalf("terminate: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if conn, err := pgx.Connect(ctx, dsn); err == nil {
		_ = conn.Close(ctx)
		t.Error("server still accepts connections after Terminate")
	}
}
