package cli

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"

	"github.com/jangraefen/agenty/internal/mcptool"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

// Exit codes. Usage errors get their own code so scripts can tell a mistyped
// command from one that ran and failed.
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

// Env is the process around a command: its streams, its environment, and
// what runs an MCP server. It is the seam between the real process, which
// cmd/agenty binds to it, and the commands: no command touches os.Stdin,
// os.Getenv or a subprocess directly, so tests run each command in-process
// with buffers, a map for an environment, and fake tool servers.
type Env struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// Interactive reports whether Stdin is a terminal. Without one, calls
	// that need approval are rejected.
	Interactive bool
	// LookupEnv reads the environment. Nil means an empty one.
	LookupEnv func(string) (string, bool)
	// Server, if set, returns the tool server for a configured MCP server,
	// which is srv itself without it; tests set it. Only agenty serve starts
	// tool servers.
	Server func(name string, srv mcptool.Server) toolgateway.ToolServer
}

// Main runs the command line args, without the program name, and returns the
// exit code: 0 on success, 1 when the command fails, 2 on a usage error.
// It fills in an empty environment when env has none and dispatches on the
// first argument; each command parses its own flags from the rest.
func Main(ctx context.Context, args []string, env Env) int {
	if env.LookupEnv == nil {
		env.LookupEnv = func(string) (string, bool) { return "", false }
	}
	if len(args) == 0 {
		return printUsage(env.Stderr, "", exitUsage)
	}
	switch args[0] {
	case "serve":
		return serve(ctx, args[1:], env)
	case "apply":
		return apply(ctx, args[1:], env)
	case "run":
		return run(ctx, args[1:], env)
	case "audit":
		return audit(ctx, args[1:], env)
	case "help", "-h", "--help":
		return printUsage(env.Stderr, "", exitOK)
	default:
		return printUsage(env.Stderr, fmt.Sprintf("unknown command %q\n", args[0]), exitUsage)
	}
}

// commands is the top-level usage text, printed by "agenty help" and after a
// usage error.
const commands = `usage: agenty <command> [flags]

Commands:
  serve   serves the HTTP API on localhost
  apply   stores a harness file on the server
  run     runs a harness on the server and prints the answer
  audit   exports the audit log, or verifies an export

Run "agenty <command> -h" for a command's flags.
`

// printUsage writes msg, if any, then the top-level usage to w, and returns
// code. If writing fails it returns exitFailure instead, as the error cannot
// be reported anywhere and code would claim the usage was shown.
func printUsage(w io.Writer, msg string, code int) int {
	if _, err := io.WriteString(w, msg+commands); err != nil {
		return exitFailure
	}
	return code
}

// clientFlags are the flags every client command has, and the token it signs
// in with. parseFlags fills them; newClient and clientLogger consume them.
type clientFlags struct {
	server, workspace string
	logLevel          slog.Level
	// token is the user's bearer token. It comes from the environment only,
	// never a flag, so it stays out of shell history and process lists.
	token string
}

// tokenVar is the environment variable holding the client's token.
const tokenVar = "AGENTY_TOKEN"

// command describes a command's flags and arguments. The client commands
// declare themselves with it, so they share one flag parser and report
// missing arguments, workspace and token the same way.
type command struct {
	// name is the command after "agenty", and usage its usage line and
	// description.
	name, usage string
	// nargs is how many arguments must follow the flags.
	nargs int
	// client gives the command the flags of a client of the server and
	// requires a token; workspace also requires a workspace.
	client, workspace bool
	// signer, if set, names whose token the command needs, such as "an
	// auditor's"; a user's otherwise.
	signer string
	// define, if set, adds the command's own flags.
	define func(*flag.FlagSet)
}

// The environment of client commands, for their usage. Variables do not
// show in the flag package's defaults, so they are listed by hand; the token
// above all must be documented, as no flag hints at it.
const (
	tokenEnv = `
Environment:
  AGENTY_TOKEN      the bearer token to sign in with (required)
`
	workspaceEnv = `  AGENTY_WORKSPACE  the default of --workspace
`
)

// parseFlags parses cmd's flags and checks that exactly cmd.nargs arguments
// follow them. ok is false, with the exit code, when the command is not to
// run.
//
// It builds a fresh FlagSet per call with ContinueOnError, so a bad flag
// returns a usage code rather than exiting the process, which tests rely on.
// Client commands get --server and --log-level and read their token from
// AGENTY_TOKEN; workspace commands also get --workspace, defaulting to
// AGENTY_WORKSPACE. The checks after parsing run in order of what the user
// most likely got wrong, and only the first is reported.
func parseFlags(cmd command, args []string, env Env) (f clientFlags, rest []string, code int, ok bool) {
	stderr := env.Stderr
	usage := cmd.usage
	if cmd.client {
		usage += tokenEnv
		if cmd.workspace {
			usage += workspaceEnv
		}
	}
	fs := flag.NewFlagSet("agenty "+cmd.name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		if _, err := io.WriteString(stderr, usage+"\nFlags:\n"); err == nil {
			fs.PrintDefaults()
		}
	}
	f.logLevel = slog.LevelInfo
	if cmd.client {
		// The token is read here, never declared as a flag: a flag's value
		// would land in shell history and in the process list.
		f.token, _ = env.LookupEnv(tokenVar)
		fs.StringVar(&f.server, "server", "http://127.0.0.1:8080", "`URL` of the agenty server")
		fs.TextVar(&f.logLevel, "log-level", slog.LevelInfo, "log `level`: debug, info, warn or error")
	}
	if cmd.workspace {
		workspace, _ := env.LookupEnv("AGENTY_WORKSPACE")
		fs.StringVar(&f.workspace, "workspace", workspace, "the `workspace` to work in")
	}
	if cmd.define != nil {
		cmd.define(fs)
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return f, nil, exitOK, false
		}
		return f, nil, exitUsage, false
	}
	// Missing pieces are reported after parsing, so that "-h" still shows
	// the usage when the token or workspace is not set.
	missing := ""
	switch {
	case fs.NArg() != cmd.nargs:
		arguments := "arguments"
		if cmd.nargs == 1 {
			arguments = "argument"
		}
		missing = fmt.Sprintf("expected %d %s, got %d; quote an argument that has spaces", cmd.nargs, arguments, fs.NArg())
	case cmd.workspace && f.workspace == "":
		missing = "--workspace or AGENTY_WORKSPACE is required"
	case cmd.client && f.token == "":
		missing = tokenVar + " is not set: sign in with " + cmp.Or(cmd.signer, "a user's") + " token"
	}
	if missing != "" {
		// Reported the way the flag package reports a bad flag.
		if _, err := fmt.Fprintf(stderr, "agenty %s: %s\n", cmd.name, missing); err != nil {
			return f, nil, exitFailure, false
		}
		fs.Usage()
		return f, nil, exitUsage, false
	}
	return f, fs.Args(), exitOK, true
}
