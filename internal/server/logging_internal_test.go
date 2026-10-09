package server

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/config"
	"github.com/jangraefen/agenty/internal/secret"
	"github.com/jangraefen/agenty/internal/store/storetest"
)

// TestInvariant_ServerLogsNoCredentials guards a trust-model guarantee:
// whatever logger the server is given, what it logs has its credentials
// redacted, errors included.
func TestInvariant_ServerLogsNoCredentials(t *testing.T) {
	const token = "s3cr3t-database-password"
	r, err := secret.NewRedactor([]string{token})
	require.NoError(t, err)
	logs := &lockedBuffer{}

	s, err := New(context.Background(), Config{
		Store:    storetest.New(t),
		Operator: &config.Config{},
		Resolved: &config.Resolved{Redactor: r},
		Logger:   slog.New(slog.NewTextHandler(logs, nil)),
	})
	require.NoError(t, err)
	t.Cleanup(s.Close)
	s.cfg.Logger.Error("cannot claim a run", "error", errors.New("dial postgres://agenty:"+token+"@db"))

	assert.NotContains(t, logs.String(), token)
	assert.Contains(t, logs.String(), "[redacted]")
}

// lockedBuffer is a buffer the server's goroutines may log to at once.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write appends p under the lock.
func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// String returns what was written so far.
func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
