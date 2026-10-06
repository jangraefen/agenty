package secret_test

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

// newLogger returns a logger whose records pass through r's handler into a
// text handler writing to out.
func newLogger(t *testing.T, out *bytes.Buffer, level slog.Level) *slog.Logger {
	t.Helper()
	r, err := secret.NewRedactor([]string{apiKey})
	require.NoError(t, err)
	return slog.New(r.Handler(slog.NewTextHandler(out, &slog.HandlerOptions{Level: level})))
}

// TestHandler_RedactsSecrets guards trust-model guarantee 5 for logs: a
// secret never reaches the log, wherever in a record it appears.
func TestHandler_RedactsSecrets(t *testing.T) {
	var out bytes.Buffer
	logger := newLogger(t, &out, slog.LevelDebug)

	logger.With("bound", "with "+apiKey).WithGroup("g").Info("message "+apiKey,
		"string", apiKey,
		"error", errors.New("failed with "+apiKey),
		"any", struct{ Token string }{apiKey},
		slog.Group("group", "nested", apiKey),
		"lazy", slog.AnyValue(lazy{}),
		"count", 3,
	)

	assert.NotContains(t, out.String(), apiKey)
	assert.Contains(t, out.String(), `msg="message [redacted]"`)
	assert.Contains(t, out.String(), `bound="with [redacted]"`)
	assert.Contains(t, out.String(), "g.group.nested=[redacted]")
	assert.Contains(t, out.String(), "g.lazy=[redacted]", "LogValuers are resolved before redaction")
	assert.Contains(t, out.String(), "g.count=3", "values without a secret are kept as they are")
}

func TestHandler_KeepsTheKindOfValuesWithoutASecret(t *testing.T) {
	r, err := secret.NewRedactor([]string{apiKey})
	require.NoError(t, err)
	var out bytes.Buffer
	logger := slog.New(r.Handler(slog.NewJSONHandler(&out, nil)))

	logger.Info("m", "count", 3, "ok", true, "token", apiKey)

	assert.Contains(t, out.String(), `"count":3,"ok":true,"token":"[redacted]"`)
}

// lazy is a LogValuer whose value holds the secret only once resolved.
type lazy struct{}

func (lazy) LogValue() slog.Value { return slog.StringValue(apiKey) }

func TestHandler_KeepsLevels(t *testing.T) {
	var out bytes.Buffer
	logger := newLogger(t, &out, slog.LevelWarn)

	logger.Info("hidden")
	logger.Warn("shown")

	assert.NotContains(t, out.String(), "hidden")
	assert.Contains(t, out.String(), "shown")
	assert.False(t, logger.Handler().Enabled(context.Background(), slog.LevelInfo))
	assert.True(t, logger.Handler().Enabled(context.Background(), slog.LevelWarn))
}
