package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jangraefen/agenty/server/internal/config"
)

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agenty.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func env(vars map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := vars[key]
		return v, ok
	}
}

func TestLoadReadsFile(t *testing.T) {
	path := writeFile(t, "server:\n  address: 127.0.0.1:9000\n  shutdownTimeout: 5s\n")

	cfg, err := config.Load(path, env(nil))

	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Address != "127.0.0.1:9000" {
		t.Errorf("Server.Address = %q, want %q", cfg.Server.Address, "127.0.0.1:9000")
	}
	if cfg.Server.ShutdownTimeout != 5*time.Second {
		t.Errorf("Server.ShutdownTimeout = %v, want 5s", cfg.Server.ShutdownTimeout)
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	path := writeFile(t, "")

	cfg, err := config.Load(path, env(nil))

	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Address != ":8080" {
		t.Errorf("Server.Address = %q, want %q", cfg.Server.Address, ":8080")
	}
	if cfg.Server.ShutdownTimeout != 30*time.Second {
		t.Errorf("Server.ShutdownTimeout = %v, want 30s", cfg.Server.ShutdownTimeout)
	}
}

func TestLoadEnvironmentOverridesFile(t *testing.T) {
	path := writeFile(t, "server:\n  address: 127.0.0.1:9000\n  shutdownTimeout: 5s\n")

	cfg, err := config.Load(path, env(map[string]string{
		"AGENTY_SERVER_ADDRESS":          "0.0.0.0:9100",
		"AGENTY_SERVER_SHUTDOWN_TIMEOUT": "1m",
	}))

	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Address != "0.0.0.0:9100" {
		t.Errorf("Server.Address = %q, want %q", cfg.Server.Address, "0.0.0.0:9100")
	}
	if cfg.Server.ShutdownTimeout != time.Minute {
		t.Errorf("Server.ShutdownTimeout = %v, want 1m", cfg.Server.ShutdownTimeout)
	}
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name      string
		file      string
		env       map[string]string
		wantInErr []string
	}{
		{
			name:      "unknown field",
			file:      "server:\n  adress: :8080\n",
			wantInErr: []string{"adress"},
		},
		{
			name:      "malformed yaml",
			file:      "server: [\n",
			wantInErr: []string{"agenty.yaml"},
		},
		{
			name:      "wrong type",
			file:      "server:\n  address: [1, 2]\n",
			wantInErr: []string{"address"},
		},
		{
			name:      "invalid duration in file",
			file:      "server:\n  shutdownTimeout: soon\n",
			wantInErr: []string{"server.shutdownTimeout", "soon"},
		},
		{
			name:      "non-positive duration",
			file:      "server:\n  shutdownTimeout: 0s\n",
			wantInErr: []string{"server.shutdownTimeout", "positive"},
		},
		{
			name:      "address without port",
			file:      "server:\n  address: localhost\n",
			wantInErr: []string{"server.address"},
		},
		{
			name:      "address with invalid port",
			file:      "server:\n  address: localhost:http-ish\n",
			wantInErr: []string{"server.address"},
		},
		{
			name:      "address with out-of-range port",
			file:      "server:\n  address: localhost:70000\n",
			wantInErr: []string{"server.address"},
		},
		{
			name:      "invalid duration from environment",
			env:       map[string]string{"AGENTY_SERVER_SHUTDOWN_TIMEOUT": "later"},
			wantInErr: []string{"server.shutdownTimeout", "AGENTY_SERVER_SHUTDOWN_TIMEOUT", "later"},
		},
		{
			name:      "empty address from environment",
			env:       map[string]string{"AGENTY_SERVER_ADDRESS": ""},
			wantInErr: []string{"server.address", "AGENTY_SERVER_ADDRESS"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeFile(t, tt.file)

			_, err := config.Load(path, env(tt.env))

			if err == nil {
				t.Fatal("Load succeeded, want error")
			}
			for _, want := range tt.wantInErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

func TestLoadReportsMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.yaml")

	_, err := config.Load(path, env(nil))

	if err == nil || !strings.Contains(err.Error(), "missing.yaml") {
		t.Errorf("error = %v, want one naming the file", err)
	}
}
