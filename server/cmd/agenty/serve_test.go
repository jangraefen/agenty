package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agenty.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestRunRejectsUnknownRole(t *testing.T) {
	var stdout, stderr bytes.Buffer
	path := writeConfig(t, "")

	code := run([]string{"--config", path, "--roles", "api,cron"}, &stdout, &stderr)

	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), `"cron"`) {
		t.Errorf("stderr = %q, want it to name the unknown role", stderr.String())
	}
}

func TestRunRequiresConfigFile(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"--roles", "api"}, &stdout, &stderr)

	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "--config") {
		t.Errorf("stderr = %q, want it to mention --config", stderr.String())
	}
}

func TestRunAbortsOnInvalidConfigurationNamingTheField(t *testing.T) {
	var stdout, stderr bytes.Buffer
	path := writeConfig(t, "server:\n  shutdownTimeout: soon\n")

	code := run([]string{"--config", path}, &stdout, &stderr)

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "server.shutdownTimeout") {
		t.Errorf("stderr = %q, want it to name server.shutdownTimeout", stderr.String())
	}
}

func TestRunAbortsOnInvalidEnvironmentOverride(t *testing.T) {
	var stdout, stderr bytes.Buffer
	path := writeConfig(t, "")
	t.Setenv("AGENTY_SERVER_ADDRESS", "nonsense")

	code := run([]string{"--config", path}, &stdout, &stderr)

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "AGENTY_SERVER_ADDRESS") {
		t.Errorf("stderr = %q, want it to name AGENTY_SERVER_ADDRESS", stderr.String())
	}
}
