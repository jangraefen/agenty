// Package toolgateway is the single path every tool call takes.
//
// For each call the gateway checks the grant, resolves the tool, decides,
// records the decision, executes, and records the result. A call is never
// executed unless its decision has been recorded.
package toolgateway

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ErrDenied is returned for every call the gateway refuses to execute.
var ErrDenied = errors.New("tool call denied")

// ErrAudit is returned when a decision or result cannot be recorded.
var ErrAudit = errors.New("audit record failed")

// Tool executes one typed capability. Only the gateway calls it.
type Tool interface {
	Definition() Definition
	Call(ctx context.Context, args json.RawMessage) (json.RawMessage, error)
}

// Definition describes a tool to the model.
type Definition struct {
	Name        string
	Description string
	// InputSchema is the JSON Schema of the tool arguments.
	InputSchema json.RawMessage
}

// ToolCall is a request, usually from the model, to call a tool.
type ToolCall struct {
	Name string
	Args json.RawMessage
}

// Config configures a Gateway.
type Config struct {
	// RunID identifies the run the gateway serves. It is required and is
	// carried by every audit record.
	RunID string
	// Granted names the tools the harness may call. Anything else is denied.
	Granted []string
	// Tools are the executors the gateway can resolve calls to.
	Tools []Tool
	// Audit records every decision and result. It is required.
	Audit Audit
}

// Gateway checks, executes and records tool calls.
type Gateway struct {
	runID   string
	granted map[string]bool
	tools   map[string]Tool
	defs    []Definition
	audit   Audit
}

// New returns a Gateway for cfg. The grants are copied, so later changes to
// cfg do not affect the gateway.
func New(cfg Config) (*Gateway, error) {
	if cfg.RunID == "" {
		return nil, errors.New("toolgateway: run id is required")
	}
	if cfg.Audit == nil {
		return nil, errors.New("toolgateway: audit is required")
	}
	g := &Gateway{
		runID:   cfg.RunID,
		granted: make(map[string]bool, len(cfg.Granted)),
		tools:   make(map[string]Tool, len(cfg.Tools)),
		audit:   cfg.Audit,
	}
	for i, name := range cfg.Granted {
		if name == "" {
			return nil, fmt.Errorf("toolgateway: grant %d is empty", i)
		}
		g.granted[name] = true
	}
	for i, tool := range cfg.Tools {
		if tool == nil {
			return nil, fmt.Errorf("toolgateway: tool %d is nil", i)
		}
		def := tool.Definition()
		name := def.Name
		if name == "" {
			return nil, fmt.Errorf("toolgateway: tool %d has no name", i)
		}
		if _, dup := g.tools[name]; dup {
			return nil, fmt.Errorf("toolgateway: duplicate tool %q", name)
		}
		g.tools[name] = tool
		if g.granted[name] {
			g.defs = append(g.defs, def)
		}
	}
	slices.SortFunc(g.defs, func(a, b Definition) int { return strings.Compare(a.Name, b.Name) })
	return g, nil
}

// Definitions describes the tools the run may call: granted and resolved,
// sorted by name.
func (g *Gateway) Definitions() []Definition {
	return slices.Clone(g.defs)
}

// Call runs one tool call through the gateway. A denied call returns an
// error wrapping ErrDenied. A failure to record returns an error wrapping
// ErrAudit; if only the result could not be recorded, the result is returned
// with that error.
func (g *Gateway) Call(ctx context.Context, call ToolCall) (json.RawMessage, error) {
	rec := Record{
		RunID:  g.runID,
		CallID: rand.Text(),
		Event:  EventDecision,
		Tool:   call.Name,
		Args:   call.Args,
	}
	tool, decision, reason := g.decide(call)
	rec.Decision, rec.Reason = decision, reason

	auditErr := g.record(ctx, rec)
	if decision != Allow {
		return nil, errors.Join(fmt.Errorf("%w: %s: %s", ErrDenied, call.Name, reason), auditErr)
	}
	if auditErr != nil {
		return nil, auditErr
	}

	result, toolErr := tool.Call(ctx, call.Args)

	rec.Event, rec.Result = EventResult, result
	if toolErr != nil {
		rec.Err = toolErr.Error()
		toolErr = fmt.Errorf("tool %s: %w", call.Name, toolErr)
	}
	return result, errors.Join(toolErr, g.record(ctx, rec))
}

// decide returns the tool to execute and the decision for call. The policy
// step belongs here, after the grant and resolve checks.
func (g *Gateway) decide(call ToolCall) (Tool, Decision, string) {
	if !g.granted[call.Name] {
		return nil, Deny, "tool not granted"
	}
	tool, ok := g.tools[call.Name]
	if !ok {
		return nil, Deny, "tool not resolved"
	}
	return tool, Allow, ""
}

func (g *Gateway) record(ctx context.Context, rec Record) error {
	if err := g.audit.Record(ctx, rec); err != nil {
		return fmt.Errorf("%w: %s %s: %w", ErrAudit, rec.Event, rec.Tool, err)
	}
	return nil
}
