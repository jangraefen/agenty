package cli

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/secret"
)

const logSecret = "log-secret-0123456789"

// TestNewLogger_RedactsSecrets checks that the CLI's logger goes through the
// redacting handler; secret.TestHandler_RedactsSecrets covers the handler.
func TestNewLogger_RedactsSecrets(t *testing.T) {
	r, err := secret.NewRedactor([]string{logSecret})
	require.NoError(t, err)
	var out bytes.Buffer
	logger := newLogger(&out, slog.LevelDebug, r)

	logger.Info("message "+logSecret, "error", errors.New("failed with "+logSecret))

	assert.NotContains(t, out.String(), logSecret)
	assert.Contains(t, out.String(), "message [redacted]")
}

func TestNewLogger_Levels(t *testing.T) {
	r, err := secret.NewRedactor(nil)
	require.NoError(t, err)
	var out bytes.Buffer
	logger := newLogger(&out, slog.LevelWarn, r)

	logger.Info("hidden")
	logger.Warn("shown")

	assert.NotContains(t, out.String(), "hidden")
	assert.Contains(t, out.String(), "shown")
	assert.True(t, logger.Handler().Enabled(context.Background(), slog.LevelWarn))
}
