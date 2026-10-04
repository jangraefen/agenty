package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/server/internal/httpapi"
)

func do(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), method, target, http.NoBody))
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &v), "decode body %q", rec.Body.String())
	return v
}

func assertProblem(t *testing.T, rec *httptest.ResponseRecorder, status int, instance string) httpapi.Problem {
	t.Helper()
	assert.Equal(t, status, rec.Code, "status")
	assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"), "Content-Type")
	p := decode[httpapi.Problem](t, rec)
	want := httpapi.Problem{Type: "about:blank", Title: http.StatusText(status), Status: status, Detail: p.Detail, Instance: instance}
	assert.Equal(t, want, p, "problem")
	assert.NotEmpty(t, p.Detail, "problem detail is empty")
	return p
}

func TestHealthzReportsActiveRoles(t *testing.T) {
	h := httpapi.NewRouter(httpapi.Options{Roles: []string{"api", "scheduler"}})

	rec := do(t, h, http.MethodGet, "/healthz")

	require.Equal(t, http.StatusOK, rec.Code, "status")
	body := decode[struct {
		Status string   `json:"status"`
		Roles  []string `json:"roles"`
	}](t, rec)
	assert.Equal(t, "ok", body.Status, "status")
	assert.Equal(t, []string{"api", "scheduler"}, body.Roles, "roles")
}

func TestReadyzReportsReadyWithoutChecks(t *testing.T) {
	h := httpapi.NewRouter(httpapi.Options{Roles: []string{"api"}})

	rec := do(t, h, http.MethodGet, "/readyz")

	require.Equal(t, http.StatusOK, rec.Code, "status")
	assert.Equal(t, "ready", decode[map[string]string](t, rec)["status"], "status")
}

func TestReadyzReportsNotReadyAsProblem(t *testing.T) {
	h := httpapi.NewRouter(httpapi.Options{
		Roles: []string{"api"},
		Ready: func(context.Context) error { return errors.New("database unreachable") },
	})

	rec := do(t, h, http.MethodGet, "/readyz")

	assertProblem(t, rec, http.StatusServiceUnavailable, "/readyz")
}

func TestReadyzDoesNotLeakCheckError(t *testing.T) {
	const checkErr = "dial tcp db.internal:5432: user=agenty database=agenty"
	h := httpapi.NewRouter(httpapi.Options{
		Roles: []string{"api"},
		Ready: func(context.Context) error { return errors.New(checkErr) },
	})

	rec := do(t, h, http.MethodGet, "/readyz")

	assertProblem(t, rec, http.StatusServiceUnavailable, "/readyz")
	for _, fragment := range []string{checkErr, "db.internal", "5432", "user=agenty"} {
		assert.NotContains(t, rec.Body.String(), fragment, "response body leaks %q from the readiness error", fragment)
	}
}

func TestUnknownRouteIsProblem(t *testing.T) {
	h := httpapi.NewRouter(httpapi.Options{Roles: []string{"api"}})

	rec := do(t, h, http.MethodGet, "/no/such/path")

	assertProblem(t, rec, http.StatusNotFound, "/no/such/path")
}

func TestWrongMethodIsProblem(t *testing.T) {
	h := httpapi.NewRouter(httpapi.Options{Roles: []string{"api"}})

	rec := do(t, h, http.MethodPost, "/healthz")

	assertProblem(t, rec, http.StatusMethodNotAllowed, "/healthz")
	assert.Equal(t, http.MethodGet, rec.Header().Get("Allow"), "Allow")
}

func TestPanicIsProblemWithoutInternals(t *testing.T) {
	h := httpapi.NewRouter(httpapi.Options{
		Roles: []string{"api"},
		Routes: func(r gin.IRouter) {
			r.GET("/boom", func(*gin.Context) { panic("secret internal detail") })
		},
	})

	rec := do(t, h, http.MethodGet, "/boom")

	p := assertProblem(t, rec, http.StatusInternalServerError, "/boom")
	assert.NotEqual(t, "secret internal detail", p.Detail, "problem detail leaks the panic value")
}

func TestWriteProblemUsesGivenStatusAndDetail(t *testing.T) {
	h := httpapi.NewRouter(httpapi.Options{
		Roles: []string{"api"},
		Routes: func(r gin.IRouter) {
			r.GET("/teapot", func(c *gin.Context) { httpapi.WriteProblem(c, http.StatusTeapot, "short and stout") })
		},
	})

	rec := do(t, h, http.MethodGet, "/teapot")

	p := assertProblem(t, rec, http.StatusTeapot, "/teapot")
	assert.Equal(t, "short and stout", p.Detail, "detail")
}
