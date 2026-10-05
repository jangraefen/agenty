package toolgateway

import (
	"context"
	"encoding/json"
	"maps"
)

// Effect says what a tool does to the systems it reaches.
type Effect string

const (
	EffectRead  Effect = "read"
	EffectWrite Effect = "write"
)

// Policy decides on tool calls that are granted and resolved. A Policy can
// only tighten: it may deny or require approval, never grant a tool.
type Policy interface {
	Evaluate(ctx context.Context, in PolicyInput) (Verdict, error)
}

// PolicyInput is everything policy sees about one call.
type PolicyInput struct {
	RunID   string
	Harness string
	Tool    string
	Effect  Effect
	Args    json.RawMessage
	// Calls counts the tool calls the run executed before this one.
	Calls CallCounts
}

// CallCounts counts executed tool calls, in total, per tool and per effect.
type CallCounts struct {
	Total    int
	ByTool   map[string]int
	ByEffect map[Effect]int
}

func (c CallCounts) clone() CallCounts {
	c.ByTool = maps.Clone(c.ByTool)
	c.ByEffect = maps.Clone(c.ByEffect)
	return c
}

// Verdict is a policy's decision on a call and the reasons for it.
type Verdict struct {
	Decision Decision
	Reasons  []string
}

// Approver answers calls that policy marks as requiring approval.
type Approver interface {
	Approve(ctx context.Context, req ApprovalRequest) (Approval, error)
}

// ApprovalRequest is what an approver is asked to decide.
type ApprovalRequest struct {
	RunID   string
	Tool    string
	Effect  Effect
	Args    json.RawMessage
	Reasons []string
}

// Approval is an approver's answer.
type Approval struct {
	Approved bool
	// Approver names who decided.
	Approver string
	Reason   string
}
