// Package harness loads and validates harness definitions.
package harness

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"

	"go.yaml.in/yaml/v3"
)

// Harness is the declarative definition of an agent.
type Harness struct {
	Name         string   `yaml:"name"`
	Instructions string   `yaml:"instructions"`
	Model        Model    `yaml:"model"`
	Tools        []string `yaml:"tools"`
	Limits       Limits   `yaml:"limits"`
}

// Model selects the model provider and model a harness runs on.
type Model struct {
	Provider string `yaml:"provider"`
	Name     string `yaml:"name"`
}

// Limits bound a single run of a harness.
type Limits struct {
	MaxSteps int `yaml:"max_steps"`
}

var _ error = (*FieldError)(nil)

// FieldError reports an invalid field, named by its YAML path.
type FieldError struct {
	Field string
	Msg   string
}

func (e *FieldError) Error() string {
	return fmt.Sprintf("field %q: %s", e.Field, e.Msg)
}

var slug = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Load reads, parses and validates the harness file at path.
func Load(path string) (*Harness, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load harness: %w", err)
	}
	h, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("harness %s: %w", path, err)
	}
	return h, nil
}

// Parse decodes and validates a harness from a single YAML document.
// Unknown keys are rejected.
func Parse(data []byte) (*Harness, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var h Harness
	if err := dec.Decode(&h); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("decode: harness must be a single document")
	}

	if err := h.Validate(); err != nil {
		return nil, err
	}
	return &h, nil
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
		switch {
		case tool == "":
			add(field, "must not be empty")
		case seen[tool]:
			add(field, fmt.Sprintf("duplicate tool %q", tool))
		}
		seen[tool] = true
	}

	if h.Limits.MaxSteps <= 0 {
		add("limits.max_steps", "must be greater than 0")
	}
	return errors.Join(errs...)
}
