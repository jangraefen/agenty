package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// RequestLogger returns Gin middleware that logs one record per request
// after it completes: method, path, matched route, status, response size,
// and duration. Server errors (5xx) are logged at error level, everything
// else at info. The query string is never logged, because it may carry
// tokens; values registered as secrets are removed by the logger's
// redacting handler.
func RequestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		level := slog.LevelInfo
		if status >= http.StatusInternalServerError {
			level = slog.LevelError
		}
		attrs := []slog.Attr{
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.Int("status", status),
			slog.Int("bytes", max(c.Writer.Size(), 0)),
			slog.Duration("duration", time.Since(start)),
		}
		if route := c.FullPath(); route != "" {
			attrs = append(attrs, slog.String("route", route))
		}
		logger.LogAttrs(c.Request.Context(), level, "http request", attrs...)
	}
}
