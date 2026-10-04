package logging_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/server/internal/logging"
)

var formats = []logging.Format{logging.FormatText, logging.FormatJSON, logging.FormatLogfmt}

func newLogger(t *testing.T, format logging.Format, level slog.Level, secrets *logging.Secrets) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	logger, err := logging.New(&buf, logging.Options{Format: format, Level: level}, secrets)
	require.NoError(t, err, "New")
	return logger, &buf
}

func TestParseFormat(t *testing.T) {
	for _, f := range formats {
		got, err := logging.ParseFormat(string(f))
		require.NoError(t, err, "ParseFormat(%q)", f)
		assert.Equal(t, f, got)
	}
	for _, bad := range []string{"", "yaml", "JSON "} {
		_, err := logging.ParseFormat(bad)
		assert.Error(t, err, "ParseFormat(%q)", bad)
	}
}

func TestNewRejectsUnknownFormat(t *testing.T) {
	_, err := logging.New(&bytes.Buffer{}, logging.Options{Format: "xml"}, logging.NewSecrets())

	assert.ErrorContains(t, err, "xml")
}

func TestNewRequiresSecrets(t *testing.T) {
	_, err := logging.New(&bytes.Buffer{}, logging.Options{Format: logging.FormatJSON}, nil)

	assert.Error(t, err, "New without a secret registry must fail rather than log unredacted")
}

func TestJSONFormatWritesOneObjectPerLine(t *testing.T) {
	logger, buf := newLogger(t, logging.FormatJSON, slog.LevelInfo, logging.NewSecrets())

	logger.Info("hello", "n", 42, slog.Group("db", slog.String("host", "localhost")))
	logger.WithGroup("req").With("id", "r1").Warn("second", "path", "/x")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 2, "output %q", buf.String())
	var first, second map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &first), "line %q", lines[0])
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &second), "line %q", lines[1])
	assert.Equal(t, "hello", first["msg"])
	assert.Equal(t, "info", first["level"])
	assert.InDelta(t, 42, first["n"], 0)
	assert.Equal(t, map[string]any{"host": "localhost"}, first["db"])
	assert.NotEmpty(t, first["time"], "time")
	assert.Equal(t, "warn", second["level"])
	assert.Equal(t, map[string]any{"id": "r1", "path": "/x"}, second["req"])
}

func TestLogfmtFormatWritesKeyValuePairs(t *testing.T) {
	logger, buf := newLogger(t, logging.FormatLogfmt, slog.LevelInfo, logging.NewSecrets())

	logger.Info("hello world", "n", 42)

	out := buf.String()
	assert.Contains(t, out, `msg="hello world"`)
	assert.Contains(t, out, "level=info")
	assert.Contains(t, out, "n=42")
	assert.Contains(t, out, "time=")
	assert.Equal(t, 1, strings.Count(out, "\n"), "one line per record: %q", out)
}

func TestTextFormatIsHumanReadable(t *testing.T) {
	logger, buf := newLogger(t, logging.FormatText, slog.LevelInfo, logging.NewSecrets())

	logger.Info("hello world", "n", 42)

	out := buf.String()
	assert.Contains(t, out, "INFO")
	assert.Contains(t, out, "hello world")
	assert.Contains(t, out, "n=42")
	assert.NotContains(t, out, "msg=", "text output must not be logfmt")
	assert.False(t, json.Valid([]byte(out)), "text output must not be JSON")
	assert.NotContains(t, out, "\x1b[", "no color codes when not writing to a terminal")
}

func TestLevelFiltersRecords(t *testing.T) {
	for _, f := range formats {
		t.Run(string(f), func(t *testing.T) {
			logger, buf := newLogger(t, f, slog.LevelWarn, logging.NewSecrets())

			logger.Info("dropped-info")
			logger.Debug("dropped-debug")
			logger.Warn("kept-warn")
			logger.Error("kept-error")

			assert.NotContains(t, buf.String(), "dropped")
			assert.Contains(t, buf.String(), "kept-warn")
			assert.Contains(t, buf.String(), "kept-error")
			assert.False(t, logger.Enabled(t.Context(), slog.LevelInfo), "Enabled(info)")
			assert.True(t, logger.Enabled(t.Context(), slog.LevelWarn), "Enabled(warn)")
		})
	}
}
