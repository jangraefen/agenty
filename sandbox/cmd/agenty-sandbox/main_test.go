package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRunPrintsVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"--version"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	if got, want := stdout.String(), "agenty-sandbox dev\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

func TestRunRejectsUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"--no-such-flag"}, &stdout, &stderr)

	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if stderr.Len() == 0 {
		t.Error("stderr is empty, want an error message")
	}
}

func TestRunWithoutArgumentsPrintsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run(nil, &stdout, &stderr)

	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !bytes.Contains(stderr.Bytes(), []byte("Usage")) {
		t.Errorf("stderr = %q, want usage text", stderr.String())
	}
}

func TestVersionIsInjectedAtBuildTime(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := filepath.Join(t.TempDir(), "agenty-sandbox")
	build := exec.Command("go", "build",
		"-ldflags", "-X github.com/jangraefen/agenty/sandbox/internal/buildinfo.Version=1.2.3",
		"-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	out, err := exec.Command(bin, "--version").Output()

	if err != nil {
		t.Fatalf("run binary: %v", err)
	}
	if got, want := string(out), "agenty-sandbox 1.2.3\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}
