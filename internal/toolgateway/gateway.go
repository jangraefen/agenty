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
	"regexp"
	"slices"
	"strings"
	"sync"
)

var toolName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// ValidateToolName checks that name works with model provider APIs, which
// allow 1 to 64 letters, digits, underscores and hyphens. Every tool and every
// grant uses such a name, so the same name appears in the harness, the audit
// log and the model request. Server-backed tools are named "<server>_<tool>".
func ValidateToolName(name string) error {
	if !toolName.MatchString(name) {
		return fmt.Errorf("tool name %q must be 1 to 64 letters, digits, underscores or hyphens", name)
	}
	return nil
}

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
	// Secrets are credential values, at least 8 characters long, that must
	// never reach the model or the audit log. The gateway replaces them with
	// "[redacted]" in tool results, tool errors, denial reasons and audit
	// records. Tools still receive their arguments unchanged.
	Secrets []string
}

// Gateway checks, executes and records tool calls.
type Gateway struct {
	runID        string
	harness      string
	granted      map[string]bool
	tools        map[string]Tool
	defs         []Definition
	maxToolCalls int
	policy       Policy
	approver     Approver
	audit        Audit
	redact       *redactor

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
		maxToolCalls: cfg.MaxToolCalls,
		policy:       cfg.Policy,
		approver:     cfg.Approver,
		audit:        cfg.Audit,
		executed:     CallCounts{ByTool: map[string]int{}},
	}
	for i, name := range cfg.Granted {
		if err := ValidateToolName(name); err != nil {
			return nil, fmt.Errorf("toolgateway: grant %d: %w", i, err)
		}
		g.granted[name] = true
	}
	if err := ValidateTools(cfg.Tools); err != nil {
		return nil, err
	}
	redact, err := newRedactor(cfg.Secrets)
	if err != nil {
		return nil, err
	}
	g.redact = redact
	for _, tool := range cfg.Tools {
		def := tool.Definition()
		g.tools[def.Name] = tool
		if g.granted[def.Name] {
			g.defs = append(g.defs, def)
		}
	}
	slices.SortFunc(g.defs, func(a, b Definition) int { return strings.Compare(a.Name, b.Name) })
	return g, nil
}

// ValidateTools checks that every tool is non-nil and has a unique, valid
// name. New applies the same check.
func ValidateTools(tools []Tool) error {
	seen := make(map[string]bool, len(tools))
	for i, tool := range tools {
		if tool == nil {
			return fmt.Errorf("toolgateway: tool %d is nil", i)
		}
		def := tool.Definition()
		if err := ValidateToolName(def.Name); err != nil {
			return fmt.Errorf("toolgateway: tool %d: %w", i, err)
		}
		if seen[def.Name] {
			return fmt.Errorf("toolgateway: duplicate tool %q", def.Name)
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
		return nil, errors.Join(denied(call.Name, g.redact.string(rec.Reason)), auditErr)
	}
	if auditErr != nil {
		return nil, auditErr
	}

	if rec.Decision == RequireApproval {
		approval, err := g.approver.Approve(ctx, verdict.Request, verdict.Reasons)
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
			return nil, errors.Join(denied(call.Name, g.redact.string(rec.Reason)), auditErr)
		}
		if auditErr != nil {
			return nil, auditErr
		}
	}

	g.countExecuted(call.Name)
	result, toolErr := tool.Call(ctx, call.Args)
	result, toolErr = g.redact.json(result), g.redact.error(toolErr)

	rec.Event, rec.Result = EventResult, result
	if toolErr != nil {
		rec.Err = toolErr.Error()
		toolErr = fmt.Errorf("tool %s: %w", call.Name, toolErr)
	}
	return result, errors.Join(toolErr, g.record(ctx, rec))
}

// decision is the outcome of decide: the gateway's decision, its reason, and
// for calls that reached policy, the request and the policy reasons.
type decision struct {
	Decision Decision
	Reason   string
	Request  Request
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

	req := Request{
		RunID:   g.runID,
		Harness: g.harness,
		Tool:    call.Name,
		Args:    call.Args,
		Calls:   counts,
	}
	verdict, err := g.policy.Evaluate(ctx, req)
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
		return tool, decision{Decision: RequireApproval, Reason: "policy: " + reasons, Request: req, Reasons: verdict.Reasons}
	default:
		return nil, deny(fmt.Sprintf("invalid policy decision %q", verdict.Decision))
	}
}

func (g *Gateway) countExecuted(name string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.executed.Total++
	g.executed.ByTool[name]++
}

// record writes rec to the audit log, with secrets redacted.
func (g *Gateway) record(ctx context.Context, rec Record) error {
	rec.Args, rec.Result = g.redact.json(rec.Args), g.redact.json(rec.Result)
	rec.Reason, rec.Err, rec.Approver = g.redact.string(rec.Reason), g.redact.string(rec.Err), g.redact.string(rec.Approver)
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
