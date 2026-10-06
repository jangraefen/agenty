package cli

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/toolgateway"
)

const logSecret = "log-secret-0123456789"

// TestLogger_RedactsSecrets guards trust-model guarantee 5 for logs: a secret
// never reaches the log, wherever in a record it appears.
func TestLogger_RedactsSecrets(t *testing.T) {
	r, err := toolgateway.NewRedactor([]string{logSecret})
	require.NoError(t, err)
	var out bytes.Buffer
	logger := newLogger(&out, slog.LevelDebug, r)

	logger.With("bound", "with "+logSecret).WithGroup("g").Info("message "+logSecret,
		"string", logSecret,
		"error", errors.New("failed with "+logSecret),
		"any", struct{ Token string }{logSecret},
		slog.Group("group", "nested", logSecret),
		"count", 3,
	)

	assert.NotContains(t, out.String(), logSecret)
	assert.Contains(t, out.String(), "message [redacted]")
	assert.Contains(t, out.String(), "count=3", "values without a secret are kept as they are")
}

func TestNewLogger_Levels(t *testing.T) {
	r, err := toolgateway.NewRedactor(nil)
	require.NoError(t, err)
	var out bytes.Buffer
	logger := newLogger(&out, slog.LevelWarn, r)

	logger.Info("hidden")
	logger.Warn("shown")

	assert.NotContains(t, out.String(), "hidden")
	assert.Contains(t, out.String(), "shown")
	assert.True(t, logger.Handler().Enabled(context.Background(), slog.LevelWarn))
}
