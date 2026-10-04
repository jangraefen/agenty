package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/gin-gonic/gin"

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
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return v
}

func assertProblem(t *testing.T, rec *httptest.ResponseRecorder, status int, instance string) httpapi.Problem {
	t.Helper()
	if rec.Code != status {
		t.Errorf("status = %d, want %d", rec.Code, status)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", got)
	}
	p := decode[httpapi.Problem](t, rec)
	want := httpapi.Problem{Type: "about:blank", Title: http.StatusText(status), Status: status, Detail: p.Detail, Instance: instance}
	if p != want {
		t.Errorf("problem = %+v, want %+v", p, want)
	}
	if p.Detail == "" {
		t.Error("problem detail is empty")
	}
	return p
}

func TestHealthzReportsActiveRoles(t *testing.T) {
	h := httpapi.NewRouter(httpapi.Options{Roles: []string{"api", "scheduler"}})

	rec := do(t, h, http.MethodGet, "/healthz")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := decode[struct {
		Status string   `json:"status"`
		Roles  []string `json:"roles"`
	}](t, rec)
	if body.Status != "ok" {
		t.Errorf("status = %q, want ok", body.Status)
	}
	if !slices.Equal(body.Roles, []string{"api", "scheduler"}) {
		t.Errorf("roles = %v, want [api scheduler]", body.Roles)
	}
}

func TestReadyzReportsReadyWithoutChecks(t *testing.T) {
	h := httpapi.NewRouter(httpapi.Options{Roles: []string{"api"}})

	rec := do(t, h, http.MethodGet, "/readyz")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := decode[map[string]string](t, rec)["status"]; got != "ready" {
		t.Errorf("status = %q, want ready", got)
	}
}

func TestReadyzReportsNotReadyAsProblem(t *testing.T) {
	h := httpapi.NewRouter(httpapi.Options{
		Roles: []string{"api"},
		Ready: func(context.Context) error { return errors.New("database unreachable") },
	})

	rec := do(t, h, http.MethodGet, "/readyz")

	p := assertProblem(t, rec, http.StatusServiceUnavailable, "/readyz")
	if p.Detail != "database unreachable" {
		t.Errorf("detail = %q, want %q", p.Detail, "database unreachable")
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
	if got := rec.Header().Get("Allow"); got != http.MethodGet {
		t.Errorf("Allow = %q, want GET", got)
	}
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
	if p.Detail == "secret internal detail" {
		t.Error("problem detail leaks the panic value")
	}
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
	if p.Detail != "short and stout" {
		t.Errorf("detail = %q, want %q", p.Detail, "short and stout")
	}
}
