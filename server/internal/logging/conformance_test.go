package logging_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"testing/slogtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/server/internal/logging"
)

// The redacting handler rebuilds every record, so it must itself follow the
// slog.Handler contract that testing/slogtest checks.
func TestRedactingHandlerConformsToSlogHandlerContract(t *testing.T) {
	var buf *bytes.Buffer
	slogtest.Run(t,
		func(*testing.T) slog.Handler {
			buf = &bytes.Buffer{}
			return logging.NewRedactingHandler(slog.NewJSONHandler(buf, nil), logging.NewSecrets())
		},
		func(t *testing.T) map[string]any {
			var m map[string]any
			require.NoError(t, json.Unmarshal(buf.Bytes(), &m), "output %q", buf.String())
			return m
		})
}

// parseLine returns the single record written to buf: decoded for the JSON
// format, and as the raw line for every format.
func parseLine(t *testing.T, f logging.Format, buf *bytes.Buffer) (map[string]any, string) {
	t.Helper()
	line := buf.String()
	require.Equal(t, 1, strings.Count(line, "\n"), "one record expected: %q", line)
	if f != logging.FormatJSON {
		return nil, line
	}
	var m map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &m), "output %q", line)
	return m, line
}

// charmbracelet/log writes its own time, level, msg, prefix, and caller
// fields; a top-level attribute with one of these keys is renamed to
// attr.<key> instead of being dropped or duplicated.
func TestRenamesTopLevelAttributesWithReservedKeys(t *testing.T) {
	for _, key := range []string{"time", "level", "msg", "prefix", "caller"} {
		for _, f := range formats {
			t.Run(key+"/"+string(f), func(t *testing.T) {
				logger, buf := newLogger(t, f, slog.LevelInfo, logging.NewSecrets())

				logger.Info("the-message", key, "user-value")

				m, line := parseLine(t, f, buf)
				assert.Contains(t, line, "the-message")
				assert.Equal(t, 1, strings.Count(line, "user-value"), "output %q", line)
				if f == logging.FormatJSON {
					assert.Equal(t, "user-value", m["attr."+key], "output %q", line)
					assert.Equal(t, "the-message", m["msg"])
					assert.Equal(t, "info", m["level"])
					return
				}
				assert.Contains(t, line, "attr."+key+"=user-value")
			})
		}
	}
}

func TestKeepsReservedKeysInsideGroups(t *testing.T) {
	logger, buf := newLogger(t, logging.FormatJSON, slog.LevelInfo, logging.NewSecrets())

	logger.WithGroup("req").Info("m", "level", "nested")

	m, _ := parseLine(t, logging.FormatJSON, buf)
	assert.Equal(t, map[string]any{"level": "nested"}, m["req"])
	assert.Equal(t, "info", m["level"])
}

// A group with an empty key is inlined into its parent, as the slog.Handler
// contract requires.
func TestInlinesEmptyKeyGroups(t *testing.T) {
	cases := []struct {
		name     string
		log      func(l *slog.Logger)
		wantJSON map[string]any
		wantText []string
	}{
		{
			name:     "top level",
			log:      func(l *slog.Logger) { l.Info("m", slog.Group("", slog.String("a", "b")), "c", "d") },
			wantJSON: map[string]any{"a": "b", "c": "d"},
			wantText: []string{"a=b", "c=d"},
		},
		{
			name:     "nested in group",
			log:      func(l *slog.Logger) { l.Info("m", slog.Group("g", slog.Group("", slog.String("a", "b")))) },
			wantJSON: map[string]any{"g": map[string]any{"a": "b"}},
			wantText: []string{"a=b"},
		},
		{
			name:     "bound with With",
			log:      func(l *slog.Logger) { l.With(slog.Group("", "a", "b")).Info("m") },
			wantJSON: map[string]any{"a": "b"},
			wantText: []string{"a=b"},
		},
		{
			name:     "reserved key inside",
			log:      func(l *slog.Logger) { l.Info("m", slog.Group("", slog.String("level", "x"))) },
			wantJSON: map[string]any{"attr.level": "x", "level": "info"},
			wantText: []string{"attr.level=x"},
		},
	}
	for _, tc := range cases {
		for _, f := range formats {
			t.Run(tc.name+"/"+string(f), func(t *testing.T) {
				logger, buf := newLogger(t, f, slog.LevelInfo, logging.NewSecrets())

				tc.log(logger)

				m, line := parseLine(t, f, buf)
				if f == logging.FormatJSON {
					for k, v := range tc.wantJSON {
						assert.Equal(t, v, m[k], "key %q in %q", k, line)
					}
					assert.NotContains(t, m, "", "no empty key: %q", line)
					return
				}
				for _, want := range tc.wantText {
					assert.Contains(t, line, want)
				}
			})
		}
	}
}

// Empty attributes (zero key and value) and groups left without attributes
// are not written.
func TestIgnoresEmptyAttributesAndGroups(t *testing.T) {
	for _, f := range formats {
		t.Run(string(f), func(t *testing.T) {
			logger, buf := newLogger(t, f, slog.LevelInfo, logging.NewSecrets())

			logger.Info("m", slog.Attr{}, slog.Group("g", slog.Attr{}), slog.Group(""), "k", "v")

			m, line := parseLine(t, f, buf)
			assert.NotContains(t, line, "<nil>")
			assert.NotContains(t, line, " g=")
			assert.Contains(t, line, "k")
			if f == logging.FormatJSON {
				assert.NotContains(t, m, "")
				assert.NotContains(t, m, "g")
				assert.Equal(t, "v", m["k"])
			}
		})
	}
}
