package toolgateway

import (
	"context"
	"encoding/json"
	"maps"
)

// Policy decides on granted tool calls. A Policy can only tighten: it may
// deny or require approval, never grant a tool.
type Policy interface {
	Evaluate(ctx context.Context, req Request) (Verdict, error)
}

// Request describes one granted call to whoever decides on it: policy, and a
// person when policy asks for their approval. Its JSON form is the input document
// policy is written against.
type Request struct {
	RunID   string          `json:"run_id"`
	Harness string          `json:"harness"`
	Tool    string          `json:"tool"`
	Args    json.RawMessage `json:"args"`
	// Calls counts the tool calls the run executed before this one.
	Calls CallCounts `json:"calls"`
}

// CallCounts counts executed tool calls, in total and per tool.
type CallCounts struct {
	Total  int            `json:"total"`
	ByTool map[string]int `json:"by_tool"`
}

func (c CallCounts) clone() CallCounts {
	c.ByTool = maps.Clone(c.ByTool)
	return c
}

// Verdict is a policy's decision on a call and the reasons for it.
type Verdict struct {
	Decision Decision
	Reasons  []string
}

// Suspended is the error Call returns for a call that policy marks as
// requiring approval: the run stops at the call, which Resume runs once a
// person has answered it. Request and Reasons are redacted, as the approver
// sees them.
type Suspended struct {
	// CallID identifies the call in the audit log; Resume takes it.
	CallID  string
	Request Request
	Reasons []string
}

func (s *Suspended) Error() string {
	return "tool " + s.Request.Tool + ": waiting for approval"
}

// Approval is a person's answer to a call that waited for approval.
type Approval struct {
	Approved bool
	// Approver names who decided.
	Approver string
	Reason   string
}
