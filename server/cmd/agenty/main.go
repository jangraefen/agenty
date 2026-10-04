// Command agenty is the entry point of the agenty server binary.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jangraefen/agenty/server/internal/buildinfo"
	"github.com/jangraefen/agenty/server/internal/config"
	"github.com/jangraefen/agenty/server/internal/database"
	"github.com/jangraefen/agenty/server/internal/httpapi"
	"github.com/jangraefen/agenty/server/internal/roles"
)

const name = "agenty"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes the command with the given arguments and returns the exit code.
// The first SIGINT or SIGTERM stops the server gracefully; a second one
// terminates the process immediately.
func run(args []string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		// Restore the default signal behavior so that a second signal
		// terminates the process while the graceful shutdown waits.
		stop()
		slog.InfoContext(context.WithoutCancel(ctx), "shutting down gracefully; send the signal again to force exit")
	}()
	return runContext(ctx, args, stdout, stderr, nil)
}

// runContext is run with an explicit lifetime: the server stops when ctx is
// canceled. onStarted, if set, is called once all selected roles are set up,
// with the address of the HTTP listener, or nil when the api role is inactive.
func runContext(ctx context.Context, args []string, stdout, stderr io.Writer, onStarted func(net.Addr)) int {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage: %s --config <file> [--roles <roles>]\n       %s --version\n\nFlags:\n", name, name)
		flags.PrintDefaults()
	}
	showVersion := flags.Bool("version", false, "print the version and exit")
	configPath := flags.String("config", "", "path to the YAML configuration file (required to start the server)")
	roleList := flags.String("roles", strings.Join(roles.Strings(roles.All()), ","),
		"comma-separated roles to start: any of api, worker, scheduler")

	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		_, _ = fmt.Fprintf(stdout, "%s %s\n", name, buildinfo.Version)
		return 0
	}
	if len(args) == 0 {
		flags.Usage()
		return 2
	}
	if *configPath == "" {
		_, _ = fmt.Fprintln(stderr, "missing required flag --config")
		flags.Usage()
		return 2
	}
	selected, err := roles.Parse(*roleList)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "invalid --roles: %v\n", err)
		return 2
	}
	cfg, err := config.Load(*configPath, os.LookupEnv)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	if err := serve(ctx, cfg, selected, onStarted); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

// serve starts the selected roles and blocks until ctx is canceled and all of
// them have stopped. onStarted, if set, is called once all roles are set up.
func serve(ctx context.Context, cfg config.Config, selected []roles.Role, onStarted func(net.Addr)) error {
	pool, err := database.Open(ctx, cfg.Database.URL)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()
	migrator, err := database.NewMigrator(pool)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer func() { _ = migrator.Close() }()
	slog.InfoContext(ctx, "migrating database", "database", cfg.Database)
	if err := migrator.Up(ctx); err != nil {
		return fmt.Errorf("database: %w", err)
	}

	components := make(map[roles.Role]roles.Component, len(selected))
	var listenAddr net.Addr
	for _, r := range selected {
		switch r {
		case roles.API:
			ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.Server.Address)
			if err != nil {
				return fmt.Errorf("listen on %s: %w", cfg.Server.Address, err)
			}
			slog.InfoContext(ctx, "api role listening", "address", ln.Addr().String())
			listenAddr = ln.Addr()
			components[r] = apiComponent{
				ln: ln,
				handler: httpapi.NewRouter(httpapi.Options{
					Roles: roles.Strings(selected),
					Ready: func(ctx context.Context) error { return database.Ready(ctx, pool, migrator) },
				}),
				cfg: cfg.Server,
			}
		case roles.Worker, roles.Scheduler:
			// No behavior yet; the role is active and stops cleanly.
			components[r] = roles.Idle()
		}
	}

	slog.InfoContext(ctx, "starting agenty", "version", buildinfo.Version, "roles", roles.Strings(selected))
	if onStarted != nil {
		onStarted(listenAddr)
	}
	err = roles.Run(ctx, components)
	slog.InfoContext(context.WithoutCancel(ctx), "agenty stopped")
	return err
}

// apiComponent serves the public HTTP API until stopped.
type apiComponent struct {
	ln      net.Listener
	handler http.Handler
	cfg     config.Server
}

func (a apiComponent) Run(ctx context.Context) error {
	return httpapi.Serve(ctx, a.ln, a.handler, a.cfg.ShutdownTimeout)
}
