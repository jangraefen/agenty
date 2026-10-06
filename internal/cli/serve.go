package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/jangraefen/agenty/internal/config"
	"github.com/jangraefen/agenty/internal/must"
	"github.com/jangraefen/agenty/internal/secret"
	"github.com/jangraefen/agenty/internal/server"
	"github.com/jangraefen/agenty/internal/store"
)

// serveFlags are the flags of "agenty serve".
type serveFlags struct {
	config, addr string
	logLevel     slog.Level
}

func parseServeFlags(args []string, stderr io.Writer) (serveFlags, int, bool) {
	var f serveFlags
	fs := flag.NewFlagSet("agenty serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		if _, err := io.WriteString(stderr, "usage: agenty serve [flags]\n\nServes the HTTP API on localhost.\n\nFlags:\n"); err == nil {
			fs.PrintDefaults()
		}
	}
	fs.StringVar(&f.config, "config", "agenty.yaml", "operator config `file`: model provider, database, MCP servers, central policy")
	fs.StringVar(&f.addr, "addr", "127.0.0.1:8080", "`address` to listen on; a loopback address, as the API has no sign-in yet")
	fs.TextVar(&f.logLevel, "log-level", slog.LevelInfo, "log `level`: debug, info, warn or error")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return f, exitOK, false
		}
		return f, exitUsage, false
	}
	if fs.NArg() != 0 {
		if _, err := fmt.Fprintln(stderr, "agenty serve: no arguments expected"); err != nil {
			return f, exitFailure, false
		}
		fs.Usage()
		return f, exitUsage, false
	}
	return f, exitOK, true
}

// serve serves the API until ctx ends, then stops the server's runs.
func serve(ctx context.Context, args []string, env Env) int {
	flags, code, ok := parseServeFlags(args, env.Stderr)
	if !ok {
		return code
	}
	logger := newLogger(env.Stderr, flags.logLevel, must.Value(secret.NewRedactor(nil)))
	if err := checkLoopback(flags.addr); err != nil {
		return failServe(logger, err)
	}
	cfg, err := config.Load(flags.config)
	if err != nil {
		return failServe(logger, err)
	}
	resolved, err := cfg.Resolve(env.LookupEnv)
	if err != nil {
		return failServe(logger, err)
	}
	logger = newLogger(env.Stderr, flags.logLevel, resolved.Redactor)
	if resolved.DatabaseURL == "" {
		return failServe(logger, errors.New("config: database.url is required to serve"))
	}

	st, err := store.Open(ctx, resolved.DatabaseURL)
	if err != nil {
		return failServe(logger, err)
	}
	defer st.Close()
	srv, err := server.New(ctx, server.Config{Store: st, Operator: cfg, Resolved: resolved, Logger: logger, Server: env.Server})
	if err != nil {
		return failServe(logger, err)
	}
	defer srv.Close()

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", flags.addr)
	if err != nil {
		return failServe(logger, err)
	}
	// A name such as localhost is resolved when listening; check what it
	// resolved to.
	if err := checkLoopback(ln.Addr().String()); err != nil {
		return failServe(logger, errors.Join(err, ln.Close()))
	}
	httpServer := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- httpServer.Serve(ln) }()
	logger.Info("serving the API", "addr", "http://"+ln.Addr().String())

	select {
	case err := <-served:
		return failServe(logger, err)
	case <-ctx.Done():
	}
	logger.Info("stopping")
	// Runs end first, so their event streams finish and the HTTP server can
	// shut down.
	srv.Close()
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return failServe(logger, err)
	}
	return exitOK
}

// checkLoopback refuses any address but a loopback one: until the API has
// sign-in, only the local machine may reach it.
func checkLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("--addr: %w", err)
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("--addr %s: the API has no sign-in yet, so it listens on a loopback address only, such as 127.0.0.1", addr)
	}
	return nil
}

func failServe(logger *slog.Logger, err error) int {
	logger.Error("serve failed", "error", err)
	return exitFailure
}
