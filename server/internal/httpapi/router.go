// Package httpapi serves the public HTTP API of the api role with Gin:
// health and readiness endpoints, RFC 9457 problem details for every error,
// and graceful shutdown.
package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
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
}

var releaseMode = sync.OnceFunc(func() { gin.SetMode(gin.ReleaseMode) })

// NewRouter returns the HTTP handler of the api role.
func NewRouter(opts Options) http.Handler {
	releaseMode()
	r := gin.New()
	r.HandleMethodNotAllowed = true
	r.Use(gin.CustomRecoveryWithWriter(io.Discard, func(c *gin.Context, err any) {
		slog.ErrorContext(c.Request.Context(), "panic while handling request",
			"method", c.Request.Method, "path", c.Request.URL.Path, "panic", err)
		WriteProblem(c, http.StatusInternalServerError, "The server encountered an unexpected error.")
	}))
	r.NoRoute(func(c *gin.Context) {
		WriteProblem(c, http.StatusNotFound, "No resource exists at this path.")
	})
	r.NoMethod(func(c *gin.Context) {
		WriteProblem(c, http.StatusMethodNotAllowed, "The resource does not support this method.")
	})

	activeRoles := append([]string{}, opts.Roles...)
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "roles": activeRoles})
	})
	r.GET("/readyz", func(c *gin.Context) {
		if opts.Ready != nil {
			if err := opts.Ready(c.Request.Context()); err != nil {
				slog.WarnContext(c.Request.Context(), "readiness check failed", "error", err)
				WriteProblem(c, http.StatusServiceUnavailable, "The server is not ready to serve traffic.")
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})
	if opts.Routes != nil {
		opts.Routes(r)
	}
	return r
}
