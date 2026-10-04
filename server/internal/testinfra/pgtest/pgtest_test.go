//go:build module

package pgtest_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	require.NoError(t, err, "connect")
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

func TestServerRunsPinnedPostgresMajor(t *testing.T) {
	conn := connect(t, pg.DSN())

	var major int
	err := conn.QueryRow(t.Context(), "SELECT current_setting('server_version_num')::int / 10000").Scan(&major)
	require.NoError(t, err, "query server version")
	assert.Equal(t, pgtest.PostgresMajor, major, "PostgreSQL major")
}

func TestNewDatabaseHasPgvector(t *testing.T) {
	conn := connect(t, pg.NewDatabase(t))

	var distance float64
	err := conn.QueryRow(t.Context(), "SELECT '[1,2,3]'::vector <-> '[1,2,5]'::vector").Scan(&distance)
	require.NoError(t, err, "vector query")
	assert.Equal(t, 2.0, distance, "L2 distance")
}

func TestNewDatabaseIsIsolated(t *testing.T) {
	first := connect(t, pg.NewDatabase(t))
	second := connect(t, pg.NewDatabase(t))

	_, err := first.Exec(t.Context(), "CREATE TABLE only_in_first (id int)")
	require.NoError(t, err, "create table")

	var exists bool
	err = second.QueryRow(t.Context(), "SELECT to_regclass('only_in_first') IS NOT NULL").Scan(&exists)
	require.NoError(t, err, "lookup table")
	assert.False(t, exists, "table created in the first database is visible in the second")
}

func TestNewDatabaseIsDroppedOnCleanup(t *testing.T) {
	var name string
	t.Run("create", func(t *testing.T) {
		conn := connect(t, pg.NewDatabase(t))
		require.NoError(t, conn.QueryRow(t.Context(), "SELECT current_database()").Scan(&name), "current database")
	})

	conn := connect(t, pg.DSN())
	var exists bool
	err := conn.QueryRow(t.Context(), "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", name).Scan(&exists)
	require.NoError(t, err, "lookup database")
	assert.False(t, exists, "database %q still exists after the test finished", name)
}

func TestTerminateStopsServer(t *testing.T) {
	srv, err := pgtest.Start(t.Context())
	require.NoError(t, err, "start")
	dsn := srv.DSN()
	connect(t, dsn).Close(t.Context())

	require.NoError(t, srv.Terminate(t.Context()), "terminate")

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err == nil {
		_ = conn.Close(ctx)
	}
	assert.Error(t, err, "server still accepts connections after Terminate")
}
