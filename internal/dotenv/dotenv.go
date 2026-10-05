// Package dotenv loads a .env file into the process environment, for local
// development. Variables that are already set win, so the real environment
// always overrides the file. The file usually holds credentials: never log
// what it loads.
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
// error.
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
