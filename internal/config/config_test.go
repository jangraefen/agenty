package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/config"
)

func TestLoad_Valid(t *testing.T) {
	got, err := config.Load(filepath.Join("testdata", "full.yaml"))
	require.NoError(t, err)

	assert.Equal(t, &config.Anthropic{
		APIKey:    config.Value{Env: "ANTHROPIC_API_KEY"},
		MaxTokens: 4096,
		BaseURL:   "http://localhost:8080",
	}, got.Provider.Anthropic)
	assert.Equal(t, map[string]config.MCPServer{
		"tickets": {
			Command: "tickets-mcp",
			Args:    []string{"--stdio"},
			Env: map[string]config.Value{
				"TICKETS_TOKEN": {Env: "TICKETS_TOKEN"},
				"LOG_LEVEL":     {Value: "info"},
			},
		},
	}, got.MCPServers)
	require.Len(t, got.Policy, 1)
	assert.Equal(t, "policies/central.rego", got.Policy[0].Name)
	assert.Contains(t, got.Policy[0].Source, "writes need a human", "policy files are read relative to the config file")

	minimal, err := config.Load(filepath.Join("testdata", "minimal.yaml"))
	require.NoError(t, err)
	assert.Empty(t, minimal.MCPServers)
	assert.Empty(t, minimal.Policy)
}

// fieldsOf returns the fields named by every config.FieldError in err.
func fieldsOf(err error) []string {
	var fields []string
	var walk func(error)
	walk = func(e error) {
		if fe, ok := e.(*config.FieldError); ok { //nolint:errorlint // the walk needs the exact node; errors.As would match wrappers too
			fields = append(fields, fe.Field)
			return
		}
		if j, ok := e.(interface{ Unwrap() []error }); ok {
			for _, inner := range j.Unwrap() {
				walk(inner)
			}
			return
		}
		if inner := errors.Unwrap(e); inner != nil {
			walk(inner)
		}
	}
	walk(err)
	return fields
}

func TestLoad_InvalidFields(t *testing.T) {
	tests := []struct {
		name       string
		yaml       string
		wantFields []string
	}{
		{"no provider", "{}", []string{"provider.anthropic"}},
		{"no max tokens", "provider: {anthropic: {api_key: {env: K}}}", []string{"provider.anthropic.max_tokens"}},
		{"api key with both env and value", "provider: {anthropic: {api_key: {env: K, value: v}, max_tokens: 1}}", []string{"provider.anthropic.api_key"}},
		{"api key with neither", "provider: {anthropic: {api_key: {}, max_tokens: 1}}", []string{"provider.anthropic.api_key"}},
		{"mcp server without command", "provider: {anthropic: {api_key: {env: K}, max_tokens: 1}}\nmcp_servers: {tickets: {}}", []string{"mcp_servers.tickets.command"}},
		{"mcp server env value with neither", "provider: {anthropic: {api_key: {env: K}, max_tokens: 1}}\nmcp_servers: {tickets: {command: x, env: {TOKEN: {}}}}", []string{"mcp_servers.tickets.env.TOKEN"}},
		{"empty policy file name", "provider: {anthropic: {api_key: {env: K}, max_tokens: 1}}\npolicy: {files: [\"\"]}", []string{"policy.files[0]"}},
		{"missing policy file", "provider: {anthropic: {api_key: {env: K}, max_tokens: 1}}\npolicy: {files: [nope.rego]}", []string{"policy.files[0]"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "agenty.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tt.yaml), 0o600))

			got, err := config.Load(path)

			require.Error(t, err)
			assert.Nil(t, got)
			assert.Equal(t, tt.wantFields, fieldsOf(err))
		})
	}
}

func TestLoad_InvalidDocuments(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{"unknown key", "provider: {anthropic: {api_key: {env: K}, max_tokens: 1, maxtokens: 2}}", "maxtokens"},
		{"unknown key in a value", "provider: {anthropic: {api_key: {env: K, from: x}, max_tokens: 1}}", `unknown key "from"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "agenty.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tt.yaml), 0o600))

			_, err := config.Load(path)

			require.ErrorContains(t, err, tt.wantErr)
		})
	}
	secretPath := filepath.Join(t.TempDir(), "agenty.yaml")
	require.NoError(t, os.WriteFile(secretPath, []byte("provider: {anthropic: {api_key: sk-ant-oops-0123, max_tokens: 1}}"), 0o600))
	_, err := config.Load(secretPath)
	require.ErrorContains(t, err, "must be {env: NAME} or {value: TEXT}")
	assert.NotContains(t, err.Error(), "sk-ant-oops-0123", "the error never repeats a value written into the file")

	_, err = config.Load(filepath.Join(t.TempDir(), "missing.yaml"))
	require.ErrorContains(t, err, "missing.yaml")
}

func TestResolve_ReadsTheEnvironmentAndCollectsSecrets(t *testing.T) {
	cfg, err := config.Load(filepath.Join("testdata", "full.yaml"))
	require.NoError(t, err)
	env := map[string]string{"ANTHROPIC_API_KEY": "sk-ant-key-0123456789", "TICKETS_TOKEN": "tickets-token-0123"}

	got, err := cfg.Resolve(func(k string) (string, bool) { v, ok := env[k]; return v, ok })

	require.NoError(t, err)
	assert.Equal(t, "sk-ant-key-0123456789", got.AnthropicAPIKey)
	assert.Equal(t, map[string]map[string]string{"tickets": {"TICKETS_TOKEN": "tickets-token-0123", "LOG_LEVEL": "info"}}, got.MCPServerEnv)
	assert.ElementsMatch(t, []string{"sk-ant-key-0123456789", "tickets-token-0123"}, got.Secrets, "everything read from the environment is a secret; plain values are not")
}

func TestResolve_Errors(t *testing.T) {
	cfg, err := config.Load(filepath.Join("testdata", "full.yaml"))
	require.NoError(t, err)
	tests := []struct {
		name      string
		env       map[string]string
		wantField string
		wantErr   string
	}{
		{"missing variable", map[string]string{"TICKETS_TOKEN": "tickets-token-0123"}, "provider.anthropic.api_key", "ANTHROPIC_API_KEY is not set"},
		{"empty variable", map[string]string{"ANTHROPIC_API_KEY": "", "TICKETS_TOKEN": "tickets-token-0123"}, "provider.anthropic.api_key", "ANTHROPIC_API_KEY is not set"},
		{"secret too short to redact", map[string]string{"ANTHROPIC_API_KEY": "sk-ant-key-0123456789", "TICKETS_TOKEN": "short"}, "mcp_servers.tickets.env.TICKETS_TOKEN", "shorter than 8 characters"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := cfg.Resolve(func(k string) (string, bool) { v, ok := tt.env[k]; return v, ok })

			require.ErrorContains(t, err, tt.wantErr)
			assert.Equal(t, []string{tt.wantField}, fieldsOf(err))
		})
	}
}
