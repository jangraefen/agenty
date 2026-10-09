// Package dotenv loads a .env file into the process environment, for local
// development. Variables that are already set win, so the real environment
// always overrides the file. The file usually holds credentials: never log
// what it loads.
//
// `agenty` (cmd/agenty) calls Load for the working directory at start, so
// `task serve` and the CLI find credentials such as ANTHROPIC_API_KEY or
// AGENTY_TOKEN without exporting them. Tests that need one, such as the
// optional real-model smoke test, call LoadModuleRoot. Values loaded become
// ordinary environment variables; the operator config reads them as
// {env: NAME}, which treats them as secrets and redacts them everywhere
// (trust-model guarantee 5). The file itself is git-ignored.
package dotenv

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

// Load loads the .env file in dir, if there is one. A missing file is not an
// error, as a deployment sets its environment directly; any other failure
// to read it is, so a broken file is never mistaken for an absent one.
// godotenv.Load leaves variables that are already set alone.
func Load(dir string) error {
	path := filepath.Join(dir, ".env")
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("dotenv: %w", err)
	}
	if err := godotenv.Load(path); err != nil {
		return fmt.Errorf("dotenv: %s: %w", path, err)
	}
	return nil
}

// LoadModuleRoot loads the .env file at the root of the Go module that
// contains the working directory. Tests use it, because they run in their
// package's directory rather than at the root.
func LoadModuleRoot() error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("dotenv: %w", err)
	}
	// Walk up from the working directory to the first directory with a
	// go.mod; reaching the file system's root without one is an error.
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return Load(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return errors.New("dotenv: no go.mod above the working directory")
		}
		dir = parent
	}
}
