// Package cli is the agenty command line. "agenty run" loads the operator
// config and a harness, starts the MCP servers the harness needs, runs the
// agent once on the input, and prints its answer. Approvals are asked at the
// terminal, every tool call is appended to a JSON-lines audit log, and
// secrets are redacted from everything the command prints.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"

	"github.com/jangraefen/agenty/internal/agent"
	"github.com/jangraefen/agenty/internal/audit"
	"github.com/jangraefen/agenty/internal/config"
	"github.com/jangraefen/agenty/internal/harness"
	"github.com/jangraefen/agenty/internal/mcptool"
	"github.com/jangraefen/agenty/internal/model/anthropic"
	"github.com/jangraefen/agenty/internal/must"
	"github.com/jangraefen/agenty/internal/secret"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

// Exit codes.
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

const usage = `usage: agenty run [flags] INPUT

Runs a harness once on INPUT and prints the answer.

`

// Env is the process around a command: its streams, its environment, and
// what runs an MCP server.
type Env struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// Interactive reports whether Stdin is a terminal. Without one, calls
	// that need approval are rejected.
	Interactive bool
	// User names the person at the terminal; approvals record it.
	User      string
	LookupEnv func(string) (string, bool)
	// Server returns the tool server for a configured MCP server: srv
	// itself outside of tests. The gateway starts it if a grant needs it.
	Server func(name string, srv mcptool.Server) toolgateway.ToolServer
}

// Main runs the command line args, without the program name, and returns the
// exit code: 0 on success, 1 when the command fails, 2 on a usage error.
func Main(ctx context.Context, args []string, env Env) int {
	if len(args) == 0 {
		return printUsage(env.Stderr, "", exitUsage)
	}
	switch args[0] {
	case "run":
		return run(ctx, args[1:], env)
	case "help", "-h", "--help":
		return printUsage(env.Stderr, "", exitOK)
	default:
		return printUsage(env.Stderr, fmt.Sprintf("unknown command %q\n", args[0]), exitUsage)
	}
}

func printUsage(w io.Writer, msg string, code int) int {
	if _, err := io.WriteString(w, msg+usage+"Run \"agenty run -h\" for its flags.\n"); err != nil {
		return exitFailure
	}
	return code
}

// runFlags are the flags of "agenty run".
type runFlags struct {
	config, harness, audit string
	logLevel               slog.Level
	input                  string
}

func parseRunFlags(args []string, stderr io.Writer) (runFlags, int, bool) {
	var f runFlags
	fs := flag.NewFlagSet("agenty run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		if _, err := io.WriteString(stderr, usage+"Flags:\n"); err == nil {
			fs.PrintDefaults()
		}
	}
	fs.StringVar(&f.config, "config", "agenty.yaml", "operator config `file`: model provider, MCP servers, central policy")
	fs.StringVar(&f.harness, "harness", "", "harness `file` to run (required)")
	fs.StringVar(&f.audit, "audit", "audit.jsonl", "audit log `file`, appended to")
	fs.TextVar(&f.logLevel, "log-level", slog.LevelInfo, "log `level`: debug, info, warn or error")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return f, exitOK, false
		}
		return f, exitUsage, false
	}
	var problem string
	switch {
	case f.harness == "":
		problem = "--harness is required"
	case fs.NArg() != 1:
		problem = "exactly one input is required; quote it if it has spaces"
	}
	if problem != "" {
		// Report it the way the flag package reports a bad flag.
		if _, err := fmt.Fprintln(stderr, "agenty run:", problem); err != nil {
			return f, exitFailure, false
		}
		fs.Usage()
		return f, exitUsage, false
	}
	f.input = fs.Arg(0)
	return f, exitOK, true
}

func run(ctx context.Context, args []string, env Env) int {
	flags, code, ok := parseRunFlags(args, env.Stderr)
	if !ok {
		return code
	}
	// No secret is known before the config is resolved, so this logger
	// redacts nothing; it only reports failures to get that far.
	logger := newLogger(env.Stderr, flags.logLevel, must.Value(secret.NewRedactor(nil)))

	cfg, err := config.Load(flags.config)
	if err != nil {
		return fail(logger, err)
	}
	h, err := harness.Load(flags.harness)
	if err != nil {
		return fail(logger, err)
	}
	resolved, err := cfg.Resolve(env.LookupEnv)
	if err != nil {
		return fail(logger, err)
	}
	// Resolve rejects secrets too short to redact.
	redact := must.Value(secret.NewRedactor(resolved.Secrets))
	logger = newLogger(env.Stderr, flags.logLevel, redact)

	output, err := runHarness(ctx, logger, env, flags, cfg, resolved, h)
	if output != "" {
		if _, werr := io.WriteString(env.Stdout, redact.String(output)+"\n"); werr != nil {
			err = errors.Join(err, fmt.Errorf("write answer: %w", werr))
		}
	}
	if err != nil {
		return fail(logger, err)
	}
	return exitOK
}

func fail(logger *slog.Logger, err error) int {
	logger.Error("run failed", "error", err)
	return exitFailure
}

// runHarness wires and runs the agent and returns its answer. The agent's
// servers and the audit log are closed before it returns, and failing to
// close them fails the run.
func runHarness(ctx context.Context, logger *slog.Logger, env Env, flags runFlags, cfg *config.Config, resolved *config.Resolved, h *harness.Harness) (output string, runErr error) {
	if h.Model.Provider != "anthropic" {
		return "", fmt.Errorf("model provider %q is not supported; use anthropic", h.Model.Provider)
	}
	// The config and the harness have validated every field anthropic.New checks.
	m := must.Value(anthropic.New(anthropic.Config{
		APIKey:    resolved.AnthropicAPIKey,
		Model:     h.Model.Name,
		MaxTokens: cfg.Provider.Anthropic.MaxTokens,
		BaseURL:   cfg.Provider.Anthropic.BaseURL,
	}))

	servers := make(map[string]toolgateway.ToolServer, len(cfg.MCPServers))
	for name, srv := range cfg.MCPServers {
		servers[name] = env.Server(name, mcptool.Server{Command: srv.Command, Args: srv.Args, Env: resolved.MCPServerEnv[name]})
	}

	log, err := audit.Open(flags.audit)
	if err != nil {
		return "", err
	}
	defer func() {
		if cerr := log.Close(); cerr != nil {
			runErr = errors.Join(runErr, cerr)
		}
	}()

	a, err := agent.New(ctx, agent.Config{
		Harness:  h,
		Model:    m,
		Servers:  servers,
		Policy:   cfg.Policy,
		Approver: newTerminalApprover(env.Stdin, env.Stderr, env.Interactive, env.User),
		Audit:    log,
		Secrets:  resolved.Secrets,
	})
	if err != nil {
		return "", err
	}
	defer func() {
		if cerr := a.Close(); cerr != nil {
			runErr = errors.Join(runErr, cerr)
		}
	}()
	var names []string
	for _, def := range a.Tools() {
		names = append(names, def.Name)
	}
	logger.Debug("tools offered to the model", "tools", names)
	res, err := a.Run(ctx, flags.input)
	logger.Info("run finished", "harness", h.Name, "run_id", res.RunID, "steps", res.Steps, "audit", flags.audit)
	return res.Output, err
}
