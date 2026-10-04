package main

import (
	"bytes"
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
	assert.Equal(t, "agenty-sandbox dev\n", stdout.String(), "stdout")
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

func TestVersionIsInjectedAtBuildTime(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := filepath.Join(t.TempDir(), "agenty-sandbox")
	build := exec.Command("go", "build",
		"-ldflags", "-X github.com/jangraefen/agenty/sandbox/internal/buildinfo.Version=1.2.3",
		"-o", bin, ".")
	out, err := build.CombinedOutput()
	require.NoError(t, err, "go build:\n%s", out)

	out, err = exec.Command(bin, "--version").Output()

	require.NoError(t, err, "run binary")
	assert.Equal(t, "agenty-sandbox 1.2.3\n", string(out), "output")
}
