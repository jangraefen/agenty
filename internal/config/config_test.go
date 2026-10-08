package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/config"
)

func TestLoad_Valid(t *testing.T) {
	got, err := config.Load(filepath.Join("testdata", "full.yaml"))
	require.NoError(t, err)

	assert.Equal(t, &config.Anthropic{
		APIKey:          config.Value{Env: "ANTHROPIC_API_KEY"},
		MaxTokens:       4096,
		BaseURL:         "http://localhost:8080",
		HistoryCacheTTL: "1h",
	}, got.Provider.Anthropic)
	assert.Equal(t, &config.Database{URL: config.Value{Env: "DATABASE_URL"}}, got.Database)
	assert.Equal(t, map[string]config.MCPServer{
		"tickets": {
			Command: "tickets-mcp",
			Args:    []string{"--stdio"},
			Env: map[string]config.Value{
				"TICKETS_TOKEN": {Env: "TICKETS_TOKEN"},
				"LOG_LEVEL":     {Value: "info"},
			},
			IdleTimeout: new(5 * time.Minute),
		},
	}, got.MCPServers)
	assert.Equal(t, 5*time.Minute, got.MCPServers["tickets"].IdleTimeoutOrDefault())
	assert.Equal(t, config.DefaultMCPIdleTimeout, config.MCPServer{}.IdleTimeoutOrDefault())
	assert.Zero(t, config.MCPServer{IdleTimeout: new(time.Duration(0))}.IdleTimeoutOrDefault(), "zero stops a server with each run")
	assert.Equal(t, map[string]config.User{
		"alice": {Token: config.Value{Env: "ALICE_TOKEN"}},
		"bob":   {Token: config.Value{Env: "BOB_TOKEN"}},
		"carol": {Token: config.Value{Env: "CAROL_TOKEN"}, Auditor: true},
	}, got.Users)
	assert.Equal(t, map[string]config.Workspace{
		"notes": {Members: []string{"alice", "bob"}},
		"ops":   {Members: []string{"bob"}},
	}, got.Workspaces)
	assert.Equal(t, config.CORS{Origins: []string{"http://localhost:5173"}}, got.CORS)
	assert.Equal(t, 15*time.Minute, got.Approvals.Timeout)
	assert.Equal(t, 4, got.Runs.WorkersOrDefault())
	assert.Equal(t, config.DefaultRunWorkers, config.Runs{}.WorkersOrDefault())
	require.Len(t, got.Policy, 1)
	assert.Equal(t, "policies/central.rego", got.Policy[0].Name)
	assert.Contains(t, got.Policy[0].Source, "writes need a human", "policy files are read relative to the config file")

	minimal, err := config.Load(filepath.Join("testdata", "minimal.yaml"))
	require.NoError(t, err)
	assert.Nil(t, minimal.Database, "only the server needs a database")
	assert.Empty(t, minimal.MCPServers)
	assert.Empty(t, minimal.Policy)
	assert.Empty(t, minimal.Users)
	assert.Empty(t, minimal.Workspaces)
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
	const provider = "provider: {anthropic: {api_key: {env: K}, max_tokens: 1}}\n"
	tests := []struct {
		name       string
		yaml       string
		wantFields []string
	}{
		{"no provider", "{}", []string{"provider.anthropic"}},
		{"no max tokens", "provider: {anthropic: {api_key: {env: K}}}", []string{"provider.anthropic.max_tokens"}},
		{"history cache TTL", "provider: {anthropic: {api_key: {env: K}, max_tokens: 1, history_cache_ttl: 10m}}", []string{"provider.anthropic.history_cache_ttl"}},
		{"api key with both env and value", "provider: {anthropic: {api_key: {env: K, value: v}, max_tokens: 1}}", []string{"provider.anthropic.api_key"}},
		{"api key with neither", "provider: {anthropic: {api_key: {}, max_tokens: 1}}", []string{"provider.anthropic.api_key"}},
		{"mcp server without command", "provider: {anthropic: {api_key: {env: K}, max_tokens: 1}}\nmcp_servers: {tickets: {}}", []string{"mcp_servers.tickets.command"}},
		{"mcp server env value with neither", "provider: {anthropic: {api_key: {env: K}, max_tokens: 1}}\nmcp_servers: {tickets: {command: x, env: {TOKEN: {}}}}", []string{"mcp_servers.tickets.env.TOKEN"}},
		{"negative mcp server idle timeout", provider + "mcp_servers: {tickets: {command: x, idle_timeout: -1m}}", []string{"mcp_servers.tickets.idle_timeout"}},
		{"database url with neither", "provider: {anthropic: {api_key: {env: K}, max_tokens: 1}}\ndatabase: {url: {}}", []string{"database.url"}},
		{"empty policy file name", "provider: {anthropic: {api_key: {env: K}, max_tokens: 1}}\npolicy: {files: [\"\"]}", []string{"policy.files[0]"}},
		{"missing policy file", "provider: {anthropic: {api_key: {env: K}, max_tokens: 1}}\npolicy: {files: [nope.rego]}", []string{"policy.files[0]"}},
		{"policy file listed twice", provider + "policy: {files: [a.rego, b.rego, a.rego]}", []string{"policy.files[2]"}},
		{"user token written into the file", provider + "users: {alice: {token: {value: alice-token-0123456789012345678901}}}", []string{"users.alice.token"}},
		{"user without a token", provider + "users: {alice: {}}", []string{"users.alice.token"}},
		{"user name that is not a slug", provider + "users: {Alice: {token: {env: T}}}", []string{"users.Alice"}},
		{"workspace name that is not a slug", provider + "workspaces: {My Notes: {members: []}}", []string{"workspaces.My Notes"}},
		{"member who is not a user", provider + "users: {alice: {token: {env: T}}}\nworkspaces: {notes: {members: [alice, mallory]}}", []string{"workspaces.notes.members[1]"}},
		{"member listed twice", provider + "users: {alice: {token: {env: T}}}\nworkspaces: {notes: {members: [alice, alice]}}", []string{"workspaces.notes.members[1]"}},
		{"origin with a path", provider + "cors: {origins: [\"http://localhost:5173/\"]}", []string{"cors.origins[0]"}},
		{"origin without a scheme", provider + "cors: {origins: [localhost:5173]}", []string{"cors.origins[0]"}},
		{"wildcard origin", provider + "cors: {origins: [\"*\"]}", []string{"cors.origins[0]"}},
		{"negative approval timeout", provider + "approvals: {timeout: -1m}", []string{"approvals.timeout"}},
		{"negative workers", provider + "runs: {workers: -1}", []string{"runs.workers"}},
		{"origin with another scheme", provider + "cors: {origins: [\"ftp://localhost\"]}", []string{"cors.origins[0]"}},
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

func TestResolve_ReadsTheEnvironmentAndRedactsItsSecrets(t *testing.T) {
	cfg, err := config.Load(filepath.Join("testdata", "full.yaml"))
	require.NoError(t, err)
	env := fullEnv()

	got, err := cfg.Resolve(func(k string) (string, bool) { v, ok := env[k]; return v, ok })

	require.NoError(t, err)
	assert.Equal(t, "sk-ant-key-0123456789", got.AnthropicAPIKey)
	assert.Equal(t, "postgres://agenty:db-password-0123@localhost/agenty", got.DatabaseURL)
	assert.Equal(t, map[string]map[string]string{"tickets": {"TICKETS_TOKEN": "tickets-token-0123", "LOG_LEVEL": "info"}}, got.MCPServerEnv)
	assert.Equal(t, map[string]string{"alice": env["ALICE_TOKEN"], "bob": env["BOB_TOKEN"], "carol": env["CAROL_TOKEN"]}, got.UserTokens)
	assert.Equal(t, "[redacted]", got.Redactor.String(env["ALICE_TOKEN"]), "user tokens are secrets")
	assert.Equal(t, "[redacted] [redacted] [redacted] info", got.Redactor.String("sk-ant-key-0123456789 tickets-token-0123 postgres://agenty:db-password-0123@localhost/agenty info"),
		"everything read from the environment is a secret; plain values are not")
}

// fullEnv is an environment that sets every variable full.yaml reads.
func fullEnv() map[string]string {
	return map[string]string{
		"ANTHROPIC_API_KEY": "sk-ant-key-0123456789",
		"TICKETS_TOKEN":     "tickets-token-0123",
		"DATABASE_URL":      "postgres://agenty:db-password-0123@localhost/agenty",
		"ALICE_TOKEN":       "alice-token-0123456789abcdefghijklmn",
		"BOB_TOKEN":         "bob-token-0123456789abcdefghijklmnop",
		"CAROL_TOKEN":       "carol-token-0123456789abcdefghijklm",
	}
}

func TestResolve_Errors(t *testing.T) {
	cfg, err := config.Load(filepath.Join("testdata", "full.yaml"))
	require.NoError(t, err)
	tests := []struct {
		name       string
		set        map[string]string
		unset      string
		wantFields []string
		wantErr    string
	}{
		{"missing variable", nil, "ANTHROPIC_API_KEY", []string{"provider.anthropic.api_key"}, "ANTHROPIC_API_KEY is not set"},
		{"empty variable", map[string]string{"ANTHROPIC_API_KEY": ""}, "", []string{"provider.anthropic.api_key"}, "ANTHROPIC_API_KEY is not set"},
		{"secret too short to redact", map[string]string{"TICKETS_TOKEN": "short"}, "", []string{"mcp_servers.tickets.env.TICKETS_TOKEN"}, "shorter than 8 characters"},
		{"missing token", nil, "BOB_TOKEN", []string{"users.bob.token"}, "BOB_TOKEN is not set"},
		{"token too short to be unguessable", map[string]string{"ALICE_TOKEN": "alice-token-0123"}, "", []string{"users.alice.token"}, "shorter than 32 characters"},
		{"two users with one token", map[string]string{"BOB_TOKEN": "alice-token-0123456789abcdefghijklmn"}, "", []string{"users.bob.token"}, "the same token as user alice"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := fullEnv()
			delete(env, tt.unset)
			for k, v := range tt.set {
				env[k] = v
			}

			_, err := cfg.Resolve(func(k string) (string, bool) { v, ok := env[k]; return v, ok })

			require.ErrorContains(t, err, tt.wantErr)
			assert.Equal(t, tt.wantFields, fieldsOf(err))
			for _, v := range fullEnv() {
				assert.NotContains(t, err.Error(), v, "an error never repeats a secret")
			}
		})
	}
}

func TestFieldError_Error(t *testing.T) {
	err := &config.FieldError{Field: "provider.anthropic.max_tokens", Msg: "must be greater than 0"}
	assert.Equal(t, `field "provider.anthropic.max_tokens": must be greater than 0`, err.Error())
}
