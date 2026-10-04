package config_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/server/internal/config"
	"github.com/jangraefen/agenty/server/internal/logging"
)

// dbSection is a valid database section; the database URL is required.
const dbSection = "database:\n  url: postgres://agenty:agenty@localhost:5432/agenty?sslmode=disable\n" //nolint:gosec // Test credential.

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agenty.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600), "write config")
	return path
}

func env(vars map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := vars[key]
		return v, ok
	}
}

func TestLoadReadsFile(t *testing.T) {
	path := writeFile(t, "server:\n  address: 127.0.0.1:9000\n  shutdownTimeout: 5s\n"+dbSection)

	cfg, err := config.Load(path, env(nil))

	require.NoError(t, err, "Load")
	assert.Equal(t, "127.0.0.1:9000", cfg.Server.Address, "Server.Address")
	assert.Equal(t, 5*time.Second, cfg.Server.ShutdownTimeout, "Server.ShutdownTimeout")
	assert.Equal(t, "postgres://agenty:agenty@localhost:5432/agenty?sslmode=disable", cfg.Database.URL, "Database.URL")
}

func TestLoadAppliesDefaults(t *testing.T) {
	path := writeFile(t, dbSection)

	cfg, err := config.Load(path, env(nil))

	require.NoError(t, err, "Load")
	assert.Equal(t, ":8080", cfg.Server.Address, "Server.Address")
	assert.Equal(t, 30*time.Second, cfg.Server.ShutdownTimeout, "Server.ShutdownTimeout")
}

func TestLoadEnvironmentOverridesFile(t *testing.T) {
	path := writeFile(t, "server:\n  address: 127.0.0.1:9000\n  shutdownTimeout: 5s\n"+dbSection)

	cfg, err := config.Load(path, env(map[string]string{
		"AGENTY_SERVER_ADDRESS":          "0.0.0.0:9100",
		"AGENTY_SERVER_SHUTDOWN_TIMEOUT": "1m",
		"AGENTY_DATABASE_URL":            "host=db user=agenty dbname=agenty",
	}))

	require.NoError(t, err, "Load")
	assert.Equal(t, "0.0.0.0:9100", cfg.Server.Address, "Server.Address")
	assert.Equal(t, time.Minute, cfg.Server.ShutdownTimeout, "Server.ShutdownTimeout")
	assert.Equal(t, "host=db user=agenty dbname=agenty", cfg.Database.URL, "Database.URL")
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name      string
		file      string
		env       map[string]string
		wantInErr []string
		notInErr  []string
	}{
		{
			name:      "unknown field",
			file:      "server:\n  adress: :8080\n" + dbSection,
			wantInErr: []string{"adress"},
		},
		{
			name:      "malformed yaml",
			file:      "server: [\n",
			wantInErr: []string{"agenty.yaml"},
		},
		{
			name:      "wrong type",
			file:      "server:\n  address: [1, 2]\n" + dbSection,
			wantInErr: []string{"address"},
		},
		{
			name:      "invalid duration in file",
			file:      "server:\n  shutdownTimeout: soon\n" + dbSection,
			wantInErr: []string{"server.shutdownTimeout", "soon"},
		},
		{
			name:      "non-positive duration",
			file:      "server:\n  shutdownTimeout: 0s\n" + dbSection,
			wantInErr: []string{"server.shutdownTimeout", "positive"},
		},
		{
			name:      "address without port",
			file:      "server:\n  address: localhost\n" + dbSection,
			wantInErr: []string{"server.address"},
		},
		{
			name:      "address with invalid port",
			file:      "server:\n  address: localhost:http-ish\n" + dbSection,
			wantInErr: []string{"server.address"},
		},
		{
			name:      "address with out-of-range port",
			file:      "server:\n  address: localhost:70000\n" + dbSection,
			wantInErr: []string{"server.address"},
		},
		{
			name:      "invalid duration from environment",
			file:      dbSection,
			env:       map[string]string{"AGENTY_SERVER_SHUTDOWN_TIMEOUT": "later"},
			wantInErr: []string{"server.shutdownTimeout", "AGENTY_SERVER_SHUTDOWN_TIMEOUT", "later"},
		},
		{
			name:      "empty address from environment",
			file:      dbSection,
			env:       map[string]string{"AGENTY_SERVER_ADDRESS": ""},
			wantInErr: []string{"server.address", "AGENTY_SERVER_ADDRESS"},
		},
		{
			name:      "missing database url",
			file:      "",
			wantInErr: []string{"database.url", "required", "AGENTY_DATABASE_URL"},
		},
		{
			name:      "empty database url from environment",
			file:      dbSection,
			env:       map[string]string{"AGENTY_DATABASE_URL": ""},
			wantInErr: []string{"database.url", "AGENTY_DATABASE_URL", "required"},
		},
		{
			name:      "invalid database url keeps the password out of the error",
			file:      "database:\n  url: postgres://agenty:hunter2-file@localhost:notaport/agenty\n",
			wantInErr: []string{"database.url"},
			notInErr:  []string{"hunter2-file"},
		},
		{
			name:      "invalid database url from environment",
			file:      dbSection,
			env:       map[string]string{"AGENTY_DATABASE_URL": "postgres://agenty:hunter2-env@localhost:5432/agenty?sslmode=bogus"}, //nolint:gosec // Test credential.
			wantInErr: []string{"database.url", "AGENTY_DATABASE_URL"},
			notInErr:  []string{"hunter2-env"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeFile(t, tt.file)

			_, err := config.Load(path, env(tt.env))

			require.Error(t, err, "Load succeeded, want error")
			for _, want := range tt.wantInErr {
				assert.ErrorContains(t, err, want)
			}
			for _, notWant := range tt.notInErr {
				assert.NotContains(t, err.Error(), notWant, "error leaks a secret")
			}
		})
	}
}

func TestLoadReadsLogSettings(t *testing.T) {
	path := writeFile(t, "log:\n  format: json\n  level: debug\n"+dbSection)

	cfg, err := config.Load(path, env(nil))

	require.NoError(t, err, "Load")
	assert.Equal(t, logging.FormatJSON, cfg.Log.Format, "Log.Format")
	assert.Equal(t, slog.LevelDebug, cfg.Log.Level, "Log.Level")
}

func TestLoadDefaultsToTextLogAtInfo(t *testing.T) {
	cfg, err := config.Load(writeFile(t, dbSection), env(nil))

	require.NoError(t, err, "Load")
	assert.Equal(t, logging.FormatText, cfg.Log.Format, "Log.Format")
	assert.Equal(t, slog.LevelInfo, cfg.Log.Level, "Log.Level")
}

func TestLoadLogSettingsFromEnvironment(t *testing.T) {
	path := writeFile(t, "log:\n  format: json\n  level: debug\n"+dbSection)

	cfg, err := config.Load(path, env(map[string]string{"AGENTY_LOG_FORMAT": "logfmt", "AGENTY_LOG_LEVEL": "WARN"}))

	require.NoError(t, err, "Load")
	assert.Equal(t, logging.FormatLogfmt, cfg.Log.Format, "Log.Format")
	assert.Equal(t, slog.LevelWarn, cfg.Log.Level, "Log.Level")
}

func TestLoadRejectsInvalidLogSettings(t *testing.T) {
	tests := []struct {
		name      string
		file      string
		env       map[string]string
		wantInErr []string
	}{
		{"unknown format", "log:\n  format: xml\n", nil, []string{"log.format", "xml"}},
		{"unknown level", "log:\n  level: loud\n", nil, []string{"log.level", "loud"}},
		{"level with offset", "log:\n  level: info+2\n", nil, []string{"log.level", "info+2"}},
		{"level with negative offset", "", map[string]string{"AGENTY_LOG_LEVEL": "WARN-1"}, []string{"log.level", "AGENTY_LOG_LEVEL", "WARN-1"}},
		{"format from environment", "", map[string]string{"AGENTY_LOG_FORMAT": "yaml"}, []string{"log.format", "AGENTY_LOG_FORMAT", "yaml"}},
		{"level from environment", "", map[string]string{"AGENTY_LOG_LEVEL": ""}, []string{"log.level", "AGENTY_LOG_LEVEL"}},
		{"unknown log field", "log:\n  colour: true\n", nil, []string{"colour"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.Load(writeFile(t, tt.file+dbSection), env(tt.env))

			require.Error(t, err, "Load succeeded, want error")
			for _, want := range tt.wantInErr {
				assert.ErrorContains(t, err, want)
			}
		})
	}
}

func TestLoadReportsMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.yaml")

	_, err := config.Load(path, env(nil))

	assert.ErrorContains(t, err, "missing.yaml", "error must name the file")
}

func TestDatabaseLogValueOmitsPassword(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"url with password in user info", "postgres://agenty:hunter2@db.example:5433/agentydb?sslmode=disable"},
		{"url with password in query", "postgres://agenty@db.example:5433/agentydb?password=hunter2"},
		{"keyword/value string", "host=db.example port=5433 user=agenty password=hunter2 dbname=agentydb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&buf, nil))

			logger.Info("connect", "database", config.Database{URL: tt.url})

			out := buf.String()
			assert.NotContains(t, out, "hunter2", "log output leaks the password")
			assert.Contains(t, out, "database.host=db.example", "host")
			assert.Contains(t, out, "database.port=5433", "port")
			assert.Contains(t, out, "database.name=agentydb", "database name")
			assert.Contains(t, out, "database.user=agenty", "user")
		})
	}
}

func TestDatabaseLogValueOfInvalidURLIsRedacted(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	logger.Info("connect", "database", config.Database{URL: "postgres://agenty:hunter2@localhost:notaport/x"})

	assert.NotContains(t, buf.String(), "hunter2", "log output leaks the password")
	assert.Contains(t, buf.String(), "database=[redacted]", "invalid URL")
}

// TestPrintedConfigurationOmitsPassword covers every way a Config or Database
// may end up in logs or errors: fmt verbs, slog handlers, and JSON
// (ARCHITECTURE §3 invariant 6).
func TestPrintedConfigurationOmitsPassword(t *testing.T) {
	urls := map[string]string{ //nolint:gosec // Test credentials.
		"url with password in user info": "postgres://agenty:hunter2@db.example:5433/agentydb?sslmode=disable",
		"url with password in query":     "postgres://agenty@db.example:5433/agentydb?password=hunter2",
		"keyword/value string":           "host=db.example port=5433 user=agenty password=hunter2 dbname=agentydb",
		"invalid url":                    "postgres://agenty:hunter2@db.example:notaport/agentydb",
	}
	for name, url := range urls {
		db := config.Database{URL: url}
		cfg := config.Config{Server: config.Server{Address: ":8080"}, Database: db}
		values := map[string]any{"Database": db, "*Database": &db, "Config": cfg, "*Config": &cfg}
		for kind, v := range values {
			t.Run(name+"/"+kind, func(t *testing.T) {
				outputs := map[string]string{}
				for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
					outputs[verb] = fmt.Sprintf(verb, v)
				}
				var text, js bytes.Buffer
				slog.New(slog.NewTextHandler(&text, nil)).Info("start", "value", v)
				slog.New(slog.NewJSONHandler(&js, nil)).Info("start", "value", v)
				outputs["slog text"] = text.String()
				outputs["slog json"] = js.String()
				encoded, err := json.Marshal(v)
				require.NoError(t, err, "json.Marshal")
				outputs["json"] = string(encoded)

				for form, out := range outputs {
					assert.NotContains(t, out, "hunter2", "%s leaks the password: %s", form, out)
					if name != "invalid url" {
						assert.Contains(t, out, "db.example", "%s omits the host: %s", form, out)
					}
				}
			})
		}
	}
}

func TestDatabasePrintsTheTargetWithoutPassword(t *testing.T) {
	db := config.Database{URL: "postgres://agenty:hunter2@db.example:5433/agentydb?sslmode=disable"} //nolint:gosec // Test credential.

	assert.Equal(t, "postgres://agenty@db.example:5433/agentydb", db.String(), "String")
	assert.Equal(t, "postgres://agenty@db.example:5433/agentydb", fmt.Sprintf("%v", db), "%v")
	assert.Equal(t, `"postgres://agenty@db.example:5433/agentydb"`, fmt.Sprintf("%q", db), "%q")
	assert.Equal(t, `config.Database{URL:"postgres://agenty@db.example:5433/agentydb"}`, fmt.Sprintf("%#v", db), "%#v")
	assert.Equal(t, "{Server:{Address: ShutdownTimeout:0s} Log:{Format: Level:INFO} Database:postgres://agenty@db.example:5433/agentydb}",
		fmt.Sprintf("%+v", config.Config{Database: db}), "%+v of Config")
	assert.Equal(t, "[redacted]", config.Database{URL: "postgres://agenty:hunter2@db.example:notaport/x"}.String(), "invalid URL")
}

func TestDatabaseSecretsContainThePasswordAndItsEscapedForms(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want []string
	}{
		{"URL with escaped password", "postgres://agenty:s3cr3t%2Fp%40ss@db:5432/agenty", []string{"s3cr3t/p@ss", "s3cr3t%2Fp%40ss"}},
		{"keyword/value string", "host=db user=agenty password=hunter2 dbname=agenty", []string{"hunter2"}},
		{"no password", "postgres://agenty@db:5432/agenty", nil},
		{"invalid URL", "postgres://agenty:hunter2@db:notaport/agenty", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := config.Database{URL: tt.url}.Secrets()

			for _, w := range tt.want {
				assert.Contains(t, got, w)
			}
			if tt.want == nil {
				assert.Empty(t, got)
			}
		})
	}
}
