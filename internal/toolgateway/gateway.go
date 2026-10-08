// Package toolgateway is the single path every tool call takes.
//
// A Gateway serves one run. For each call it checks the grant, enforces the
// run's tool call limit, asks policy, and records each decision before it
// executes the tool and records the result. A call that policy marks as
// requiring approval suspends the run instead: Call returns a *Suspended
// error, and Resume runs the call once a person has answered it, after
// recording the answer. A call is never executed unless its decision and
// approval have been recorded, and anything short of a clear allow is a
// denial.
//
// Every tool comes from a tool server, such as an MCP server. The gateway owns
// them: it starts those that serve a granted tool and stops them on Close,
// after which it denies every call. Stopping may only hand a server back to
// a Pool, which keeps it for the conversation's next run.
package toolgateway

import (
	"bytes"
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
	// RunID identifies the run, the gateway's only one, in audit records and
	// policy input. It is required.
	RunID string
	// Records is the audit log of a run that made calls before, such as one
	// resumed after it was suspended: its call counts are restored from them,
	// so the run's limit and policy see the calls it made before. Every call
	// with a decision is an attempt, denied ones included, and every call
	// with a result was executed. Records of other runs are ignored. A call
	// whose result could not be recorded ended its run, which is then not
	// resumed.
	Records []Record
	// Harness names the harness of the run, for policy.
	Harness string
	// Granted names the tools the harness may call. Anything else is denied.
	// Each must be a tool of a server in Servers, named "<server>_<tool>".
	Granted []string
	// Servers are tool servers, by name. The gateway starts those that serve
	// a granted tool, calls their tools, and stops them on Close.
	Servers map[string]ToolServer
	// MaxToolCalls bounds the calls of the run, denied ones included. It is
	// required.
	MaxToolCalls int
	// Policy decides on granted calls. It is required.
	Policy Policy
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

// Gateway is one run's path to the tools: its grants, tools, policy, audit
// and secrets, and the run's ID and call counts. Each run gets a gateway of
// its own, so runs never share counts, and one run's calls cannot affect
// another's limit or policy input.
type Gateway struct {
	id      string
	harness string
	// tools are the granted tools; every other call is denied.
	tools        map[string]Tool
	defs         []Definition
	maxToolCalls int
	policy       Policy
	audit        Audit
	redact       *secret.Redactor

	// mu guards what follows. A run's calls are sequential, but Close may
	// come at any time.
	mu       sync.Mutex
	sessions []namedSession
	// closed is set by Close.
	closed   bool
	attempts int
	executed CallCounts
}

// New returns the Gateway of the run cfg.RunID, after starting the servers
// that serve a granted tool. A grant that no server serves is an error. The
// grants are copied, so later changes to cfg do not affect the gateway.
// Close stops the servers.
func New(ctx context.Context, cfg Config) (*Gateway, error) {
	switch {
	case cfg.RunID == "":
		return nil, errors.New("toolgateway: run ID is required")
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
		id:           cfg.RunID,
		harness:      cfg.Harness,
		tools:        make(map[string]Tool, len(granted)),
		maxToolCalls: cfg.MaxToolCalls,
		policy:       cfg.Policy,
		audit:        cfg.Audit,
		redact:       cfg.Redactor,
		sessions:     sessions,
		executed:     CallCounts{ByTool: map[string]int{}},
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
	g.restore(cfg.Records)
	return g, nil
}

// restore restores the run's call counts from records, its audit log; see
// Config.Records.
func (g *Gateway) restore(records []Record) {
	attempted := map[string]bool{}
	for _, rec := range records {
		if rec.RunID != g.id {
			continue
		}
		switch rec.Event {
		case EventDecision:
			attempted[rec.CallID] = true
		case EventResult:
			g.countExecuted(rec.Tool)
		case EventApproval:
		}
	}
	g.attempts = len(attempted)
}

// ID identifies the run in audit records and policy input.
func (g *Gateway) ID() string {
	return g.id
}

// Close stops the gateway's servers and reports every one that failed to
// stop. Calls are denied afterwards. Closing again does nothing.
func (g *Gateway) Close() error {
	g.mu.Lock()
	sessions := g.sessions
	g.sessions, g.closed = nil, true
	g.mu.Unlock()
	return closeSessions(sessions)
}

// Definitions describes the tools the run may call: the granted ones, sorted
// by name.
func (g *Gateway) Definitions() []Definition {
	return slices.Clone(g.defs)
}

// Call runs one tool call through the gateway. A denied call returns an
// error wrapping ErrDenied. A failure to record returns an error wrapping
// ErrAudit; if only the result could not be recorded, the result is returned
// with that error. A call that policy marks as requiring approval does not
// run: it returns a *Suspended error, and the run stops at the call until it
// is answered, when Resume runs it, or not.
func (g *Gateway) Call(ctx context.Context, call ToolCall) (json.RawMessage, error) {
	rec := Record{
		RunID:  g.id,
		CallID: rand.Text(),
		Event:  EventDecision,
		Tool:   call.Name,
		Args:   call.Args,
	}
	tool, verdict := g.decide(ctx, call, true)
	if err := g.recordDecision(ctx, &rec, verdict); err != nil {
		return nil, err
	}
	if rec.Decision == RequireApproval {
		// The approver is a person: show them the call, never a secret.
		req := verdict.Request
		req.Args = g.redact.JSON(req.Args)
		reasons := make([]string, len(verdict.Reasons))
		for i, reason := range verdict.Reasons {
			reasons[i] = g.redact.String(reason)
		}
		var err error
		switch {
		case ctx.Err() != nil:
			// A cancelled run is not suspended: it ends.
			err = context.Cause(ctx)
		case !bytes.Equal(req.Args, call.Args):
			// A suspended call runs later as it is stored, redacted, so one
			// whose arguments hold a secret could not run as it was asked.
			err = errHoldsSecret
		default:
			return nil, &Suspended{CallID: rec.CallID, Request: req, Reasons: reasons}
		}
		return nil, g.recordAnswer(ctx, &rec, Approval{}, err)
	}
	return g.execute(ctx, rec, tool, call)
}

// errHoldsSecret fails the approval of a call that cannot wait for it.
var errHoldsSecret = errors.New("its arguments hold a secret, so it cannot wait for approval")

// Resume runs the call a Call suspended, identified by callID, with the
// answer a. It decides the call again, so policy as it is now applies, but
// does not count it as another attempt, and records it under its own ID.
// An answered call is no longer waiting for approval: approved, it runs
// unless policy now denies it; rejected, it is denied.
func (g *Gateway) Resume(ctx context.Context, callID string, call ToolCall, a Approval) (json.RawMessage, error) {
	rec := Record{
		RunID:  g.id,
		CallID: callID,
		Event:  EventDecision,
		Tool:   call.Name,
		Args:   call.Args,
	}
	tool, verdict := g.decide(ctx, call, false)
	if err := g.recordDecision(ctx, &rec, verdict); err != nil {
		return nil, err
	}
	if err := g.recordAnswer(ctx, &rec, a, nil); err != nil {
		return nil, err
	}
	return g.execute(ctx, rec, tool, call)
}

// recordDecision records the decision on the call rec describes. It returns
// an error wrapping ErrDenied for a denied call, and one wrapping ErrAudit if
// the decision could not be recorded.
func (g *Gateway) recordDecision(ctx context.Context, rec *Record, verdict decision) error {
	rec.Decision, rec.Reason = verdict.Decision, verdict.Reason
	auditErr := g.record(ctx, *rec)
	if rec.Decision == Deny {
		return errors.Join(denied(rec.Tool, g.redact.String(rec.Reason)), auditErr)
	}
	return auditErr
}

// recordAnswer records the answer to the call rec describes, or err, why
// the call could not wait for one. Like recordDecision, it returns an error
// for a call that does not go ahead.
func (g *Gateway) recordAnswer(ctx context.Context, rec *Record, approval Approval, err error) error {
	rec.Event, rec.Approver = EventApproval, approval.Approver
	rec.Decision, rec.Reason = Allow, approval.Reason
	switch {
	case err != nil:
		rec.Decision, rec.Reason = Deny, "approval failed: "+err.Error()
	case ctx.Err() != nil:
		// The run was cancelled while it waited: cancellation wins over
		// an approval that arrived at the same moment.
		rec.Decision, rec.Reason = Deny, "approval failed: "+context.Cause(ctx).Error()
	case !approval.Approved:
		rec.Decision, rec.Reason = Deny, "approval rejected: "+approval.Reason
	}
	auditErr := g.record(ctx, *rec)
	if rec.Decision == Deny {
		return errors.Join(denied(rec.Tool, g.redact.String(rec.Reason)), auditErr)
	}
	return auditErr
}

// execute runs an allowed call and records its result.
func (g *Gateway) execute(ctx context.Context, rec Record, tool Tool, call ToolCall) (json.RawMessage, error) {
	g.countExecuted(call.Name)
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
// anything. Every attempt counts towards the run's call limit; deciding a
// resumed call again is not another attempt.
func (g *Gateway) decide(ctx context.Context, call ToolCall, attempt bool) (Tool, decision) {
	g.mu.Lock()
	if attempt {
		g.attempts++
	}
	attempts, counts, closed := g.attempts, g.executed.clone(), g.closed
	g.mu.Unlock()
	if closed {
		return nil, deny("the run's tool servers were stopped")
	}
	tool, ok := g.tools[call.Name]
	if !ok {
		return nil, deny("tool not granted")
	}
	if attempts > g.maxToolCalls {
		return nil, deny("tool call limit reached")
	}

	req := Request{
		RunID:   g.id,
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
