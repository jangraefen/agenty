package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/server/internal/httpapi"
	"github.com/jangraefen/agenty/server/internal/logging"
)

const testSecret = "db-pa55word-0a1b2c"

// jsonLogger returns a logger that writes JSON through the redacting handler,
// with testSecret registered, and a function returning the records so far.
func jsonLogger(t *testing.T) (*slog.Logger, func() []map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	secrets := logging.NewSecrets()
	secrets.Register(testSecret)
	logger, err := logging.New(&buf, logging.Options{Format: logging.FormatJSON, Level: slog.LevelDebug}, secrets)
	require.NoError(t, err, "logging.New")
	return logger, func() []map[string]any {
		t.Helper()
		assert.NotContains(t, buf.String(), testSecret, "log output leaks the secret")
		var records []map[string]any
		for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
			if line == "" {
				continue
			}
			var rec map[string]any
			require.NoError(t, json.Unmarshal([]byte(line), &rec), "log line %q", line)
			records = append(records, rec)
		}
		return records
	}
}

func requestRecords(records []map[string]any) []map[string]any {
	var out []map[string]any
	for _, r := range records {
		if r["msg"] == "http request" {
			out = append(out, r)
		}
	}
	return out
}

func TestRequestLoggingRecordsEachRequest(t *testing.T) {
	logger, records := jsonLogger(t)
	h := httpapi.NewRouter(httpapi.Options{Roles: []string{roleAPI}, Logger: logger})

	rec := do(t, h, http.MethodGet, "/healthz?token=query-secret")

	require.Equal(t, http.StatusOK, rec.Code)
	reqs := requestRecords(records())
	require.Len(t, reqs, 1)
	r := reqs[0]
	assert.Equal(t, "debug", r["level"], "probes are logged at debug level")
	assert.Equal(t, "GET", r["method"])
	assert.Equal(t, "/healthz", r["path"])
	assert.Equal(t, "/healthz", r["route"])
	assert.InDelta(t, http.StatusOK, r["status"], 0)
	assert.Contains(t, r, "duration")
	assert.Contains(t, r, "bytes")
	raw, err := json.Marshal(r)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "query-secret", "query strings are not logged")
}

func TestRequestLoggingRecordsUnknownRoutes(t *testing.T) {
	logger, records := jsonLogger(t)
	h := httpapi.NewRouter(httpapi.Options{Roles: []string{roleAPI}, Logger: logger})

	do(t, h, http.MethodGet, "/no/such/path")

	reqs := requestRecords(records())
	require.Len(t, reqs, 1)
	assert.Equal(t, "info", reqs[0]["level"])
	assert.InDelta(t, http.StatusNotFound, reqs[0]["status"], 0)
	assert.Equal(t, "/no/such/path", reqs[0]["path"])
	assert.NotContains(t, reqs[0], "route", "no route matched")
}

func TestRequestLoggingRedactsSecretsInPath(t *testing.T) {
	logger, records := jsonLogger(t)
	h := httpapi.NewRouter(httpapi.Options{Roles: []string{roleAPI}, Logger: logger})

	do(t, h, http.MethodGet, "/x/"+testSecret)

	reqs := requestRecords(records())
	require.Len(t, reqs, 1)
	assert.Equal(t, "/x/[REDACTED]", reqs[0]["path"])
}

func TestPanicIsLoggedAsErrorWithRedaction(t *testing.T) {
	logger, records := jsonLogger(t)
	h := httpapi.NewRouter(httpapi.Options{
		Roles:  []string{roleAPI},
		Logger: logger,
		Routes: func(r gin.IRouter) {
			r.GET("/boom", func(*gin.Context) { panic(errors.New("cannot connect with " + testSecret)) })
		},
	})

	rec := do(t, h, http.MethodGet, "/boom")

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	all := records()
	var panics []map[string]any
	for _, r := range all {
		if r["msg"] == "panic while handling request" {
			panics = append(panics, r)
		}
	}
	require.Len(t, panics, 1, "records %v", all)
	assert.Equal(t, "error", panics[0]["level"])
	assert.Equal(t, "cannot connect with [REDACTED]", panics[0]["panic"])
	stack, ok := panics[0]["stack"].(string)
	require.True(t, ok, "stack attribute missing or not a string: %v", panics[0])
	assert.Contains(t, stack, "TestPanicIsLoggedAsErrorWithRedaction", "stack names the panicking function")
	reqs := requestRecords(all)
	require.Len(t, reqs, 1)
	assert.Equal(t, "error", reqs[0]["level"], "server errors are logged at error level")
	assert.InDelta(t, http.StatusInternalServerError, reqs[0]["status"], 0)
	assert.Equal(t, "/boom", reqs[0]["route"])
}

func TestReadinessFailureIsLoggedWithRedaction(t *testing.T) {
	logger, records := jsonLogger(t)
	h := httpapi.NewRouter(httpapi.Options{
		Roles:  []string{roleAPI},
		Logger: logger,
		Ready: func(context.Context) error {
			return errors.New("dial postgres://agenty:" + testSecret + "@db:5432")
		},
	})

	do(t, h, http.MethodGet, "/readyz")

	all := records()
	var found bool
	for _, r := range all {
		if r["msg"] == "readiness check failed" {
			found = true
			assert.Equal(t, "warn", r["level"])
			assert.Equal(t, "dial postgres://agenty:[REDACTED]@db:5432", r["error"])
		}
	}
	assert.True(t, found, "no readiness failure record in %v", all)
	reqs := requestRecords(all)
	require.Len(t, reqs, 1)
	assert.Equal(t, "debug", reqs[0]["level"], "the 503 is reported by the warn record above")
}

func TestRequestLoggerMiddlewareStandalone(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(httpapi.RequestLogger(logger))
	r.POST("/items/:id", func(c *gin.Context) { c.String(http.StatusTeapot, "short") })

	rec := do(t, r, http.MethodPost, "/items/42")

	require.Equal(t, http.StatusTeapot, rec.Code)
	var got map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got), "log %q", buf.String())
	assert.Equal(t, "INFO", got["level"])
	assert.Equal(t, "POST", got["method"])
	assert.Equal(t, "/items/42", got["path"])
	assert.Equal(t, "/items/:id", got["route"])
	assert.InDelta(t, http.StatusTeapot, got["status"], 0)
	assert.InDelta(t, len("short"), got["bytes"], 0)
}

func TestRequestLoggerLogsQuietRoutesAtDebugLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(httpapi.RequestLogger(logger, "/probe"))
	r.GET("/probe", func(c *gin.Context) { c.Status(http.StatusServiceUnavailable) })
	r.GET("/other", func(c *gin.Context) { c.Status(http.StatusServiceUnavailable) })

	do(t, r, http.MethodGet, "/probe")
	do(t, r, http.MethodGet, "/other")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 2, "log %q", buf.String())
	var probe, other map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &probe))
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &other))
	assert.Equal(t, "DEBUG", probe["level"], "quiet routes are logged at debug level whatever their status")
	assert.Equal(t, "ERROR", other["level"])
}
