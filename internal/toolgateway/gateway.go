// Package toolgateway is the single path every tool call takes.
//
// For each call the gateway checks the grant, resolves the tool, enforces the
// run's tool call limit, asks policy, asks an approver when policy requires
// one, and records each decision before it executes the tool and records the
// result. A call is never executed unless its decision and approval have been
// recorded, and anything short of a clear allow is a denial.
package toolgateway

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

// ErrDenied is returned for every call the gateway refuses to execute.
var ErrDenied = errors.New("tool call denied")

// ErrAudit is returned when a decision, approval or result cannot be recorded.
var ErrAudit = errors.New("audit record failed")

// Tool executes one typed capability. Only the gateway calls it.
type Tool interface {
	Definition() Definition
	Call(ctx context.Context, args json.RawMessage) (json.RawMessage, error)
}

// Definition describes a tool to the model and to policy.
type Definition struct {
	Name        string
	Description string
	// InputSchema is the JSON Schema of the tool arguments.
	InputSchema json.RawMessage
	Effect      Effect
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
	// Harness names the harness of the run, for policy.
	Harness string
	// Granted names the tools the harness may call. Anything else is denied.
	Granted []string
	// Tools are the executors the gateway can resolve calls to.
	Tools []Tool
	// MaxToolCalls bounds the calls of the run, denied ones included. It is
	// required.
	MaxToolCalls int
	// Policy decides on granted, resolved calls. It is required.
	Policy Policy
	// Approver answers calls that policy marks as requiring approval. Without
	// one, such calls are denied.
	Approver Approver
	// Audit records every decision, approval and result. It is required.
	Audit Audit
}

// Gateway checks, executes and records tool calls.
type Gateway struct {
	runID        string
	harness      string
	granted      map[string]bool
	tools        map[string]Tool
	effects      map[string]Effect
	defs         []Definition
	maxToolCalls int
	policy       Policy
	approver     Approver
	audit        Audit

	mu       sync.Mutex
	attempts int
	executed CallCounts
}

// New returns a Gateway for cfg. The grants are copied, so later changes to
// cfg do not affect the gateway.
func New(cfg Config) (*Gateway, error) {
	switch {
	case cfg.RunID == "":
		return nil, errors.New("toolgateway: run id is required")
	case cfg.Audit == nil:
		return nil, errors.New("toolgateway: audit is required")
	case cfg.Policy == nil:
		return nil, errors.New("toolgateway: policy is required")
	case cfg.MaxToolCalls <= 0:
		return nil, errors.New("toolgateway: max tool calls must be greater than 0")
	}
	g := &Gateway{
		runID:        cfg.RunID,
		harness:      cfg.Harness,
		granted:      make(map[string]bool, len(cfg.Granted)),
		tools:        make(map[string]Tool, len(cfg.Tools)),
		effects:      make(map[string]Effect, len(cfg.Tools)),
		maxToolCalls: cfg.MaxToolCalls,
		policy:       cfg.Policy,
		approver:     cfg.Approver,
		audit:        cfg.Audit,
		executed:     CallCounts{ByTool: map[string]int{}, ByEffect: map[Effect]int{}},
	}
	for i, name := range cfg.Granted {
		if name == "" {
			return nil, fmt.Errorf("toolgateway: grant %d is empty", i)
		}
		g.granted[name] = true
	}
	if err := ValidateTools(cfg.Tools); err != nil {
		return nil, err
	}
	for _, tool := range cfg.Tools {
		def := tool.Definition()
		g.tools[def.Name] = tool
		g.effects[def.Name] = def.Effect
		if g.granted[def.Name] {
			g.defs = append(g.defs, def)
		}
	}
	slices.SortFunc(g.defs, func(a, b Definition) int { return strings.Compare(a.Name, b.Name) })
	return g, nil
}

// ValidateTools checks that every tool is non-nil and has a unique, non-empty
// name and a known effect. New applies the same check.
func ValidateTools(tools []Tool) error {
	seen := make(map[string]bool, len(tools))
	for i, tool := range tools {
		if tool == nil {
			return fmt.Errorf("toolgateway: tool %d is nil", i)
		}
		def := tool.Definition()
		switch {
		case def.Name == "":
			return fmt.Errorf("toolgateway: tool %d has no name", i)
		case seen[def.Name]:
			return fmt.Errorf("toolgateway: duplicate tool %q", def.Name)
		case def.Effect != EffectRead && def.Effect != EffectWrite:
			return fmt.Errorf("toolgateway: tool %q has invalid effect %q", def.Name, def.Effect)
		}
		seen[def.Name] = true
	}
	return nil
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
	tool, verdict := g.decide(ctx, call)
	rec.Decision, rec.Reason = verdict.Decision, verdict.Reason

	auditErr := g.record(ctx, rec)
	if rec.Decision == Deny {
		return nil, errors.Join(denied(call.Name, rec.Reason), auditErr)
	}
	if auditErr != nil {
		return nil, auditErr
	}

	if rec.Decision == RequireApproval {
		approval, err := g.approve(ctx, call, verdict.Reasons)
		rec.Event, rec.Approver = EventApproval, approval.Approver
		rec.Decision, rec.Reason = Allow, approval.Reason
		switch {
		case err != nil:
			rec.Decision, rec.Reason = Deny, "approval failed: "+err.Error()
		case !approval.Approved:
			rec.Decision, rec.Reason = Deny, "approval rejected: "+approval.Reason
		}
		auditErr := g.record(ctx, rec)
		if rec.Decision == Deny {
			return nil, errors.Join(denied(call.Name, rec.Reason), auditErr)
		}
		if auditErr != nil {
			return nil, auditErr
		}
	}

	g.countExecuted(call.Name)
	result, toolErr := tool.Call(ctx, call.Args)

	rec.Event, rec.Result = EventResult, result
	if toolErr != nil {
		rec.Err = toolErr.Error()
		toolErr = fmt.Errorf("tool %s: %w", call.Name, toolErr)
	}
	return result, errors.Join(toolErr, g.record(ctx, rec))
}

// decision is the outcome of decide: the gateway's decision, its reason, and
// the policy reasons behind it.
type decision struct {
	Decision Decision
	Reason   string
	Reasons  []string
}

// decide returns the tool to execute and the decision for call. Grant and
// resolve come first, so policy only ever sees granted, resolvable calls and
// cannot grant anything. Every attempt counts towards the call limit.
func (g *Gateway) decide(ctx context.Context, call ToolCall) (Tool, decision) {
	g.mu.Lock()
	g.attempts++
	attempts := g.attempts
	counts := g.executed.clone()
	g.mu.Unlock()

	if !g.granted[call.Name] {
		return nil, deny("tool not granted")
	}
	tool, ok := g.tools[call.Name]
	if !ok {
		return nil, deny("tool not resolved")
	}
	if attempts > g.maxToolCalls {
		return nil, deny("tool call limit reached")
	}

	verdict, err := g.policy.Evaluate(ctx, PolicyInput{
		RunID:   g.runID,
		Harness: g.harness,
		Tool:    call.Name,
		Effect:  g.effects[call.Name],
		Args:    call.Args,
		Calls:   counts,
	})
	if err != nil {
		return nil, deny("policy error: " + err.Error())
	}
	reasons := strings.Join(verdict.Reasons, "; ")
	switch verdict.Decision {
	case Allow:
		return tool, decision{Decision: Allow}
	case Deny:
		return nil, deny("policy: " + reasons)
	case RequireApproval:
		if g.approver == nil {
			return nil, deny("no approver for required approval: " + reasons)
		}
		return tool, decision{Decision: RequireApproval, Reason: "policy: " + reasons, Reasons: verdict.Reasons}
	default:
		return nil, deny(fmt.Sprintf("invalid policy decision %q", verdict.Decision))
	}
}

func (g *Gateway) approve(ctx context.Context, call ToolCall, reasons []string) (Approval, error) {
	return g.approver.Approve(ctx, ApprovalRequest{
		RunID:   g.runID,
		Tool:    call.Name,
		Effect:  g.effects[call.Name],
		Args:    call.Args,
		Reasons: reasons,
	})
}

func (g *Gateway) countExecuted(name string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.executed.Total++
	g.executed.ByTool[name]++
	g.executed.ByEffect[g.effects[name]]++
}

func (g *Gateway) record(ctx context.Context, rec Record) error {
	if err := g.audit.Record(ctx, rec); err != nil {
		return fmt.Errorf("%w: %s %s: %w", ErrAudit, rec.Event, rec.Tool, err)
	}
	return nil
}

func deny(reason string) decision {
	return decision{Decision: Deny, Reason: reason}
}

func denied(tool, reason string) error {
	return fmt.Errorf("%w: %s: %s", ErrDenied, tool, reason)
}
