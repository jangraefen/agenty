// Package cli is the agenty command line. "agenty serve" runs the server,
// which keeps harnesses, runs and the audit log in PostgreSQL and runs
// agents. "agenty apply", "agenty run" and "agenty audit export" are its
// clients: they store a harness, run one, answering approvals at the
// terminal, and export the audit log, which "agenty audit verify" checks
// offline. Clients sign in with the token in AGENTY_TOKEN; apply and run
// work in one workspace.
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

// Exit codes.
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

// Env is the process around a command: its streams, its environment, and
// what runs an MCP server.
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

const commands = `usage: agenty <command> [flags]

Commands:
  serve   serves the HTTP API on localhost
  apply   stores a harness file on the server
  run     runs a harness on the server and prints the answer
  audit   exports the audit log, or verifies an export

Run "agenty <command> -h" for a command's flags.
`

func printUsage(w io.Writer, msg string, code int) int {
	if _, err := io.WriteString(w, msg+commands); err != nil {
		return exitFailure
	}
	return code
}

// clientFlags are the flags every client command has, and the token it signs
// in with.
type clientFlags struct {
	server, workspace string
	logLevel          slog.Level
	// token is the user's bearer token. It comes from the environment only,
	// never a flag, so it stays out of shell history and process lists.
	token string
}

// tokenVar is the environment variable holding the client's token.
const tokenVar = "AGENTY_TOKEN"

// command describes a command's flags and arguments.
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

// The environment of client commands, for their usage.
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
