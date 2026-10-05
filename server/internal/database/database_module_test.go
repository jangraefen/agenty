//go:build module

package database

import (
	"context"
	"os"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/server/internal/testinfra/pgtest"
)

// pg is the PostgreSQL server shared by all tests in this package.
var pg *pgtest.Server

func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m, &pg))
}

func openPool(t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	pool, err := Open(t.Context(), dsn)
	require.NoError(t, err, "Open")
	t.Cleanup(pool.Close)
	return pool
}

func newTestMigrator(t *testing.T, pool *pgxpool.Pool, fsys fstest.MapFS) *Migrator {
	t.Helper()
	m, err := NewMigrator(pool, WithMigrations(fsys))
	require.NoError(t, err, "NewMigrator")
	t.Cleanup(func() { _ = m.Close() })
	return m
}

func queryInt(t *testing.T, pool *pgxpool.Pool, sql string) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(t.Context(), sql).Scan(&n), "query %s", sql)
	return n
}

// racingMigrations would fail or record duplicates if two migrators applied
// them at the same time: CREATE TABLE is not idempotent, and every
// application of a migration inserts a row. The sleep widens the window in
// which a second migrator would interfere.
var racingMigrations = fstest.MapFS{
	"00001_create.sql": {Data: []byte(`-- +goose Up
CREATE TABLE applied (version int NOT NULL);
INSERT INTO applied VALUES (1);
SELECT pg_sleep(2);
-- +goose Down
DROP TABLE applied;
`)},
	"00002_insert.sql": {Data: []byte(`-- +goose Up
INSERT INTO applied VALUES (2);
SELECT pg_sleep(0.5);
-- +goose Down
DELETE FROM applied WHERE version = 2;
`)},
}

func TestConcurrentMigratorsApplyEachMigrationOnce(t *testing.T) {
	dsn := pg.NewDatabase(t)
	const instances = 2
	migrators := make([]*Migrator, instances)
	for i := range migrators {
		// Each instance has its own pool, as separate server processes do.
		migrators[i] = newTestMigrator(t, openPool(t, dsn), racingMigrations)
	}

	start := make(chan struct{})
	errs := make([]error, instances)
	var wg sync.WaitGroup
	for i, m := range migrators {
		wg.Go(func() {
			<-start
			errs[i] = m.Up(t.Context())
		})
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		assert.NoError(t, err, "migrator %d", i)
	}
	pool := openPool(t, dsn)
	assert.Equal(t, 1, queryInt(t, pool, "SELECT count(*) FROM applied WHERE version = 1"), "applications of migration 1")
	assert.Equal(t, 1, queryInt(t, pool, "SELECT count(*) FROM applied WHERE version = 2"), "applications of migration 2")
	assert.Equal(t, 2, queryInt(t, pool, "SELECT count(*) FROM goose_db_version WHERE version_id > 0"), "recorded migrations")
}

func TestUpIsIdempotent(t *testing.T) {
	pool := openPool(t, pg.NewDatabase(t))
	m := newTestMigrator(t, pool, racingMigrations)

	require.NoError(t, m.Up(t.Context()), "first Up")
	require.NoError(t, m.Up(t.Context()), "second Up")

	assert.Equal(t, 2, queryInt(t, pool, "SELECT count(*) FROM applied"), "applications")
}

func TestUpReportsFailingMigration(t *testing.T) {
	pool := openPool(t, pg.NewDatabase(t))
	m := newTestMigrator(t, pool, fstest.MapFS{
		"00001_broken.sql": {Data: []byte("-- +goose Up\nSELECT * FROM no_such_table;\n")},
	})

	err := m.Up(t.Context())

	assert.ErrorContains(t, err, "no_such_table")
}

func TestReadyRequiresAppliedMigrations(t *testing.T) {
	pool := openPool(t, pg.NewDatabase(t))
	m := newTestMigrator(t, pool, racingMigrations)

	err := Ready(t.Context(), pool, m)

	require.ErrorContains(t, err, "pending migrations", "before migrating")
	require.NoError(t, m.Up(t.Context()), "Up")
	assert.NoError(t, Ready(t.Context(), pool, m), "after migrating")
}

func TestReadyReportsMigrationsAddedByNewerInstanceAsApplied(t *testing.T) {
	dsn := pg.NewDatabase(t)
	newer := newTestMigrator(t, openPool(t, dsn), racingMigrations)
	require.NoError(t, newer.Up(t.Context()), "Up newer")
	pool := openPool(t, dsn)
	older := newTestMigrator(t, pool, fstest.MapFS{"00001_create.sql": racingMigrations["00001_create.sql"]})

	assert.NoError(t, Ready(t.Context(), pool, older))
}

func TestReadyFailsWhenAMigrationIsNoLongerRecorded(t *testing.T) {
	pool := openPool(t, pg.NewDatabase(t))
	m := newTestMigrator(t, pool, racingMigrations)
	require.NoError(t, m.Up(t.Context()), "Up")
	_, err := pool.Exec(t.Context(), "DELETE FROM goose_db_version WHERE version_id = 2")
	require.NoError(t, err, "forget migration 2")

	assert.ErrorContains(t, Ready(t.Context(), pool, m), "pending migrations")
}

func TestReadyFailsWhenTheMigrationHistoryIsUnreadable(t *testing.T) {
	pool := openPool(t, pg.NewDatabase(t))
	m := newTestMigrator(t, pool, racingMigrations)
	require.NoError(t, m.Up(t.Context()), "Up")
	_, err := pool.Exec(t.Context(), "DROP TABLE goose_db_version")
	require.NoError(t, err, "drop the migration history")

	assert.ErrorContains(t, Ready(t.Context(), pool, m), "check migrations")
}

func TestReadyFailsWhenTheDatabaseIsUnreachable(t *testing.T) {
	dsn := pg.NewDatabase(t)
	pool := openPool(t, dsn)
	m := newTestMigrator(t, pool, racingMigrations)
	require.NoError(t, m.Up(t.Context()), "Up")
	require.NoError(t, Ready(t.Context(), pool, m), "Ready while reachable")
	pool.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	assert.ErrorContains(t, Ready(ctx, pool, m), "database unreachable", "Ready after the pool is closed")
}

func TestEmbeddedMigrationsApplyWithoutDatabaseLogic(t *testing.T) {
	pool := openPool(t, pg.NewDatabase(t))
	m, err := NewMigrator(pool)
	require.NoError(t, err, "NewMigrator")
	t.Cleanup(func() { _ = m.Close() })

	require.NoError(t, m.Up(t.Context()), "Up")

	assert.NoError(t, Ready(t.Context(), pool, m), "Ready")
	// No logic in the database (ARCHITECTURE D5): no triggers, and no functions
	// or procedures other than those of extensions.
	assert.Equal(t, 0, queryInt(t, pool, "SELECT count(*) FROM pg_trigger WHERE NOT tgisinternal"), "triggers")
	assert.Equal(t, 0, queryInt(t, pool, `
		SELECT count(*) FROM pg_proc p
		JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
		  AND NOT EXISTS (SELECT 1 FROM pg_depend d
		                  WHERE d.classid = 'pg_proc'::regclass AND d.objid = p.oid AND d.deptype = 'e')`),
		"functions and procedures")
}
