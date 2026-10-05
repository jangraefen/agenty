package database

import (
	"context"
	"net"
	"testing"
	"testing/fstest"
	"time"

	"github.com/pressly/goose/v3/lock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unreachableDSN points at a port nothing listens on; its password must
// never show up in an error.
const unreachableDSN = "postgres://agenty:hunter2@127.0.0.1:1/agenty?sslmode=disable&connect_timeout=2" //nolint:gosec // Test credential.

func TestOpenRejectsInvalidURLWithoutLeakingThePassword(t *testing.T) {
	_, err := Open(t.Context(), "postgres://agenty:hunter2@localhost:notaport/agenty")

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "hunter2", "error leaks the password")
}

func TestUnreachableDatabaseFailsWithoutLeakingThePassword(t *testing.T) {
	pool, err := Open(t.Context(), unreachableDSN)
	require.NoError(t, err, "Open is lazy and must not connect")
	t.Cleanup(pool.Close)
	m, err := NewMigrator(pool)
	require.NoError(t, err, "NewMigrator")
	t.Cleanup(func() { _ = m.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	upErr := m.Up(ctx)
	readyErr := Ready(ctx, pool, m)

	require.Error(t, upErr, "Up")
	require.Error(t, readyErr, "Ready")
	assert.NotContains(t, upErr.Error(), "hunter2", "Up error leaks the password")
	assert.NotContains(t, readyErr.Error(), "hunter2", "Ready error leaks the password")
}

func TestNewMigratorRejectsInvalidMigrations(t *testing.T) {
	pool, err := Open(t.Context(), unreachableDSN)
	require.NoError(t, err, "Open")
	t.Cleanup(pool.Close)

	_, err = NewMigrator(pool, WithMigrations(fstest.MapFS{
		"00001_a.sql": {Data: []byte("-- +goose Up\nSELECT 1;\n")},
		"00001_b.sql": {Data: []byte("-- +goose Up\nSELECT 1;\n")},
	}))

	assert.Error(t, err, "duplicate versions")
}

func TestNewMigratorReportsInvalidLockSettings(t *testing.T) {
	pool, err := Open(t.Context(), unreachableDSN)
	require.NoError(t, err, "Open")
	t.Cleanup(pool.Close)
	previous := lockTimeout
	lockTimeout = lock.WithLockTimeout(0, 1)
	t.Cleanup(func() { lockTimeout = previous })

	_, err = NewMigrator(pool)

	assert.ErrorContains(t, err, "migration lock")
}

func TestEmbeddedMigrationsAreValid(t *testing.T) {
	pool, err := Open(t.Context(), unreachableDSN)
	require.NoError(t, err, "Open")
	t.Cleanup(pool.Close)

	m, err := NewMigrator(pool)

	require.NoError(t, err, "NewMigrator")
	assert.NoError(t, m.Close(), "Close")
}

// silentHost accepts TCP connections and never answers, like a database host
// that silently drops traffic. It returns a DSN without a connect timeout.
func silentHost(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "listen")
	var conns []net.Conn
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			conns = append(conns, c)
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
		for _, c := range conns {
			_ = c.Close()
		}
	})
	return "postgres://agenty:hunter2@" + ln.Addr().String() + "/agenty?sslmode=disable"
}

func TestReadyGivesUpOnASilentDatabaseHost(t *testing.T) {
	pool, err := Open(t.Context(), silentHost(t))
	require.NoError(t, err, "Open")
	t.Cleanup(pool.Close)
	m, err := NewMigrator(pool)
	require.NoError(t, err, "NewMigrator")
	t.Cleanup(func() { _ = m.Close() })

	begin := time.Now()
	// The context never expires: only Ready's own timeout ends the check.
	err = Ready(context.WithoutCancel(t.Context()), pool, m)
	elapsed := time.Since(begin)

	require.Error(t, err, "Ready")
	assert.NotContains(t, err.Error(), "hunter2", "Ready error leaks the password")
	assert.GreaterOrEqual(t, elapsed, readyTimeout-100*time.Millisecond, "Ready returned before its timeout")
	assert.Less(t, elapsed, readyTimeout+2*time.Second, "Ready outlasted its timeout")
}
