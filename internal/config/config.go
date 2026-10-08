// Package config loads the operator configuration: the model provider, the
// database, the MCP servers, central policy, and who may use the API. Secrets never live in the file: a value is
// either read from the environment, {env: NAME}, or written as plain text,
// {value: TEXT}, and everything read from the environment is treated as a
// secret.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/jangraefen/agenty/internal/must"
	"github.com/jangraefen/agenty/internal/policy"
	"github.com/jangraefen/agenty/internal/secret"
)

// Config is the operator configuration.
type Config struct {
	Provider Provider `yaml:"provider"`
	// Database is where the server keeps its state.
	Database   Database             `yaml:"database"`
	MCPServers map[string]MCPServer `yaml:"mcp_servers"`
	// Users may use the API, by name. Each signs in with a bearer token.
	Users map[string]User `yaml:"users"`
	// Workspaces hold harnesses and runs, by name. Only a workspace's
	// members see it, and they may do everything in it.
	Workspaces map[string]Workspace `yaml:"workspaces"`
	// CORS lists the web origins whose pages may call the API.
	CORS      CORS      `yaml:"cors"`
	Runs      Runs      `yaml:"runs"`
	Approvals Approvals `yaml:"approvals"`
	// Policy is central policy: Rego modules in package agenty.tool. Load
	// reads them from the files the config names.
	Policy []policy.Module `yaml:"-"`
}

// file is a config file: the config, and its policy as written.
type file struct {
	Config `yaml:",inline"`
	Policy policySection `yaml:"policy"`
}

// policySection is central policy as written: Rego files relative to the
// config file.
type policySection struct {
	Files []string `yaml:"files"`
}

// Provider configures the model providers. Anthropic is the only one so far.
type Provider struct {
	Anthropic *Anthropic `yaml:"anthropic"`
}

// Anthropic configures the Anthropic provider.
type Anthropic struct {
	APIKey    Value `yaml:"api_key"`
	MaxTokens int   `yaml:"max_tokens"`
	// BaseURL overrides the API endpoint. Empty means Anthropic's.
	BaseURL string `yaml:"base_url"`
	// HistoryCacheTTL is how long the prompt cache keeps a conversation's
	// earlier runs: "5m", the default, or "1h", which costs more to write
	// but outlasts a user who takes a while to reply.
	HistoryCacheTTL string `yaml:"history_cache_ttl"`
}

// Database configures the server's PostgreSQL database.
type Database struct {
	// URL is a PostgreSQL connection URL. It usually holds a password, so
	// read it from the environment.
	URL Value `yaml:"url"`
}

// MCPServer is an MCP server Agenty starts over stdio.
type MCPServer struct {
	Command string           `yaml:"command"`
	Args    []string         `yaml:"args"`
	Env     map[string]Value `yaml:"env"`
	// IdleTimeout is how long a conversation's server keeps running after
	// the conversation's last run, so the next run finds what it held. Unset
	// means DefaultMCPIdleTimeout; zero stops it with each run.
	IdleTimeout *time.Duration `yaml:"idle_timeout"`
}

// DefaultMCPIdleTimeout is an MCP server's idle timeout when none is
// configured.
const DefaultMCPIdleTimeout = 15 * time.Minute

// IdleTimeoutOrDefault returns the server's idle timeout.
func (s MCPServer) IdleTimeoutOrDefault() time.Duration {
	if s.IdleTimeout == nil {
		return DefaultMCPIdleTimeout
	}
	return *s.IdleTimeout
}

// User is a person who may use the API.
type User struct {
	// Token is the user's bearer token. It must come from the environment:
	// a token is a credential.
	Token Value `yaml:"token"`
	// Auditor lets the user read the audit log of every workspace. It gives
	// nothing in a workspace: what the user may do there comes from being a
	// member.
	Auditor bool `yaml:"auditor"`
}

// Workspace is a group of harnesses and runs.
type Workspace struct {
	// Members are the users who may use the workspace.
	Members []string `yaml:"members"`
}

// CORS lists the web origins, such as the web portal's, whose pages may call
// the API. Requests from any other page are refused.
type CORS struct {
	// Origins are written as browsers send them: scheme://host[:port].
	Origins []string `yaml:"origins"`
}

// Runs configures how runs execute.
type Runs struct {
	// Workers is how many runs execute at once; the others wait, queued.
	// Zero means DefaultRunWorkers.
	Workers int `yaml:"workers"`
}

// DefaultRunWorkers is the number of workers when none is configured.
const DefaultRunWorkers = 16

// WorkersOrDefault returns the number of workers.
func (r Runs) WorkersOrDefault() int {
	if r.Workers == 0 {
		return DefaultRunWorkers
	}
	return r.Workers
}

// Approvals configures approval requests.
type Approvals struct {
	// Timeout is how long a call waits for an answer before it is rejected.
	// Zero means DefaultApprovalTimeout.
	Timeout time.Duration `yaml:"timeout"`
}

// DefaultApprovalTimeout is the approval timeout when none is configured.
const DefaultApprovalTimeout = time.Hour

// TokenMinLength is the shortest bearer token accepted: long enough that it
// cannot be guessed.
const TokenMinLength = 32

// namePattern is the form of user and workspace names, which appear in URLs.
var namePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Value is a configuration value: read from an environment variable, which
// makes it a secret, or given as plain text. Exactly one is set.
type Value struct {
	Env   string `yaml:"env"`
	Value string `yaml:"value"`
}

// UnmarshalYAML accepts only {env: NAME} or {value: TEXT}. Its errors never
// repeat what was written, because a secret written into the file by mistake
// must not end up in an error message.
func (v *Value) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: a value must be {env: NAME} or {value: TEXT}; never write a secret into the config", node.Line)
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, val := node.Content[i], node.Content[i+1]
		switch key.Value {
		case "env":
			v.Env = val.Value
		case "value":
			v.Value = val.Value
		default:
			return fmt.Errorf("line %d: unknown key %q in a value", key.Line, key.Value)
		}
	}
	return nil
}

var _ error = (*FieldError)(nil) //nolint:errcheck // an interface guard, not a discarded error

// FieldError reports an invalid field, named by its YAML path.
type FieldError struct {
	Field string
	Msg   string
}

func (e *FieldError) Error() string {
	return fmt.Sprintf("field %q: %s", e.Field, e.Msg)
}

// Load reads, parses and validates the config file at path, and reads its
// policy files relative to it. Unknown keys are rejected.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the operator names the config file.
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var f file
	if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("config %s: decode: %w", path, err)
	}
	if err := f.validate(); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	c := f.Config
	for i, name := range f.Policy.Files {
		src, err := os.ReadFile(filepath.Join(filepath.Dir(path), name)) //nolint:gosec // G304: the config names its policy files.
		if err != nil {
			return nil, fmt.Errorf("config %s: %w", path, &FieldError{Field: fmt.Sprintf("policy.files[%d]", i), Msg: err.Error()})
		}
		c.Policy = append(c.Policy, policy.Module{Name: name, Source: string(src)})
	}
	return &c, nil
}

func (f *file) validate() error {
	var errs []error
	add := func(field, msg string) {
		errs = append(errs, &FieldError{Field: field, Msg: msg})
	}
	checkValue := func(field string, v Value) {
		if (v.Env == "") == (v.Value == "") {
			add(field, "needs exactly one of env or value")
		}
	}

	if a := f.Provider.Anthropic; a == nil {
		add("provider.anthropic", "is required")
	} else {
		checkValue("provider.anthropic.api_key", a.APIKey)
		if a.MaxTokens <= 0 {
			add("provider.anthropic.max_tokens", "must be greater than 0")
		}
		if !slices.Contains([]string{"", "5m", "1h"}, a.HistoryCacheTTL) {
			add("provider.anthropic.history_cache_ttl", `must be "5m" or "1h"`)
		}
	}
	checkValue("database.url", f.Database.URL)
	for _, name := range slices.Sorted(maps.Keys(f.MCPServers)) {
		srv := f.MCPServers[name]
		if srv.IdleTimeout != nil && *srv.IdleTimeout < 0 {
			add("mcp_servers."+name+".idle_timeout", "must not be negative")
		}
		if srv.Command == "" {
			add("mcp_servers."+name+".command", "is required")
		}
		for _, key := range slices.Sorted(maps.Keys(srv.Env)) {
			checkValue("mcp_servers."+name+".env."+key, srv.Env[key])
		}
	}
	for i, name := range f.Policy.Files {
		field := fmt.Sprintf("policy.files[%d]", i)
		switch {
		case name == "":
			add(field, "must not be empty")
		case slices.Contains(f.Policy.Files[:i], name):
			// Policy modules are named by their files, and two of one name
			// cannot be compiled together.
			add(field, fmt.Sprintf("%q is listed twice", name))
		}
	}
	if len(f.Users) == 0 {
		add("users", "none are configured, so no one could sign in")
	}
	for _, user := range slices.Sorted(maps.Keys(f.Users)) {
		if !namePattern.MatchString(user) {
			add("users."+user, "a user name must be lowercase letters and digits, separated by single dashes")
		}
		if t := f.Users[user].Token; t.Env == "" || t.Value != "" {
			add("users."+user+".token", "must be {env: NAME}: a token is a credential, so it never lives in the config")
		}
	}
	for _, ws := range slices.Sorted(maps.Keys(f.Workspaces)) {
		if !namePattern.MatchString(ws) {
			add("workspaces."+ws, "a workspace name must be lowercase letters and digits, separated by single dashes")
		}
		for i, member := range f.Workspaces[ws].Members {
			field := fmt.Sprintf("workspaces.%s.members[%d]", ws, i)
			switch {
			case slices.Contains(f.Workspaces[ws].Members[:i], member):
				add(field, fmt.Sprintf("%q is listed twice", member))
			case !hasKey(f.Users, member):
				add(field, fmt.Sprintf("%q is not a configured user", member))
			}
		}
	}
	if f.Runs.Workers < 0 {
		add("runs.workers", "must not be negative")
	}
	if f.Approvals.Timeout < 0 {
		add("approvals.timeout", "must not be negative")
	}
	for i, origin := range f.CORS.Origins {
		if u, err := url.Parse(origin); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || origin != u.Scheme+"://"+u.Host {
			add(fmt.Sprintf("cors.origins[%d]", i), "an origin must be scheme://host[:port], with an http or https scheme and nothing after the host")
		}
	}
	return errors.Join(errs...)
}

func hasKey[V any](m map[string]V, key string) bool {
	_, ok := m[key]
	return ok
}

// Resolved holds the configuration's values, read from the environment where
// configured, and the redactor for the secrets among them.
type Resolved struct {
	AnthropicAPIKey string
	DatabaseURL     string
	// MCPServerEnv holds each server's environment, by server name.
	MCPServerEnv map[string]map[string]string
	// UserTokens holds each user's bearer token, by user name. Tokens are
	// distinct.
	UserTokens map[string]string
	// Redactor redacts every value read from the environment. It is the one
	// redactor of a run: logs, output and the tool gateway all use it.
	Redactor *secret.Redactor
}

// Resolve reads the configured environment variables through lookup.
func (c *Config) Resolve(lookup func(string) (string, bool)) (*Resolved, error) {
	r := &Resolved{MCPServerEnv: map[string]map[string]string{}, UserTokens: map[string]string{}}
	var secrets []string
	var errs []error
	resolve := func(field string, v Value) string {
		if v.Env == "" {
			return v.Value
		}
		val, ok := lookup(v.Env)
		switch {
		case !ok || val == "":
			errs = append(errs, &FieldError{Field: field, Msg: fmt.Sprintf("environment variable %s is not set", v.Env)})
		case len(val) < secret.MinLength:
			errs = append(errs, &FieldError{Field: field, Msg: fmt.Sprintf("comes from the environment, so it is redacted as a secret, but it is shorter than %d characters", secret.MinLength)})
		default:
			secrets = append(secrets, val)
		}
		return val
	}

	r.AnthropicAPIKey = resolve("provider.anthropic.api_key", c.Provider.Anthropic.APIKey)
	r.DatabaseURL = resolve("database.url", c.Database.URL)
	for _, name := range slices.Sorted(maps.Keys(c.MCPServers)) {
		env := map[string]string{}
		for _, key := range slices.Sorted(maps.Keys(c.MCPServers[name].Env)) {
			env[key] = resolve("mcp_servers."+name+".env."+key, c.MCPServers[name].Env[key])
		}
		r.MCPServerEnv[name] = env
	}
	owners := map[string]string{}
	for _, user := range slices.Sorted(maps.Keys(c.Users)) {
		field := "users." + user + ".token"
		token := resolve(field, c.Users[user].Token)
		switch {
		case token == "":
			// resolve reported it.
		case len(token) < TokenMinLength:
			errs = append(errs, &FieldError{Field: field, Msg: fmt.Sprintf("is shorter than %d characters, too short to be unguessable", TokenMinLength)})
		case owners[token] != "":
			errs = append(errs, &FieldError{Field: field, Msg: "is the same token as user " + owners[token] + "'s; each user needs their own"})
		default:
			owners[token] = user
			r.UserTokens[user] = token
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	// Every secret is long enough: resolve rejected the short ones.
	r.Redactor = must.Value(secret.NewRedactor(secrets))
	return r, nil
}
