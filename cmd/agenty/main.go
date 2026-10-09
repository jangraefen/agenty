// Command agenty runs governed AI agent harnesses. See "agenty help".
//
// This is the process entry point and nothing more: it binds the real
// process (its streams, its environment, whether stdin is a terminal, and an
// interrupt signal) to cli.Main, which holds every command. Keeping the
// commands in internal/cli behind the cli.Env seam lets tests drive the whole
// command line in-process with their own streams, environment and fake tool
// servers, without spawning the binary.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/jangraefen/agenty/internal/cli"
	"github.com/jangraefen/agenty/internal/dotenv"
)

// main exits with the code run returns. It is kept apart from run so that
// run's deferred calls, such as releasing the signal handler, take effect
// before os.Exit, which skips them.
func main() {
	os.Exit(run())
}

// run prepares the process environment and hands the arguments to cli.Main.
// An interrupt cancels the context every command runs in: "agenty serve"
// then stops its runs and shuts down, and "agenty run" cancels the run it
// started on the server rather than leaving it behind.
func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// A .env file in the working directory fills in variables the
	// environment does not set, for local development. It is loaded into the
	// process environment before anything reads it, so the operator config's
	// {env: NAME} values and AGENTY_TOKEN see it like any other variable;
	// variables already set win.
	if err := dotenv.Load("."); err != nil {
		return failed(err)
	}
	return cli.Main(ctx, os.Args[1:], cli.Env{
		Stdin:       os.Stdin,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		Interactive: isTerminal(os.Stdin),
		LookupEnv:   os.LookupEnv,
	})
}

// isTerminal reports whether f is a terminal. If it cannot tell, it says no,
// so nothing is approved without a person.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// failed reports an error that happens before cli.Main runs, when no
// logger exists yet, and returns the failure exit code. The only such error
// is a .env file that cannot be read or parsed.
func failed(err error) int {
	fmt.Fprintln(os.Stderr, "agenty:", err)
	return 1
}
