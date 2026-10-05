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
	Evaluate(ctx context.Context, req Request) (Verdict, error)
}

// Request describes one granted, resolved call to whoever decides on it:
// policy, and an approver when policy asks for one. Its JSON form is the
// input document policy is written against.
type Request struct {
	RunID   string          `json:"run_id"`
	Harness string          `json:"harness"`
	Tool    string          `json:"tool"`
	Effect  Effect          `json:"effect"`
	Args    json.RawMessage `json:"args"`
	// Calls counts the tool calls the run executed before this one.
	Calls CallCounts `json:"calls"`
}

// CallCounts counts executed tool calls, in total, per tool and per effect.
type CallCounts struct {
	Total    int            `json:"total"`
	ByTool   map[string]int `json:"by_tool"`
	ByEffect map[Effect]int `json:"by_effect"`
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

// Approver answers calls that policy marks as requiring approval. It sees the
// same request as policy, and the reasons policy gave.
type Approver interface {
	Approve(ctx context.Context, req Request, reasons []string) (Approval, error)
}

// Approval is an approver's answer.
type Approval struct {
	Approved bool
	// Approver names who decided.
	Approver string
	Reason   string
}
