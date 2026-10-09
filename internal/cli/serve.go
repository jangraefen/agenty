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

// serveFlags are the flags of "agenty serve". serve has no token and no
// workspace, as it is the server rather than a client of one, so it parses
// its flags itself instead of through parseFlags.
type serveFlags struct {
	config, addr string
	logLevel     slog.Level
}

// parseServeFlags parses the flags of "agenty serve" and rejects any
// argument. Like parseFlags it returns ok false, with the exit code, when
// serve is not to run: exitOK after -h, exitUsage after a mistake.
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
	fs.StringVar(&f.addr, "addr", "127.0.0.1:8080", "`address` to listen on; a loopback address, as the API has no TLS yet")
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
//
// It wires the server together: the operator config is loaded and
// resolved, which yields the secrets and their redactor; the store is opened
// on the resolved database URL; server.New builds the API on both; and an
// HTTP server serves it on a loopback listener. Each step needs the one
// before, and any failure ends serve with exitFailure after logging it.
func serve(ctx context.Context, args []string, env Env) int {
	flags, code, ok := parseServeFlags(args, env.Stderr)
	if !ok {
		return code
	}
	// No secret is known before the config is resolved, but the logger
	// goes through a redactor from the start, so every log line takes the
	// same path.
	logger := newLogger(env.Stderr, flags.logLevel, must.Value(secret.NewRedactor(nil)))
	// The address is checked before anything is opened: a mistake here
	// should not cost a database connection first.
	if err := checkLoopback(flags.addr); err != nil {
		return fail(logger, "serve failed", err)
	}
	cfg, err := config.Load(flags.config)
	if err != nil {
		return fail(logger, "serve failed", err)
	}
	resolved, err := cfg.Resolve(env.LookupEnv)
	if err != nil {
		return fail(logger, "serve failed", err)
	}
	// From here on every log line is redacted of everything read from the
	// environment: the model API key, the database URL, the MCP servers'
	// environments and the users' tokens (guarantee 5).
	logger = newLogger(env.Stderr, flags.logLevel, resolved.Redactor)

	st, err := store.Open(ctx, resolved.DatabaseURL)
	if err != nil {
		return fail(logger, "serve failed", err)
	}
	defer st.Close()
	// server.New takes the parsed config too, not only the resolved values:
	// it needs central policy, users, workspaces, MCP servers and limits.
	// env.Server is nil outside tests, so MCP servers start as subprocesses.
	srv, err := server.New(ctx, server.Config{Store: st, Operator: cfg, Resolved: resolved, Logger: logger, Server: env.Server})
	if err != nil {
		return fail(logger, "serve failed", err)
	}
	defer srv.Close()

	// The listener is opened only once the server is ready, so a client
	// that can connect finds the API working.
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", flags.addr)
	if err != nil {
		return fail(logger, "serve failed", err)
	}
	// A name such as localhost is resolved when listening; check what it
	// resolved to.
	if err := checkLoopback(ln.Addr().String()); err != nil {
		return fail(logger, "serve failed", errors.Join(err, ln.Close()))
	}
	// ReadHeaderTimeout bounds how long a client may hold a connection
	// before sending its headers. No write timeout is set: run event
	// streams stay open for as long as a run lasts.
	httpServer := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- httpServer.Serve(ln) }()
	logger.Info("serving the API", "addr", "http://"+ln.Addr().String())

	// Serve returns only on failure until Shutdown is called, so whichever
	// comes first decides: a failed listener, or the signal to stop.
	select {
	case err := <-served:
		return fail(logger, "serve failed", err)
	case <-ctx.Done():
	}
	logger.Info("stopping")
	// Runs end first, so their event streams finish and the HTTP server can
	// shut down.
	srv.Close()
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fail(logger, "serve failed", err)
	}
	return exitOK
}

// checkLoopback refuses any address but a loopback one: until the API has
// TLS, only the local machine may reach it, so tokens never cross a network
// unencrypted. The name "localhost" is accepted as written, since it is
// resolved only when listening; serve checks the resolved address again.
// The server's localOnly middleware complements this by refusing requests
// for a non-local Host, as a DNS-rebound page would send.
func checkLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("--addr: %w", err)
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("--addr %s: the API has no TLS yet, so it listens on a loopback address only, such as 127.0.0.1", addr)
	}
	return nil
}
