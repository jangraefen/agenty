// Package cli is the agenty command line. "agenty serve" runs the server,
// which keeps harnesses, runs and the audit log in PostgreSQL and runs
// agents. "agenty apply" and "agenty run" are its clients: they store a
// harness and run one, answering approvals at the terminal. Clients sign in
// with the token in AGENTY_TOKEN and work in one workspace.
package cli

import (
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
	// Server returns the tool server for a configured MCP server: srv
	// itself outside of tests. Only agenty serve starts tool servers.
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

// clientEnv documents the environment of every client command.
const clientEnv = `
Environment:
  AGENTY_TOKEN      the bearer token to sign in with (required)
  AGENTY_WORKSPACE  the default of --workspace
`

// parseClientFlags parses a client command's flags and checks that exactly
// nargs arguments follow them. usage is the command's usage line and
// description.
func parseClientFlags(name, usage string, nargs int, args []string, env Env) (clientFlags, []string, int, bool) {
	var f clientFlags
	stderr := env.Stderr
	fs := flag.NewFlagSet("agenty "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		if _, err := io.WriteString(stderr, usage+clientEnv+"\nFlags:\n"); err == nil {
			fs.PrintDefaults()
		}
	}
	workspace, _ := env.LookupEnv("AGENTY_WORKSPACE")
	f.token, _ = env.LookupEnv(tokenVar)
	fs.StringVar(&f.server, "server", "http://127.0.0.1:8080", "`URL` of the agenty server")
	fs.StringVar(&f.workspace, "workspace", workspace, "the `workspace` to work in")
	fs.TextVar(&f.logLevel, "log-level", slog.LevelInfo, "log `level`: debug, info, warn or error")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return f, nil, exitOK, false
		}
		return f, nil, exitUsage, false
	}
	if fs.NArg() != nargs {
		// Report it the way the flag package reports a bad flag.
		if _, err := fmt.Fprintf(stderr, "agenty %s: expected %d arguments, got %d; quote an argument that has spaces\n", name, nargs, fs.NArg()); err != nil {
			return f, nil, exitFailure, false
		}
		fs.Usage()
		return f, nil, exitUsage, false
	}
	missing := ""
	switch {
	case f.workspace == "":
		missing = "--workspace or AGENTY_WORKSPACE is required"
	case f.token == "":
		missing = tokenVar + " is not set: sign in with a user's token"
	}
	if missing != "" {
		if _, err := fmt.Fprintf(stderr, "agenty %s: %s\n", name, missing); err != nil {
			return f, nil, exitFailure, false
		}
		fs.Usage()
		return f, nil, exitUsage, false
	}
	return f, fs.Args(), exitOK, true
}
