// Package cli is the agenty command line. "agenty serve" runs the server,
// which keeps harnesses, runs and the audit log in PostgreSQL and runs
// agents. "agenty apply" and "agenty run" are its clients: they store a
// harness and run one, answering approvals at the terminal.
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
	// User names the person at the terminal; approvals record it.
	User      string
	LookupEnv func(string) (string, bool)
	// Server returns the tool server for a configured MCP server: srv
	// itself outside of tests. Only agenty serve starts tool servers.
	Server func(name string, srv mcptool.Server) toolgateway.ToolServer
}

// Main runs the command line args, without the program name, and returns the
// exit code: 0 on success, 1 when the command fails, 2 on a usage error.
func Main(ctx context.Context, args []string, env Env) int {
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

// clientFlags are the flags every client command has.
type clientFlags struct {
	server   string
	logLevel slog.Level
}

// parseClientFlags parses a client command's flags and checks that exactly
// nargs arguments follow them. usage is the command's usage line and
// description.
func parseClientFlags(name, usage string, nargs int, args []string, stderr io.Writer) (clientFlags, []string, int, bool) {
	var f clientFlags
	fs := flag.NewFlagSet("agenty "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		if _, err := io.WriteString(stderr, usage+"\nFlags:\n"); err == nil {
			fs.PrintDefaults()
		}
	}
	fs.StringVar(&f.server, "server", "http://127.0.0.1:8080", "`URL` of the agenty server")
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
	return f, fs.Args(), exitOK, true
}
