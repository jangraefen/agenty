package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/server/internal/logging"
)

const (
	secret   = "s3cr3t-T0ken-9f8e7d" //nolint:gosec // A fake secret the tests try to leak.
	redacted = "[REDACTED]"
)

type stringer struct{ s string }

func (s stringer) String() string { return "stringer(" + s.s + ")" }

type valuer struct{ s string }

func (v valuer) LogValue() slog.Value { return slog.StringValue("valuer(" + v.s + ")") }

type groupValuer struct{ s string }

func (v groupValuer) LogValue() slog.Value {
	return slog.GroupValue(slog.String("user", "alice"), slog.String("password", v.s))
}

type textMarshaler struct{ s string }

func (m textMarshaler) MarshalText() ([]byte, error) { return []byte("text(" + m.s + ")"), nil }

type jsonOnly struct{ s string }

// MarshalJSON reveals the secret only when encoded as JSON, never via fmt.
func (j jsonOnly) MarshalJSON() ([]byte, error) { return json.Marshal(map[string]string{"k": j.s}) }

type credentials struct {
	User     string
	Password string
}

type wrapper struct {
	Inner *credentials
}

// leakCases log the secret in every place a record can carry it.
var leakCases = []struct {
	name string
	log  func(l *slog.Logger)
}{
	{"message", func(l *slog.Logger) { l.Info("token is " + secret) }},
	{"message formatted with fmt", func(l *slog.Logger) { l.Info(fmt.Sprintf("auth with %q failed", secret)) }},
	{"string attribute", func(l *slog.Logger) { l.Info("m", "token", secret) }},
	{"string attribute embedded", func(l *slog.Logger) { l.Info("m", "dsn", "postgres://agenty:"+secret+"@db:5432/agenty") }},
	{"slog.Attr", func(l *slog.Logger) { l.Info("m", slog.String("token", secret)) }},
	{"attribute key", func(l *slog.Logger) { l.Info("m", secret, "v") }},
	{"group", func(l *slog.Logger) { l.Info("m", slog.Group("db", slog.String("password", secret))) }},
	{"nested group", func(l *slog.Logger) {
		l.Info("m", slog.Group("a", slog.Group("b", slog.Group("c", slog.String("password", secret)))))
	}},
	{"group name", func(l *slog.Logger) { l.Info("m", slog.Group(secret, slog.String("k", "v"))) }},
	{"error", func(l *slog.Logger) { l.Info("m", "error", errors.New("login failed for "+secret)) }},
	{"wrapped error", func(l *slog.Logger) {
		l.Info("m", "error", fmt.Errorf("connect: %w", errors.New("password "+secret+" rejected")))
	}},
	{"joined error", func(l *slog.Logger) { l.Info("m", "error", errors.Join(errors.New("a"), errors.New(secret))) }},
	{"fmt.Stringer", func(l *slog.Logger) { l.Info("m", "v", stringer{secret}) }},
	{"LogValuer", func(l *slog.Logger) { l.Info("m", "v", valuer{secret}) }},
	{"LogValuer returning group", func(l *slog.Logger) { l.Info("m", "v", groupValuer{secret}) }},
	{"TextMarshaler", func(l *slog.Logger) { l.Info("m", "v", textMarshaler{secret}) }},
	{"json.Marshaler", func(l *slog.Logger) { l.Info("m", "v", jsonOnly{secret}) }},
	{"struct", func(l *slog.Logger) { l.Info("m", "creds", credentials{"alice", secret}) }},
	{"pointer to struct", func(l *slog.Logger) { l.Info("m", "creds", &credentials{"alice", secret}) }},
	{"map", func(l *slog.Logger) { l.Info("m", "headers", map[string]string{"Authorization": "Bearer " + secret}) }},
	{"slice", func(l *slog.Logger) { l.Info("m", "args", []string{"--token", secret}) }},
	{"byte slice", func(l *slog.Logger) { l.Info("m", "body", []byte("token="+secret)) }},
	{"any holding string", func(l *slog.Logger) { l.Info("m", slog.Any("v", secret)) }},
	{"With attribute", func(l *slog.Logger) { l.With("token", secret).Info("m") }},
	{"With group attribute", func(l *slog.Logger) { l.With(slog.Group("g", "token", secret)).Info("m") }},
	{"WithGroup then With", func(l *slog.Logger) { l.WithGroup("req").With("token", secret).Info("m") }},
	{"WithGroup then record attribute", func(l *slog.Logger) { l.WithGroup("req").Info("m", "token", secret) }},
	{"WithGroup name", func(l *slog.Logger) { l.WithGroup(secret).Info("m", "k", "v") }},
	{"LogAttrs", func(l *slog.Logger) {
		l.LogAttrs(context.Background(), slog.LevelError, "m", slog.Any("error", errors.New(secret)))
	}},
}

func assertRedacted(t *testing.T, out, secretValue string) {
	t.Helper()
	assert.NotEmpty(t, out, "no output")
	assert.Contains(t, out, redacted, "output %q", out)
	assert.NotContains(t, out, secretValue, "secret leaked: %q", out)
	// The secret must not appear in an escaped form either.
	quoted := strconv.Quote(secretValue)
	assert.NotContains(t, out, quoted[1:len(quoted)-1], "escaped secret leaked: %q", out)
	jsonQuoted, err := json.Marshal(secretValue)
	require.NoError(t, err)
	assert.NotContains(t, out, string(jsonQuoted[1:len(jsonQuoted)-1]), "JSON-escaped secret leaked: %q", out)
}

func TestRedactsRegisteredSecretsInEveryFormat(t *testing.T) {
	for _, f := range formats {
		for _, tc := range leakCases {
			t.Run(string(f)+"/"+tc.name, func(t *testing.T) {
				secrets := logging.NewSecrets()
				secrets.Register(secret)
				logger, buf := newLogger(t, f, slog.LevelDebug, secrets)

				tc.log(logger)

				assertRedacted(t, buf.String(), secret)
			})
		}
	}
}

func TestRedactsSecretsRegisteredAfterLoggerCreation(t *testing.T) {
	for _, f := range formats {
		t.Run(string(f), func(t *testing.T) {
			secrets := logging.NewSecrets()
			logger, buf := newLogger(t, f, slog.LevelDebug, secrets)
			bound := logger.WithGroup("req").With("token", secret)

			secrets.Register(secret)
			bound.Info("m", "again", secret)

			assertRedacted(t, buf.String(), secret)
		})
	}
}

func TestRedactsSecretsWithSpecialCharacters(t *testing.T) {
	special := []string{
		`pa"ss\word<&>`,
		"multi\nline\tsecret",
		"unicodé-🔑-secret",
		"key=value secret",
	}
	for _, f := range formats {
		for _, s := range special {
			t.Run(string(f)+"/"+strconv.Quote(s), func(t *testing.T) {
				secrets := logging.NewSecrets()
				secrets.Register(s)
				logger, buf := newLogger(t, f, slog.LevelDebug, secrets)

				logger.Info("msg "+s, "attr", "x"+s+"y", "err", errors.New(s), slog.Group("g", "v", s))

				assertRedacted(t, buf.String(), s)
			})
		}
	}
}

func TestRedactsNonStringValuesWhoseTextIsASecret(t *testing.T) {
	for _, f := range formats {
		t.Run(string(f), func(t *testing.T) {
			secrets := logging.NewSecrets()
			secrets.Register("4242424242")
			logger, buf := newLogger(t, f, slog.LevelDebug, secrets)

			logger.Info("m", "pin", 4242424242, "upin", uint64(4242424242), slog.Int64("i64", 4242424242)) //nolint:sloglint // Mixed on purpose: both argument forms must be redacted.

			assertRedacted(t, buf.String(), "4242424242")
		})
	}
}

func TestRedactsEverySecretAndPrefersTheLongestMatch(t *testing.T) {
	for _, f := range formats {
		t.Run(string(f), func(t *testing.T) {
			secrets := logging.NewSecrets()
			secrets.Register("abc123", "abc123def456", "zzz-other-secret")
			logger, buf := newLogger(t, f, slog.LevelDebug, secrets)

			logger.Info("m", "a", "abc123def456", "b", "zzz-other-secret", "c", "abc123")

			out := buf.String()
			assert.NotContains(t, out, "abc123")
			assert.NotContains(t, out, "def456", "longest secret must win over its prefix")
			assert.NotContains(t, out, "zzz-other-secret")
			assert.Equal(t, 3, strings.Count(out, redacted), "output %q", out)
		})
	}
}

// A JSON encoder would follow the pointer and write the secret; the value is
// rendered as fmt does instead, which prints only the address.
func TestDoesNotFollowPointersNestedInValues(t *testing.T) {
	for _, f := range formats {
		t.Run(string(f), func(t *testing.T) {
			secrets := logging.NewSecrets()
			secrets.Register(secret)
			logger, buf := newLogger(t, f, slog.LevelDebug, secrets)

			logger.Info("m", "w", wrapper{&credentials{"alice", secret}})

			assert.Contains(t, buf.String(), "Inner:0x")
			assert.NotContains(t, buf.String(), secret)
			assert.NotContains(t, buf.String(), "alice")
		})
	}
}

func TestRedactsOverlappingSecretsCompletely(t *testing.T) {
	secrets := logging.NewSecrets()
	secrets.Register("xxabc", "abcyy")

	assert.Equal(t, "<[REDACTED]>", secrets.Redact("<xxabcyy>"))
	assert.Equal(t, "[REDACTED]-[REDACTED]", secrets.Redact("abcyy-xxabc"))
	assert.Equal(t, "[REDACTED]", secrets.Redact("xxabcabcyy"), "adjacent occurrences")
}

func TestKeepsNonSecretContent(t *testing.T) {
	secrets := logging.NewSecrets()
	secrets.Register(secret)
	logger, buf := newLogger(t, logging.FormatJSON, slog.LevelDebug, secrets)

	logger.WithGroup("req").With("id", "r1").Info("token "+secret+" used",
		"dsn", "postgres://agenty:"+secret+"@db/agenty", "n", 7, "ok", true,
		slog.Group("db", slog.String("host", "db")))

	var got map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got), "output %q", buf.String())
	assert.Equal(t, "token [REDACTED] used", got["msg"])
	assert.Equal(t, map[string]any{
		"id":  "r1",
		"dsn": "postgres://agenty:[REDACTED]@db/agenty",
		"n":   float64(7),
		"ok":  true,
		"db":  map[string]any{"host": "db"},
	}, got["req"])
}

func TestIgnoresEmptySecret(t *testing.T) {
	secrets := logging.NewSecrets()
	secrets.Register("")
	logger, buf := newLogger(t, logging.FormatLogfmt, slog.LevelDebug, secrets)

	logger.Info("plain message", "k", "v")

	assert.NotContains(t, buf.String(), redacted)
	assert.Contains(t, buf.String(), `msg="plain message"`)
}

func TestSecretsRedact(t *testing.T) {
	secrets := logging.NewSecrets()
	assert.Equal(t, "nothing registered", secrets.Redact("nothing registered"))

	secrets.Register(secret, secret)

	assert.Equal(t, "a [REDACTED] b [REDACTED]", secrets.Redact("a "+secret+" b "+secret))
}

func TestSecretsAreSafeForConcurrentUse(t *testing.T) {
	secrets := logging.NewSecrets()
	logger, buf := newLogger(t, logging.FormatJSON, slog.LevelDebug, secrets)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() { secrets.Register(fmt.Sprintf("concurrent-secret-%02d", i)) })
		wg.Go(func() { logger.Info("m", "v", fmt.Sprintf("value-%02d", i)) })
	}
	wg.Wait()
	buf.Reset()

	for i := range 20 {
		logger.Info("m", "v", fmt.Sprintf("concurrent-secret-%02d", i))
	}

	assert.NotContains(t, buf.String(), "concurrent-secret")
	assert.Equal(t, 20, strings.Count(buf.String(), redacted))
}

// recorder is a minimal slog.Handler that keeps the records it receives.
type recorder struct {
	records *[]slog.Record
}

func (r recorder) Enabled(context.Context, slog.Level) bool { return true }
func (r recorder) Handle(_ context.Context, rec slog.Record) error {
	*r.records = append(*r.records, rec)
	return nil
}
func (r recorder) WithAttrs([]slog.Attr) slog.Handler {
	panic("redacting handler must not forward WithAttrs")
}
func (r recorder) WithGroup(string) slog.Handler {
	panic("redacting handler must not forward WithGroup")
}

func TestRedactingHandlerWrapsAnyHandler(t *testing.T) {
	var records []slog.Record
	secrets := logging.NewSecrets()
	secrets.Register(secret)
	h := logging.NewRedactingHandler(recorder{&records}, secrets)
	logger := slog.New(h)

	logger.WithGroup("g").With("a", secret).WithGroup("").With().Info("m "+secret, "b", 1)

	require.Len(t, records, 1)
	rec := records[0]
	assert.Equal(t, "m [REDACTED]", rec.Message)
	assert.NotZero(t, rec.PC, "source position preserved")
	var attrs []slog.Attr
	rec.Attrs(func(a slog.Attr) bool { attrs = append(attrs, a); return true })
	require.Len(t, attrs, 1)
	assert.Equal(t, "g", attrs[0].Key)
	assert.Equal(t, "[a=[REDACTED] b=1]", attrs[0].Value.String())
}

func TestRedactingHandlerDropsEmptyGroups(t *testing.T) {
	var records []slog.Record
	h := logging.NewRedactingHandler(recorder{&records}, logging.NewSecrets())

	slog.New(h).WithGroup("empty").Info("m")

	require.Len(t, records, 1)
	assert.Zero(t, records[0].NumAttrs(), "an empty group must not produce an attribute")
}

func TestRedactingHandlerDelegatesEnabled(t *testing.T) {
	var buf bytes.Buffer
	next := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError})
	h := logging.NewRedactingHandler(next, logging.NewSecrets())

	assert.False(t, h.Enabled(t.Context(), slog.LevelWarn))
	assert.True(t, h.Enabled(t.Context(), slog.LevelError))
	assert.False(t, h.WithGroup("g").Enabled(t.Context(), slog.LevelWarn))
}

func TestRedactingHandlerWorksWithStandardJSONHandler(t *testing.T) {
	var buf bytes.Buffer
	secrets := logging.NewSecrets()
	secrets.Register(secret)
	logger := slog.New(logging.NewRedactingHandler(slog.NewJSONHandler(&buf, nil), secrets))

	logger.WithGroup("req").Info("m", "creds", &credentials{"alice", secret})

	assertRedacted(t, buf.String(), secret)
	var got map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	want := map[string]any{"creds": "&{User:alice Password:[REDACTED]}"} //nolint:gosec // Expected redacted output.
	assert.Equal(t, want, got["req"])
}

type nilErr struct{ msg string }

func (e *nilErr) Error() string { return e.msg }

type nilMarshaler struct{ s string }

func (m *nilMarshaler) MarshalText() ([]byte, error) { return []byte(m.s), nil }

// A typed nil pointer passed as an error or TextMarshaler must be logged as
// <nil>, as log/slog's handlers do, instead of panicking in the log call.
func TestLogsTypedNilPointersWithoutPanicking(t *testing.T) {
	for _, f := range formats {
		t.Run(string(f), func(t *testing.T) {
			secrets := logging.NewSecrets()
			secrets.Register(secret)
			logger, buf := newLogger(t, f, slog.LevelDebug, secrets)

			var err *nilErr
			var m *nilMarshaler
			require.NotPanics(t, func() { logger.Info("m", "err", err, "text", m) })

			assert.Equal(t, 2, strings.Count(buf.String(), "<nil>"), "output %q", buf.String())
		})
	}
}

// Durations, times, and floats are written differently by the JSON format
// (encoding/json) than by the text formats (Value.String); a secret that
// matches either form is redacted.
func TestRedactsNonStringValuesWhoseJSONIsASecret(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	tsJSON, err := ts.MarshalJSON()
	require.NoError(t, err)
	for _, tc := range []struct {
		name, secret string
		attr         slog.Attr
	}{
		{"duration", "1500000000", slog.Duration("d", 1500*time.Millisecond)},
		{"time", strings.Trim(string(tsJSON), `"`), slog.Time("t", ts)},
		{"float", "100000000000000000000", slog.Float64("f", 1e20)},
	} {
		for _, f := range formats {
			t.Run(tc.name+"/"+string(f), func(t *testing.T) {
				secrets := logging.NewSecrets()
				secrets.Register(tc.secret)
				logger, buf := newLogger(t, f, slog.LevelDebug, secrets)

				logger.LogAttrs(t.Context(), slog.LevelInfo, "m", tc.attr)

				assert.NotContains(t, buf.String(), tc.secret, "output %q", buf.String())
			})
		}
	}
}
