package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// readHeaderTimeout bounds how long a client may take to send request headers.
const readHeaderTimeout = 10 * time.Second

// idleTimeout bounds how long a keep-alive connection may wait for its next
// request; each idle connection holds a goroutine and a file descriptor.
// There is deliberately no WriteTimeout: it would cut off long-lived
// streaming responses (server-sent events).
const idleTimeout = 120 * time.Second

// Serve serves h on ln until ctx is canceled, then shuts down gracefully: it
// stops accepting connections and waits for in-flight requests for at most
// shutdownTimeout before closing the remaining connections. Errors that
// net/http reports itself (such as TLS handshake failures or superfluous
// WriteHeader calls) go to logger at warn level; nil means slog.Default().
func Serve(ctx context.Context, ln net.Listener, h http.Handler, logger *slog.Logger, shutdownTimeout time.Duration) error {
	return serve(ctx, ln, h, logger, shutdownTimeout, idleTimeout)
}

func serve(ctx context.Context, ln net.Listener, h http.Handler, logger *slog.Logger, shutdownTimeout, idle time.Duration) error {
	if logger == nil {
		logger = slog.Default()
	}
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idle,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		return fmt.Errorf("serve http: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		_ = srv.Close()
		return fmt.Errorf("graceful shutdown after %s: %w", shutdownTimeout, err)
	}
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve http: %w", err)
	}
	return nil
}
