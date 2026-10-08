// Package storetest gives tests a Store backed by a fresh PostgreSQL
// database.
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
// where it fails, so a missing database never passes silently.
func New(t *testing.T) *store.Store {
	t.Helper()
	s, _ := NewWithURL(t)
	return s
}

// NewWithURL is New, and also returns the URL of the new database.
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
	name := "agenty_test_" + strings.ToLower(rand.Text())
	_, err = conn.Exec(ctx, "CREATE DATABASE "+name)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := conn.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)")
		assert.NoError(t, err)
		assert.NoError(t, conn.Close(ctx))
	})

	u, err := url.Parse(admin)
	require.NoError(t, err)
	u.Path = "/" + name
	s, err := store.Open(ctx, u.String())
	require.NoError(t, err)
	t.Cleanup(s.Close)
	return s, u.String()
}
