// Package storetest gives tests a Store backed by a fresh PostgreSQL
// database.
//
// Every test gets a database of its own, created on the server that
// AGENTY_TEST_DATABASE_URL names and dropped when the test ends, so tests
// can run in parallel and never see each other's rows: each audit log, for
// one, starts at id 1. The tests of the store, the server and the CLI use
// it; `task test` and `task check` start a PostgreSQL server for them.
package storetest

import (
	"context"
	"crypto/rand"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/store"
)

// EnvURL names the environment variable with the URL of a PostgreSQL server
// tests may create databases on, such as
// postgres://postgres:postgres@127.0.0.1:5432/postgres.
const EnvURL = "AGENTY_TEST_DATABASE_URL"

// New creates an empty database, opens a Store on it, and drops the database
// when the test ends. Without EnvURL set, the test is skipped, except in CI,
// where it fails, so a missing database never passes silently. The Store is
// migrated, as Open migrates.
func New(t *testing.T) *store.Store {
	t.Helper()
	s, _ := NewWithURL(t)
	return s
}

// NewWithURL is New, and also returns the URL of the new database, for
// tests that open a second connection or Store on it, such as to hold a
// lock another claim must skip, or to tamper with the audit log behind the
// store's back.
func NewWithURL(t *testing.T) (*store.Store, string) {
	t.Helper()
	admin := os.Getenv(EnvURL)
	if admin == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("%s is not set", EnvURL)
		}
		t.Skipf("%s is not set; set it to run tests against PostgreSQL", EnvURL)
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, admin)
	require.NoError(t, err)
	// A random name keeps concurrent tests apart; lower case, as PostgreSQL
	// folds unquoted names to it.
	name := "agenty_test_" + strings.ToLower(rand.Text())
	_, err = conn.Exec(ctx, "CREATE DATABASE "+name)
	require.NoError(t, err)
	// FORCE ends the connections a test left open, such as a Store it
	// closed late, so the drop never waits on them.
	t.Cleanup(func() {
		_, err := conn.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)")
		assert.NoError(t, err)
		assert.NoError(t, conn.Close(ctx))
	})

	// The new database is reached through the admin URL with its path
	// replaced, keeping its credentials and options.
	u, err := url.Parse(admin)
	require.NoError(t, err)
	u.Path = "/" + name
	s, err := store.Open(ctx, u.String())
	require.NoError(t, err)
	t.Cleanup(s.Close)
	return s, u.String()
}
