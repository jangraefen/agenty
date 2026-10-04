// Package database provides PostgreSQL access for the server (ARCHITECTURE
// §11.1): the pgx connection pool, goose migrations embedded in the binary and
// applied at startup under a PostgreSQL advisory lock, and the readiness check.
//
// Queries are written in SQL under queries/ and compiled to Go by sqlc into
// dbgen (task generate). Migrations live in migrations/.
package database

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/jangraefen/agenty/server/internal/database/dbgen"
	"github.com/jangraefen/agenty/server/internal/database/migrations"
)

// lockTimeout retries acquiring the migration lock every second, for up to
// 5 minutes, while another instance holds it. A variable so that tests can
// replace it.
var lockTimeout = lock.WithLockTimeout(1, 300)

// Open creates a connection pool for url, a PostgreSQL connection URL or
// keyword/value string; pool settings such as pool_max_conns may be part of
// it. Connections are established lazily. The error never contains the url
// or pgx's parse error, which may hold the password.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, errors.New("invalid database connection string or pool settings")
	}
	return pool, nil
}

// Migrator applies the schema migrations.
type Migrator struct {
	provider *goose.Provider
}

// MigratorOption configures a Migrator.
type MigratorOption func(*migratorOptions)

type migratorOptions struct {
	fsys fs.FS
}

// WithMigrations makes the Migrator apply the migrations at the root of fsys
// instead of those embedded in the binary. Tests use it to run migration sets
// of their own.
func WithMigrations(fsys fs.FS) MigratorOption {
	return func(o *migratorOptions) { o.fsys = fsys }
}

// NewMigrator returns a Migrator, by default for the migrations embedded in
// the binary. Close releases it; the pool stays open.
func NewMigrator(pool *pgxpool.Pool, opts ...MigratorOption) (*Migrator, error) {
	o := migratorOptions{fsys: migrations.FS}
	for _, opt := range opts {
		opt(&o)
	}
	fsys := o.fsys
	locker, err := lock.NewPostgresSessionLocker(lockTimeout)
	if err != nil {
		return nil, fmt.Errorf("create migration lock: %w", err)
	}
	db := stdlib.OpenDBFromPool(pool)
	provider, err := goose.NewProvider(goose.DialectPostgres, db, fsys,
		goose.WithSessionLocker(locker),
		goose.WithDisableGlobalRegistry(true),
	)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("load migrations: %w", err)
	}
	return &Migrator{provider: provider}, nil
}

// Up applies all pending migrations. It holds a PostgreSQL session advisory
// lock while doing so, so that instances starting concurrently apply each
// migration exactly once: the others wait for the lock and then find nothing
// pending.
func (m *Migrator) Up(ctx context.Context) error {
	results, err := m.provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	for _, r := range results {
		slog.InfoContext(ctx, "applied migration", "version", r.Source.Version, "file", r.Source.Path, "duration", r.Duration)
	}
	return nil
}

// Close releases the Migrator's database handle; the pool stays open.
func (m *Migrator) Close() error {
	return m.provider.Close()
}

// readyTimeout bounds a readiness check, so that a database host that silently
// drops traffic makes /readyz fail promptly instead of hanging.
const readyTimeout = 2 * time.Second

// Ready reports nil only when the database is reachable and every migration
// known to m has been applied. Migrations applied by a newer instance that m
// does not know do not make it unready.
// The check gives up after readyTimeout, however long ctx lasts.
func Ready(ctx context.Context, pool *pgxpool.Pool, m *Migrator) error {
	ctx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()
	if _, err := dbgen.New(pool).Ping(ctx); err != nil {
		return fmt.Errorf("database unreachable: %w", err)
	}
	pending, err := m.provider.HasPending(ctx)
	if err != nil {
		return fmt.Errorf("check migrations: %w", err)
	}
	if pending {
		return errors.New("pending migrations")
	}
	return nil
}
