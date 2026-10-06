// Package config loads the operator configuration: the model provider, the
// MCP servers, and central policy. Secrets never live in the file: a value is
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
	"os"
	"path/filepath"
	"slices"

	"go.yaml.in/yaml/v3"

	"github.com/jangraefen/agenty/internal/policy"
	"github.com/jangraefen/agenty/internal/secret"
)

// Config is the operator configuration.
type Config struct {
	Provider   Provider             `yaml:"provider"`
	MCPServers map[string]MCPServer `yaml:"mcp_servers"`
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
}

// MCPServer is an MCP server Agenty starts over stdio.
type MCPServer struct {
	Command string           `yaml:"command"`
	Args    []string         `yaml:"args"`
	Env     map[string]Value `yaml:"env"`
}

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
	}
	for _, name := range slices.Sorted(maps.Keys(f.MCPServers)) {
		srv := f.MCPServers[name]
		if srv.Command == "" {
			add("mcp_servers."+name+".command", "is required")
		}
		for _, key := range slices.Sorted(maps.Keys(srv.Env)) {
			checkValue("mcp_servers."+name+".env."+key, srv.Env[key])
		}
	}
	for i, name := range f.Policy.Files {
		if name == "" {
			add(fmt.Sprintf("policy.files[%d]", i), "must not be empty")
		}
	}
	return errors.Join(errs...)
}

// Resolved holds the configuration's values, read from the environment where
// configured, and the secrets among them.
type Resolved struct {
	AnthropicAPIKey string
	// MCPServerEnv holds each server's environment, by server name.
	MCPServerEnv map[string]map[string]string
	// Secrets are all values read from the environment. They are redacted
	// from everything the model and the audit log see.
	Secrets []string
}

// Resolve reads the configured environment variables through lookup.
func (c *Config) Resolve(lookup func(string) (string, bool)) (*Resolved, error) {
	r := &Resolved{MCPServerEnv: map[string]map[string]string{}}
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
			r.Secrets = append(r.Secrets, val)
		}
		return val
	}

	r.AnthropicAPIKey = resolve("provider.anthropic.api_key", c.Provider.Anthropic.APIKey)
	for _, name := range slices.Sorted(maps.Keys(c.MCPServers)) {
		env := map[string]string{}
		for _, key := range slices.Sorted(maps.Keys(c.MCPServers[name].Env)) {
			env[key] = resolve("mcp_servers."+name+".env."+key, c.MCPServers[name].Env[key])
		}
		r.MCPServerEnv[name] = env
	}
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return r, nil
}
