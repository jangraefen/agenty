// Package httpapi serves the public HTTP API of the api role with Gin through
// the server interfaces generated from api/openapi.yaml (openapi.gen.go):
// health and readiness endpoints, RFC 9457 problem details for every error,
// and graceful shutdown.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"sync"

	"github.com/gin-gonic/gin"
)

// Options configures the router.
type Options struct {
	// Roles are the active roles, reported by /healthz.
	Roles []string
	// Ready reports whether the server can serve traffic; a non-nil error
	// makes /readyz answer 503 with a generic detail and is logged, never
	// returned to the client. Nil means always ready.
	Ready func(ctx context.Context) error
	// Routes registers additional routes.
	Routes func(r gin.IRouter)
	// Logger receives request logs, readiness failures, and panics; it
	// should redact secrets (logging.New). Nil means slog.Default().
	Logger *slog.Logger
}

var releaseMode = sync.OnceFunc(func() { gin.SetMode(gin.ReleaseMode) })

// NewRouter returns the HTTP handler of the api role.
func NewRouter(opts Options) http.Handler {
	releaseMode()
	r := gin.New()
	r.HandleMethodNotAllowed = true
	// A trailing-slash variant of a route is not a resource: answer it with
	// the 404 problem instead of Gin's default HTML redirect.
	r.RedirectTrailingSlash = false
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	// The request logger runs outside recovery, so it records the 500 that
	// recovery writes after a panic.
	r.Use(RequestLogger(logger, "/healthz", "/readyz"))
	// A nil writer stops Gin from formatting its own copy of the panic; the
	// handler logs it with the stack through the (redacting) logger.
	r.Use(gin.CustomRecoveryWithWriter(nil, func(c *gin.Context, err any) {
		logger.ErrorContext(c.Request.Context(), "panic while handling request",
			"method", c.Request.Method, "path", c.Request.URL.Path, "panic", err, "stack", string(debug.Stack()))
		if c.Writer.Written() {
			// The status line is already sent; a problem body would corrupt it.
			c.Abort()
			return
		}
		WriteProblem(c, http.StatusInternalServerError, "The server encountered an unexpected error.")
	}))
	r.NoRoute(func(c *gin.Context) {
		WriteProblem(c, http.StatusNotFound, "No resource exists at this path.")
	})
	r.NoMethod(func(c *gin.Context) {
		WriteProblem(c, http.StatusMethodNotAllowed, "The resource does not support this method.")
	})

	healthRoles := make([]HealthRoles, len(opts.Roles))
	for i, role := range opts.Roles {
		healthRoles[i] = HealthRoles(role)
	}
	RegisterHandlersWithOptions(r, &server{roles: healthRoles, ready: opts.Ready, logger: logger},
		GinServerOptions{ErrorHandler: parameterProblem})
	if opts.Routes != nil {
		opts.Routes(r)
	}
	return r
}

// parameterProblem answers requests whose parameters the generated code cannot
// bind. The binding error is not returned, because it may echo request data.
func parameterProblem(c *gin.Context, _ error, status int) {
	WriteProblem(c, status, "The request parameters are invalid.")
}

// server implements the operations of the contract in api/openapi.yaml.
type server struct {
	roles  []HealthRoles
	ready  func(ctx context.Context) error
	logger *slog.Logger
}

var _ ServerInterface = (*server)(nil)

// GetHealthz reports liveness and the active roles.
func (s *server) GetHealthz(c *gin.Context) {
	c.JSON(http.StatusOK, Health{Status: HealthStatusOk, Roles: s.roles})
}

// GetReadyz reports whether the server can serve traffic.
func (s *server) GetReadyz(c *gin.Context) {
	if s.ready != nil {
		if err := s.ready(c.Request.Context()); err != nil {
			s.logger.WarnContext(c.Request.Context(), "readiness check failed", "error", err)
			WriteProblem(c, http.StatusServiceUnavailable, "The server is not ready to serve traffic.")
			return
		}
	}
	c.JSON(http.StatusOK, Readiness{Status: ReadinessStatusReady})
}
