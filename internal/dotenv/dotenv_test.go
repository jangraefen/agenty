package dotenv_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/dotenv"
)

// These tests only use .env files they write to temporary directories.

// writeEnv writes a .env file with content to dir, readable by its owner
// only, as a file of credentials should be.
func writeEnv(t *testing.T, dir, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0o600))
}

// unsetLater removes variables that dotenv set, which t.Setenv cannot track.
func unsetLater(t *testing.T, keys ...string) {
	t.Cleanup(func() {
		for _, k := range keys {
			assert.NoError(t, os.Unsetenv(k))
		}
	})
}

func TestLoad_SetsVariablesButTheEnvironmentWins(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, "AGENTY_DOTENV_A=from-file\nAGENTY_DOTENV_B=from-file\n")
	t.Setenv("AGENTY_DOTENV_B", "from-environment")
	unsetLater(t, "AGENTY_DOTENV_A")

	require.NoError(t, dotenv.Load(dir))

	assert.Equal(t, "from-file", os.Getenv("AGENTY_DOTENV_A"))
	assert.Equal(t, "from-environment", os.Getenv("AGENTY_DOTENV_B"))
}

func TestLoad_MissingFileIsFine(t *testing.T) {
	require.NoError(t, dotenv.Load(t.TempDir()))
}

func TestLoad_Errors(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T) string
		wantErr string
	}{
		{"malformed file", func(t *testing.T) string {
			dir := t.TempDir()
			writeEnv(t, dir, "AGENTY_DOTENV_C=\"unterminated\n")
			return dir
		}, ".env"},
		{"unreadable directory", func(t *testing.T) string {
			dir := t.TempDir()
			require.NoError(t, os.Chmod(dir, 0o000))
			t.Cleanup(func() { assert.NoError(t, os.Chmod(dir, 0o700)) }) //nolint:gosec // G302: a directory needs its x bit back so the test can remove it
			return dir
		}, "permission denied"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := dotenv.Load(tt.setup(t))
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestLoadModuleRoot_FindsTheModuleFromASubdirectory(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/m\n"), 0o600))
	writeEnv(t, root, "AGENTY_DOTENV_D=from-module-root\n")
	sub := filepath.Join(root, "internal", "pkg")
	require.NoError(t, os.MkdirAll(sub, 0o700))
	t.Chdir(sub)
	unsetLater(t, "AGENTY_DOTENV_D")

	require.NoError(t, dotenv.LoadModuleRoot())

	assert.Equal(t, "from-module-root", os.Getenv("AGENTY_DOTENV_D"))
}

func TestLoadModuleRoot_OutsideAModuleIsAnError(t *testing.T) {
	t.Chdir(t.TempDir())

	require.ErrorContains(t, dotenv.LoadModuleRoot(), "no go.mod")
}
