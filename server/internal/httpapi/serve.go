package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// readHeaderTimeout bounds how long a client may take to send request headers.
const readHeaderTimeout = 10 * time.Second

// Serve serves h on ln until ctx is canceled, then shuts down gracefully: it
// stops accepting connections and waits for in-flight requests for at most
// shutdownTimeout before closing the remaining connections.
func Serve(ctx context.Context, ln net.Listener, h http.Handler, shutdownTimeout time.Duration) error {
	srv := &http.Server{Handler: h, ReadHeaderTimeout: readHeaderTimeout}
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
