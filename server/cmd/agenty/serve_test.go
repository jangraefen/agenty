package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agenty.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600), "write config")
	return path
}

func TestRunRejectsUnknownRole(t *testing.T) {
	var stdout, stderr bytes.Buffer
	path := writeConfig(t, "")

	code := run([]string{"--config", path, "--roles", "api,cron"}, &stdout, &stderr)

	assert.Equal(t, 2, code, "exit code")
	assert.Contains(t, stderr.String(), `"cron"`, "stderr must name the unknown role")
}

func TestRunRequiresConfigFile(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"--roles", "api"}, &stdout, &stderr)

	assert.Equal(t, 2, code, "exit code")
	assert.Contains(t, stderr.String(), "--config", "stderr must mention --config")
}

func TestRunAbortsOnInvalidConfigurationNamingTheField(t *testing.T) {
	var stdout, stderr bytes.Buffer
	path := writeConfig(t, "server:\n  shutdownTimeout: soon\n")

	code := run([]string{"--config", path}, &stdout, &stderr)

	assert.Equal(t, 1, code, "exit code")
	assert.Contains(t, stderr.String(), "server.shutdownTimeout", "stderr must name server.shutdownTimeout")
}

func TestRunAbortsOnInvalidLogFormat(t *testing.T) {
	var stdout, stderr bytes.Buffer
	path := writeConfig(t, "log:\n  format: xml\n")

	code := run([]string{"--config", path}, &stdout, &stderr)

	assert.Equal(t, 1, code, "exit code")
	assert.Contains(t, stderr.String(), "log.format", "stderr must name log.format")
}

func TestRunAbortsOnInvalidEnvironmentOverride(t *testing.T) {
	var stdout, stderr bytes.Buffer
	path := writeConfig(t, "")
	t.Setenv("AGENTY_SERVER_ADDRESS", "nonsense")

	code := run([]string{"--config", path}, &stdout, &stderr)

	assert.Equal(t, 1, code, "exit code")
	assert.Contains(t, stderr.String(), "AGENTY_SERVER_ADDRESS", "stderr must name AGENTY_SERVER_ADDRESS")
}
