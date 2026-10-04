package httpapi_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/jangraefen/agenty/server/internal/httpapi"
)

// recordingServer implements the generated ServerInterface and records which
// operations were called.
type recordingServer struct{ called []string }

func (s *recordingServer) GetHealthz(c *gin.Context) {
	s.called = append(s.called, "getHealthz")
	c.Status(http.StatusNoContent)
}

func (s *recordingServer) GetReadyz(c *gin.Context) {
	s.called = append(s.called, "getReadyz")
	c.Status(http.StatusNoContent)
}

var operations = map[string]string{"/healthz": "getHealthz", "/readyz": "getReadyz"}

func TestGeneratedRegisterHandlersRoutesEveryOperation(t *testing.T) {
	for path, op := range operations {
		t.Run(op, func(t *testing.T) {
			s := &recordingServer{}
			r := gin.New()
			httpapi.RegisterHandlers(r, s)

			rec := do(t, r, http.MethodGet, path)

			assert.Equal(t, http.StatusNoContent, rec.Code, "status")
			assert.Equal(t, []string{op}, s.called, "called operations")
		})
	}
}

func TestGeneratedMiddlewaresRunBeforeEveryOperation(t *testing.T) {
	for path, op := range operations {
		t.Run(op, func(t *testing.T) {
			for name, tc := range map[string]struct {
				abort      bool
				wantCalled []string
			}{
				"passing middleware":  {abort: false, wantCalled: []string{op}},
				"aborting middleware": {abort: true, wantCalled: nil},
			} {
				t.Run(name, func(t *testing.T) {
					s := &recordingServer{}
					var ran bool
					r := gin.New()
					httpapi.RegisterHandlersWithOptions(r, s, httpapi.GinServerOptions{
						Middlewares: []httpapi.MiddlewareFunc{func(c *gin.Context) {
							ran = true
							if tc.abort {
								c.AbortWithStatus(http.StatusForbidden)
							}
						}},
					})

					do(t, r, http.MethodGet, path)

					assert.True(t, ran, "middleware did not run")
					assert.Equal(t, tc.wantCalled, s.called, "called operations")
				})
			}
		})
	}
}

func TestParameterErrorsAreProblemsWithoutInternals(t *testing.T) {
	r := gin.New()
	r.GET("/params", func(c *gin.Context) {
		httpapi.ParameterProblem(c, errors.New("strconv.Atoi: parsing \"secret\""), http.StatusBadRequest)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/params", http.NoBody))

	p := assertProblem(t, rec, http.StatusBadRequest, "/params")
	assert.NotContains(t, rec.Body.String(), "secret", "problem leaks the parameter error")
	assert.Equal(t, "The request parameters are invalid.", p.Detail, "detail")
}
