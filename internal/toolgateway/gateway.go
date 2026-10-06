// Package toolgateway is the single path every tool call takes.
//
// For each call the gateway checks the grant, enforces the run's tool call
// limit, asks policy, asks an approver when policy requires one, and records
// each decision before it executes the tool and records the result. A call is
// never executed unless its decision and approval have been recorded, and
// anything short of a clear allow is a denial.
//
// Every tool comes from a tool server, such as an MCP server. The gateway owns
// them: it starts those that serve a granted tool and stops them on Close.
package toolgateway

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/jangraefen/agenty/internal/secret"
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
	// Harness names the harness of the run, for policy.
	Harness string
	// Granted names the tools the harness may call. Anything else is denied.
	// Each must be a tool of a server in Servers, named "<server>_<tool>".
	Granted []string
	// Servers are tool servers, by name. The gateway starts those that serve
	// a granted tool, calls their tools, and stops them on Close.
	Servers map[string]ToolServer
	// MaxToolCalls bounds the calls of each run, denied ones included. It is
	// required.
	MaxToolCalls int
	// Policy decides on granted calls. It is required.
	Policy Policy
	// Approver answers calls that policy marks as requiring approval. Without
	// one, such calls are denied.
	Approver Approver
	// Audit records every decision, approval and result. It is required.
	Audit Audit
	// Redactor holds the credentials that must never reach the model, an
	// approver or the audit log. The gateway redacts them from tool results,
	// tool errors, denial reasons, approval requests and audit records; tools
	// still receive their arguments unchanged. It is required, so redaction
	// is never left out by omission: without secrets, pass one built from
	// none.
	Redactor *secret.Redactor
}

// Gateway holds what every run of a harness shares: grants, tools, policy,
// approver, audit and secrets. It is built and validated once; each run then
// starts with Start and calls tools through the returned Run.
type Gateway struct {
	harness string
	// tools are the granted tools; every other call is denied.
	tools        map[string]Tool
	defs         []Definition
	maxToolCalls int
	policy       Policy
	approver     Approver
	audit        Audit
	redact       *secret.Redactor

	mu       sync.Mutex
	sessions []namedSession
}

// New returns a Gateway for cfg, after starting the servers that serve a
// granted tool. A grant that no server serves is an error. The grants are
// copied, so later changes to cfg do not affect the gateway. Close stops the
// servers.
func New(ctx context.Context, cfg Config) (*Gateway, error) {
	switch {
	case cfg.Audit == nil:
		return nil, errors.New("toolgateway: audit is required")
	case cfg.Policy == nil:
		return nil, errors.New("toolgateway: policy is required")
	case cfg.MaxToolCalls <= 0:
		return nil, errors.New("toolgateway: max tool calls must be greater than 0")
	case cfg.Redactor == nil:
		return nil, errors.New("toolgateway: redactor is required")
	}
	if err := validateServers(cfg.Servers); err != nil {
		return nil, err
	}
	granted := make(map[string]bool, len(cfg.Granted))
	for i, name := range cfg.Granted {
		if err := ValidateToolName(name); err != nil {
			return nil, fmt.Errorf("toolgateway: grant %d: %w", i, err)
		}
		if server, _, _ := strings.Cut(name, "_"); cfg.Servers[server] == nil {
			return nil, fmt.Errorf("toolgateway: grant %s: no server %q is configured", name, server)
		}
		granted[name] = true
	}

	sessions, served, err := startServers(ctx, cfg.Servers, granted)
	if err != nil {
		return nil, err
	}
	g := &Gateway{
		harness:      cfg.Harness,
		tools:        make(map[string]Tool, len(granted)),
		maxToolCalls: cfg.MaxToolCalls,
		policy:       cfg.Policy,
		approver:     cfg.Approver,
		audit:        cfg.Audit,
		redact:       cfg.Redactor,
		sessions:     sessions,
	}
	for _, tool := range served {
		if def := tool.Definition(); granted[def.Name] {
			g.tools[def.Name] = tool
			g.defs = append(g.defs, def)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(granted)) {
		if g.tools[name] == nil {
			server, _, _ := strings.Cut(name, "_")
			return nil, errors.Join(fmt.Errorf("toolgateway: grant %s: server %s has no such tool", name, server), closeSessions(sessions))
		}
	}
	slices.SortFunc(g.defs, func(a, b Definition) int { return strings.Compare(a.Name, b.Name) })
	return g, nil
}

// Close stops the gateway's servers and reports every one that failed to
// stop. Calls to their tools fail afterwards. Closing again does nothing.
func (g *Gateway) Close() error {
	g.mu.Lock()
	sessions := g.sessions
	g.sessions = nil
	g.mu.Unlock()
	return closeSessions(sessions)
}

// Definitions describes the tools runs may call: the granted ones, sorted by
// name.
func (g *Gateway) Definitions() []Definition {
	return slices.Clone(g.defs)
}

// Start begins a run: it mints the run's ID, which every audit record of the
// run carries, and starts the run's call counts at zero. Runs never share
// counts, so one run's calls cannot affect another's limit or policy input.
func (g *Gateway) Start() *Run {
	return &Run{
		gateway:  g,
		id:       rand.Text(),
		executed: CallCounts{ByTool: map[string]int{}},
	}
}

// Run is one run's path to the tools: its ID and its call counts.
type Run struct {
	gateway *Gateway
	id      string

	mu       sync.Mutex
	attempts int
	executed CallCounts
}

// ID identifies the run in audit records and policy input.
func (r *Run) ID() string {
	return r.id
}

// Call runs one tool call through the gateway. A denied call returns an
// error wrapping ErrDenied. A failure to record returns an error wrapping
// ErrAudit; if only the result could not be recorded, the result is returned
// with that error.
func (r *Run) Call(ctx context.Context, call ToolCall) (json.RawMessage, error) {
	g := r.gateway
	rec := Record{
		RunID:  r.id,
		CallID: rand.Text(),
		Event:  EventDecision,
		Tool:   call.Name,
		Args:   call.Args,
	}
	tool, verdict := r.decide(ctx, call)
	rec.Decision, rec.Reason = verdict.Decision, verdict.Reason

	auditErr := g.record(ctx, rec)
	if rec.Decision == Deny {
		return nil, errors.Join(denied(call.Name, g.redact.String(rec.Reason)), auditErr)
	}
	if auditErr != nil {
		return nil, auditErr
	}

	if rec.Decision == RequireApproval {
		// The approver is a person: show them the call, never a secret.
		req := verdict.Request
		req.Args = g.redact.JSON(req.Args)
		reasons := make([]string, len(verdict.Reasons))
		for i, reason := range verdict.Reasons {
			reasons[i] = g.redact.String(reason)
		}
		approval, err := g.approver.Approve(ctx, req, reasons)
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
			return nil, errors.Join(denied(call.Name, g.redact.String(rec.Reason)), auditErr)
		}
		if auditErr != nil {
			return nil, auditErr
		}
	}

	r.countExecuted(call.Name)
	result, toolErr := tool.Call(ctx, call.Args)
	result, toolErr = g.redact.JSON(result), g.redact.Error(toolErr)

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

// decide returns the tool to execute and the decision for call. The grant
// comes first, so policy only ever sees granted calls and cannot grant
// anything. Every attempt counts towards the run's call limit.
func (r *Run) decide(ctx context.Context, call ToolCall) (Tool, decision) {
	g := r.gateway
	r.mu.Lock()
	r.attempts++
	attempts := r.attempts
	counts := r.executed.clone()
	r.mu.Unlock()

	tool, ok := g.tools[call.Name]
	if !ok {
		return nil, deny("tool not granted")
	}
	if attempts > g.maxToolCalls {
		return nil, deny("tool call limit reached")
	}

	req := Request{
		RunID:   r.id,
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

func (r *Run) countExecuted(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.executed.Total++
	r.executed.ByTool[name]++
}

// record writes rec to the audit log, with secrets redacted.
func (g *Gateway) record(ctx context.Context, rec Record) error {
	rec.Args, rec.Result = g.redact.JSON(rec.Args), g.redact.JSON(rec.Result)
	rec.Reason, rec.Err, rec.Approver = g.redact.String(rec.Reason), g.redact.String(rec.Err), g.redact.String(rec.Approver)
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
