// Command agenty-sandbox is the entry point of the sandbox runner binary.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/jangraefen/agenty/sandbox/internal/buildinfo"
)

const name = "agenty-sandbox"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes the command with the given arguments and returns the exit code.
func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage: %s [flags]\n\nFlags:\n", name)
		flags.PrintDefaults()
	}
	showVersion := flags.Bool("version", false, "print the version and exit")

	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		_, _ = fmt.Fprintf(stdout, "%s %s\n", name, buildinfo.Version)
		return 0
	}

	flags.Usage()
	return 2
}
