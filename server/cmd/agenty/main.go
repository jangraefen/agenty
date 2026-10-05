// Command agenty is the entry point of the agenty server binary.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jangraefen/agenty/server/internal/buildinfo"
	"github.com/jangraefen/agenty/server/internal/config"
	"github.com/jangraefen/agenty/server/internal/database"
	"github.com/jangraefen/agenty/server/internal/httpapi"
	"github.com/jangraefen/agenty/server/internal/logging"
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
		// ctx is also canceled by the deferred stop when runContext returns
		// without a signal, for example after --version or a failure.
		if sig, ok := shutdownSignal(ctx); ok {
			slog.InfoContext(context.WithoutCancel(ctx), "shutting down gracefully; send the signal again to force exit", "signal", sig)
		}
	}()
	return runContext(ctx, args, stdout, stderr, options{setDefaultLogger: slog.SetDefault})
}

// shutdownSignal reports the name of the signal that canceled ctx, a context
// from signal.NotifyContext. ok is false when ctx was canceled by its stop
// function instead.
func shutdownSignal(ctx context.Context) (sig string, ok bool) {
	cause := context.Cause(ctx)
	// The signal cause matches context.Canceled under errors.Is, so only the
	// identity comparison tells the plain cancellation by stop apart.
	if cause == nil || cause == context.Canceled {
		return "", false
	}
	return strings.TrimSuffix(cause.Error(), " signal received"), true
}

// options adjust runContext for tests.
type options struct {
	// onStarted, if set, is called once all selected roles are set up (the
	// migrations may still be running), with the address of the HTTP
	// listener, or nil when the api role is inactive.
	onStarted func(net.Addr)
	// migrations, if set, replaces the migrations embedded in the binary.
	migrations fs.FS
	// setDefaultLogger, if set, is called with the server logger before the
	// roles start. run sets it to slog.SetDefault; tests leave it nil so that
	// they do not replace the process-wide default logger.
	setDefaultLogger func(*slog.Logger)
}

// runContext is run with an explicit lifetime: the server stops when ctx is
// canceled.
func runContext(ctx context.Context, args []string, stdout, stderr io.Writer, opts options) int {
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
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
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
	logger, err := newLogger(cfg, stderr)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	// Libraries and the standard log package log through the redacting
	// logger, too.
	if opts.setDefaultLogger != nil {
		opts.setDefaultLogger(logger)
	}
	if err := serve(ctx, cfg, logger, selected, opts); err != nil {
		logger.ErrorContext(context.WithoutCancel(ctx), "agenty failed", "error", err)
		return 1
	}
	return 0
}

// newLogger builds the redacting logger from cfg. Secret values known from the
// configuration, such as the database password, are registered before the
// first record is written, so they never reach the logs (ARCHITECTURE §3
// invariant 6). Components register further secrets as they load them.
func newLogger(cfg config.Config, w io.Writer) (*slog.Logger, error) {
	secrets := logging.NewSecrets()
	secrets.Register(cfg.Database.Secrets()...)
	return logging.New(w, logging.Options{Format: cfg.Log.Format, Level: cfg.Log.Level}, secrets)
}

// serve starts the selected roles and blocks until ctx is canceled and all of
// them have stopped. The api role serves at once, so /healthz answers while
// the database migrations run in the background; /readyz reports not ready,
// and the worker and scheduler roles wait, until the migrations have been
// applied. A failed migration stops the process with an error. opts.onStarted,
// if set, is called once all roles are set up.
func serve(ctx context.Context, cfg config.Config, logger *slog.Logger, selected []roles.Role, opts options) error {
	pool, migrator, err := openDatabase(ctx, cfg.Database, opts.migrations)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()
	defer func() { _ = migrator.Close() }()

	// migrated is closed once the migrations have been applied.
	migrated := make(chan struct{})
	ready := func(ctx context.Context) error {
		select {
		case <-migrated:
			return database.Ready(ctx, pool, migrator)
		default:
			return errors.New("database migrations have not completed")
		}
	}
	components, listenAddr, err := setUpRoles(ctx, cfg.Server, logger, selected, ready, migrated)
	if err != nil {
		return err
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	migrateErr := migrateInBackground(runCtx, logger, cancel, migrator, cfg.Database, migrated)
	logger.InfoContext(ctx, "starting agenty", "version", buildinfo.Version, "roles", roles.Strings(selected))
	if opts.onStarted != nil {
		opts.onStarted(listenAddr)
	}
	runErr := roles.Run(runCtx, components)
	cancel()
	err = errors.Join(<-migrateErr, runErr)
	logger.InfoContext(context.WithoutCancel(ctx), "agenty stopped")
	return err
}

// openDatabase opens the connection pool and a Migrator for migrations, or
// for the embedded migrations when migrations is nil.
func openDatabase(ctx context.Context, cfg config.Database, migrations fs.FS) (*pgxpool.Pool, *database.Migrator, error) {
	pool, err := database.Open(ctx, cfg.URL)
	if err != nil {
		return nil, nil, err
	}
	var migratorOpts []database.MigratorOption
	if migrations != nil {
		migratorOpts = append(migratorOpts, database.WithMigrations(migrations))
	}
	migrator, err := database.NewMigrator(pool, migratorOpts...)
	if err != nil {
		pool.Close()
		return nil, nil, err
	}
	return pool, migrator, nil
}

// setUpRoles creates the components of the selected roles. The api role
// listens at once and reports readiness with ready; the other roles start
// once migrated is closed. listenAddr is nil when the api role is inactive.
func setUpRoles(ctx context.Context, cfg config.Server, logger *slog.Logger, selected []roles.Role, ready func(context.Context) error,
	migrated <-chan struct{},
) (components map[roles.Role]roles.Component, listenAddr net.Addr, err error) {
	components = make(map[roles.Role]roles.Component, len(selected))
	for _, r := range selected {
		switch r {
		case roles.API:
			ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.Address)
			if err != nil {
				return nil, nil, fmt.Errorf("listen on %s: %w", cfg.Address, err)
			}
			logger.InfoContext(ctx, "api role listening", "address", ln.Addr().String())
			listenAddr = ln.Addr()
			components[r] = apiComponent{
				ln:      ln,
				handler: httpapi.NewRouter(httpapi.Options{Roles: roles.Strings(selected), Ready: ready, Logger: logger}),
				cfg:     cfg,
			}
		case roles.Worker, roles.Scheduler:
			// No behavior yet; the role is active and stops cleanly. It needs
			// the schema, so it starts only after the migrations.
			components[r] = afterMigrations{migrated: migrated, next: roles.Idle()}
		}
	}
	return components, listenAddr, nil
}

// migrateInBackground applies the migrations and closes migrated once they
// are applied. When they fail, it logs the error and calls stop. The returned
// channel yields the failure, or nil when the migrations were applied or
// interrupted because ctx was canceled.
func migrateInBackground(ctx context.Context, logger *slog.Logger, stop context.CancelFunc, migrator *database.Migrator, cfg config.Database,
	migrated chan<- struct{},
) <-chan error {
	result := make(chan error, 1)
	go func() {
		logger.InfoContext(ctx, "migrating database", "database", cfg)
		err := migrator.Up(ctx)
		switch {
		case err == nil:
			logger.InfoContext(ctx, "database migrations complete")
			close(migrated)
		case ctx.Err() != nil:
			// The process is stopping; the interrupted migration is not a failure.
			err = nil
		default:
			err = fmt.Errorf("database: %w", err)
			logger.ErrorContext(ctx, "database migrations failed; stopping", "error", err)
			stop()
		}
		result <- err
	}()
	return result
}

// afterMigrations runs next once migrated is closed.
type afterMigrations struct {
	migrated <-chan struct{}
	next     roles.Component
}

func (a afterMigrations) Run(ctx context.Context) error {
	select {
	case <-a.migrated:
		return a.next.Run(ctx)
	case <-ctx.Done():
		return nil
	}
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
