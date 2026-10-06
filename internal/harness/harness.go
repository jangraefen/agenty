// Package harness loads and validates harness definitions.
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

// Harness is the declarative definition of an agent.
type Harness struct {
	Name         string   `yaml:"name"`
	Instructions string   `yaml:"instructions"`
	Model        Model    `yaml:"model"`
	Tools        []string `yaml:"tools"`
	Limits       Limits   `yaml:"limits"`
	// Policy optionally tightens central policy for this harness: Rego
	// modules in package agenty.tool, compiled together as one layer, so they
	// may share rules with each other, but not with central policy. Load
	// fills it from the file's policy section.
	Policy []policy.Module `yaml:"-"`
}

// file is a harness file: the harness, and its policy as written.
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

// Model selects the model provider and model a harness runs on.
type Model struct {
	Provider string `yaml:"provider"`
	Name     string `yaml:"name"`
}

// Limits bound a single run of a harness.
type Limits struct {
	// MaxSteps bounds the model calls of a run.
	MaxSteps int `yaml:"max_steps"`
	// MaxToolCalls bounds the tool calls of a run, denied ones included.
	MaxToolCalls int `yaml:"max_tool_calls"`
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

var slug = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

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
		if p.Rules != "" {
			h.Policy = append(h.Policy, policy.RulesModule(h.Name+" (inline policy)", p.Rules))
		}
	}
	return &h, nil
}

// parse decodes and validates a harness file from a single YAML document.
func parse(data []byte) (*file, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var f file
	if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("decode: harness must be a single document")
	}

	if err := errors.Join(f.Validate(), f.Policy.validate()); err != nil {
		return nil, err
	}
	return &f, nil
}

// Validate reports every invalid field, joined into one error of FieldErrors.
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

	if h.Limits.MaxSteps <= 0 {
		add("limits.max_steps", "must be greater than 0")
	}
	if h.Limits.MaxToolCalls <= 0 {
		add("limits.max_tool_calls", "must be greater than 0")
	}
	return errors.Join(errs...)
}

// validate reports every invalid field of the policy section, if there is
// one.
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
