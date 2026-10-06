// Command agenty runs governed AI agent harnesses. See "agenty help".
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/jangraefen/agenty/internal/cli"
	"github.com/jangraefen/agenty/internal/dotenv"
	"github.com/jangraefen/agenty/internal/mcptool"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// A .env file in the working directory fills in variables the
	// environment does not set, for local development.
	if err := dotenv.Load("."); err != nil {
		return failed(err)
	}
	return cli.Main(ctx, os.Args[1:], cli.Env{
		Stdin:       os.Stdin,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		Interactive: isTerminal(os.Stdin),
		LookupEnv:   os.LookupEnv,
		Server:      func(_ string, srv mcptool.Server) toolgateway.ToolServer { return srv },
	})
}

// isTerminal reports whether f is a terminal. If it cannot tell, it says no,
// so nothing is approved without a person.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func failed(err error) int {
	fmt.Fprintln(os.Stderr, "agenty:", err)
	return 1
}
