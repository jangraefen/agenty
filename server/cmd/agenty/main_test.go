package main

import (
	"bytes"
	"log/slog"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunPrintsVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"--version"}, &stdout, &stderr)

	require.Equal(t, 0, code, "exit code (stderr: %q)", stderr.String())
	assert.Equal(t, "agenty dev\n", stdout.String(), "stdout")
}

func TestRunRejectsUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"--no-such-flag"}, &stdout, &stderr)

	assert.Equal(t, 2, code, "exit code")
	assert.NotZero(t, stderr.Len(), "stderr is empty, want an error message")
}

func TestRunWithoutArgumentsPrintsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run(nil, &stdout, &stderr)

	assert.Equal(t, 2, code, "exit code")
	assert.Contains(t, stderr.String(), "Usage", "stderr must contain usage text")
}

func TestRunAbortsWhenTheDatabaseIsUnreachableWithoutLeakingThePassword(t *testing.T) {
	var stdout, stderr, logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	path := writeConfig(t, "server:\n  address: 127.0.0.1:0\ndatabase:\n"+
		"  url: postgres://agenty:hunter2@127.0.0.1:1/agenty?sslmode=disable&connect_timeout=2\n")

	code := runContext(t.Context(), []string{"--config", path}, &stdout, &stderr, nil)

	assert.Equal(t, 1, code, "exit code")
	assert.Contains(t, stderr.String(), "database", "stderr must name the database")
	assert.NotContains(t, stderr.String(), "hunter2", "stderr leaks the password")
	assert.NotContains(t, logs.String(), "hunter2", "logs leak the password")
}

func TestVersionIsInjectedAtBuildTime(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := filepath.Join(t.TempDir(), "agenty")
	build := exec.Command("go", "build",
		"-ldflags", "-X github.com/jangraefen/agenty/server/internal/buildinfo.Version=1.2.3",
		"-o", bin, ".")
	out, err := build.CombinedOutput()
	require.NoError(t, err, "go build:\n%s", out)

	out, err = exec.Command(bin, "--version").Output()

	require.NoError(t, err, "run binary")
	assert.Equal(t, "agenty 1.2.3\n", string(out), "output")
}
