// Package harness loads and validates harness definitions: the declarative,
// framework-free description of an agent, with its instructions, model,
// granted tools, limits and optional policy.
//
// # Role in the architecture
//
// A harness is written as a YAML file by a builder, who never touches
// credentials or MCP server configuration: those live in the operator
// config (internal/config). The CLI (internal/cli, agenty apply) reads the
// file with Load, which resolves its policy files, and sends the result to
// the server. The API types (internal/api) convert Harness to and from its
// JSON form; the server validates an uploaded harness again, and the store
// (internal/store) validates it once more before it stores it as an
// immutable version. Package agent validates the version it runs a last
// time and hands its grants and limits to the tool gateway and its policy
// to the policy engine.
//
// The package depends on internal/policy only for the Module type and for
// RulesModule, which turns inline rules into a module, and on
// internal/toolgateway for the rule a tool name must follow. It compiles no
// policy itself: the policy engine checks the modules when it compiles them.
//
// # Contents and how they fit
//
// Harness is the definition, with Model and Limits as its parts. A harness
// file is decoded into file, which adds the policySection as written; Load
// reads the file, parse decodes and validates it, and Load then turns the
// policy section into Harness.Policy. Validate checks the fields of a
// Harness however it was made, and reports every problem at once as
// FieldErrors named by their YAML path, so a builder fixes them in one go.
//
// # Trust-model guarantees
//
// The harness is where grants and limits are declared, so its validation
// guards what the rest of the system relies on: every grant is a valid,
// unique tool name, which the gateway enforces default deny with
// (guarantee 3), and every run is bounded by positive step and tool call
// limits. Harness policy can only add deny and require_approval rules, in
// a layer of its own, so a builder tightens central policy and never
// loosens it (guarantee 4). Unknown keys are refused, so a misspelt field,
// such as a limit, cannot silently fall back to nothing.
package harness

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"go.yaml.in/yaml/v3"

	"github.com/jangraefen/agenty/internal/policy"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

// Harness is the declarative definition of an agent. It is stored as an
// immutable version per change, and a conversation runs the version it
// started with. Its YAML and JSON forms name the same fields, except
// Policy, which the YAML file writes as files and rules (see policySection)
// and the JSON form holds as resolved modules.
type Harness struct {
	// Name identifies the harness within its workspace; a slug, as it
	// appears in URLs and audit records.
	Name string `yaml:"name" json:"name"`
	// Instructions are the system prompt of every run.
	Instructions string `yaml:"instructions" json:"instructions"`
	Model        Model  `yaml:"model" json:"model"`
	// Tools are the grants: the names of the tools the agent may call, as
	// "<server>_<tool>". A tool not listed is denied.
	Tools  []string `yaml:"tools" json:"tools"`
	Limits Limits   `yaml:"limits" json:"limits"`
	// Policy optionally tightens central policy for this harness: Rego
	// modules in package agenty.tool, compiled together as one layer, so they
	// may share rules with each other, but not with central policy. Load
	// fills it from the file's policy section; Validate leaves it to the
	// policy engine, which checks the modules when it compiles them.
	Policy []policy.Module `yaml:"-" json:"policy"`
}

// file is a harness file: the harness, and its policy as written. The
// inline Harness keeps the file's keys flat, and its Policy field, tagged
// yaml:"-", leaves the policy key to this struct, so the file's form of
// policy never leaks into Harness.
type file struct {
	Harness `yaml:",inline"`
	Policy  *policySection `yaml:"policy"`
}

// policySection is a harness policy as written: Rego files, inline rules, or
// both.
type policySection struct {
	// Files are Rego modules in package agenty.tool, relative to the harness
	// file.
	Files []string `yaml:"files"`
	// Rules are Rego rules without a package line; package agenty.tool is
	// implied.
	Rules string `yaml:"rules"`
}

// Model selects the model provider and model a harness runs on. The server
// builds the model from it, with the credentials of the operator config, so
// a harness names a model but never holds a key.
type Model struct {
	Provider string `yaml:"provider" json:"provider"`
	Name     string `yaml:"name" json:"name"`
}

// Limits bound a single run of a harness, not a conversation: each run of
// a conversation has the full limits again.
type Limits struct {
	// MaxSteps bounds the model calls of a run.
	MaxSteps int `yaml:"max_steps" json:"max_steps"`
	// MaxToolCalls bounds the tool calls of a run, denied ones included.
	MaxToolCalls int `yaml:"max_tool_calls" json:"max_tool_calls"`
}

// A compile-time check that *FieldError is an error.
var _ error = (*FieldError)(nil) //nolint:errcheck // an interface guard, not a discarded error

// FieldError reports an invalid field, named by its YAML path, such as
// "tools[2]" or "limits.max_steps". Validation joins one per problem with
// errors.Join, so callers can name every field a builder has to fix.
type FieldError struct {
	Field string
	Msg   string
}

// Error names the field and says what is wrong with it.
func (e *FieldError) Error() string {
	return fmt.Sprintf("field %q: %s", e.Field, e.Msg)
}

// slug is the form of a harness name: lowercase letters and digits in
// groups joined by single hyphens, safe in URLs without escaping.
var slug = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// packageLine finds a package declaration on any line of inline rules,
// which must not have one, as RulesModule adds package agenty.tool.
var packageLine = regexp.MustCompile(`(?m)^\s*package\s`)

// Load reads, parses and validates the harness file at path, and reads its
// policy files, if any, relative to it. Unknown keys are rejected.
func Load(path string) (*Harness, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: reading the file the caller names is the purpose of Load.
	if err != nil {
		return nil, fmt.Errorf("load harness: %w", err)
	}
	f, err := parse(data)
	if err != nil {
		return nil, fmt.Errorf("harness %s: %w", path, err)
	}
	// Policy files are read here, not by the server: the server never reads
	// a builder's files, it gets the harness with its policy resolved into
	// modules. A relative path is relative to the harness file, so a harness
	// and its policy move together. Each module keeps the name it was written
	// with, which policy errors then cite.
	h := f.Harness
	if p := f.Policy; p != nil {
		for i, name := range p.Files {
			policyPath := name
			if !filepath.IsAbs(policyPath) {
				policyPath = filepath.Join(filepath.Dir(path), policyPath)
			}
			src, err := os.ReadFile(policyPath) //nolint:gosec // G304: the harness names its own policy file.
			if err != nil {
				return nil, fmt.Errorf("harness %s: %w", path, &FieldError{Field: fmt.Sprintf("policy.files[%d]", i), Msg: err.Error()})
			}
			h.Policy = append(h.Policy, policy.Module{Name: name, Source: string(src)})
		}
		// Inline rules come last and form one module with the package line
		// implied, so they share rules with the files in the same layer.
		if p.Rules != "" {
			h.Policy = append(h.Policy, policy.RulesModule(h.Name+" (inline policy)", p.Rules))
		}
	}
	return &h, nil
}

// parse decodes and validates a harness file from a single YAML document.
// It does not touch the file system, so the policy files are read only once
// the harness itself is valid.
func parse(data []byte) (*file, error) {
	// Unknown keys are errors: a misspelt key, such as a limit, would
	// otherwise be dropped silently and leave its field at zero.
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	// An empty file decodes to EOF, which is not a decoding error: it
	// leaves every field empty, and validation then names them all.
	var f file
	if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode: %w", err)
	}
	// A second document would be ignored by a single Decode, so its
	// presence is refused: a file means exactly one harness.
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("decode: harness must be a single document")
	}

	// Both checks run, so the builder sees every problem at once.
	if err := errors.Join(f.Validate(), f.Policy.validate()); err != nil {
		return nil, err
	}
	return &f, nil
}

// Validate reports every invalid field, joined into one error of FieldErrors.
// It checks the harness however it was made, from a file, the API or the
// store, which is why the CLI, the server, the store and the agent all call
// it. It does not check the policy modules, which only the policy engine
// can, nor whether a granted tool exists, which depends on the operator
// config and is checked when the gateway starts.
func (h *Harness) Validate() error {
	var errs []error
	add := func(field, msg string) {
		errs = append(errs, &FieldError{Field: field, Msg: msg})
	}

	switch {
	case h.Name == "":
		add("name", "is required")
	case !slug.MatchString(h.Name):
		add("name", "must be lowercase letters, digits and single hyphens")
	}
	if h.Instructions == "" {
		add("instructions", "is required")
	}
	if h.Model.Provider == "" {
		add("model.provider", "is required")
	}
	if h.Model.Name == "" {
		add("model.name", "is required")
	}

	// Grants are checked by the gateway's own rule, so a name valid here is
	// one the gateway and the model provider accept. A duplicate grant is a
	// mistake, not a harmless repeat, and is reported as one.
	seen := make(map[string]bool, len(h.Tools))
	for i, tool := range h.Tools {
		field := fmt.Sprintf("tools[%d]", i)
		if err := toolgateway.ValidateToolName(tool); err != nil {
			add(field, err.Error())
		} else if seen[tool] {
			add(field, fmt.Sprintf("duplicate tool %q", tool))
		}
		seen[tool] = true
	}

	// Limits are required: an unset limit is zero, not unlimited, so every
	// run is bounded.
	if h.Limits.MaxSteps <= 0 {
		add("limits.max_steps", "must be greater than 0")
	}
	if h.Limits.MaxToolCalls <= 0 {
		add("limits.max_tool_calls", "must be greater than 0")
	}
	return errors.Join(errs...)
}

// validate reports every invalid field of the policy section, if there is
// one. A nil section, a harness without policy, is valid: central policy
// alone applies. It checks only the section's form; the files' content is
// read by Load and checked by the policy engine.
func (p *policySection) validate() error {
	if p == nil {
		return nil
	}
	var errs []error
	add := func(field, msg string) {
		errs = append(errs, &FieldError{Field: field, Msg: msg})
	}
	switch {
	case len(p.Files) == 0 && p.Rules == "":
		add("policy", "needs files or rules")
	case packageLine.MatchString(p.Rules):
		add("policy.rules", "must not declare a package; package agenty.tool is implied")
	}
	seen := make(map[string]bool, len(p.Files))
	for i, name := range p.Files {
		field := fmt.Sprintf("policy.files[%d]", i)
		switch {
		case name == "":
			add(field, "must not be empty")
		case seen[name]:
			add(field, fmt.Sprintf("duplicate file %q", name))
		}
		seen[name] = true
	}
	return errors.Join(errs...)
}
