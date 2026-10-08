package toolgateway

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
)

// Policy decides on granted tool calls. A Policy can only tighten: it may
// deny or require approval, never grant a tool.
type Policy interface {
	Evaluate(ctx context.Context, req Request) (Verdict, error)
}

// Request describes one granted call to whoever decides on it: policy, and an
// approver when policy asks for one. Its JSON form is the input document
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

// Approver answers calls that policy marks as requiring approval. It sees the
// same request as policy, and the reasons policy gave.
type Approver interface {
	Approve(ctx context.Context, req Request, reasons []string) (Approval, error)
}

// ErrSuspend is returned by an Approver that keeps a request to be answered
// later instead of waiting for the answer: the run stops at the call, which
// Run.Resume runs once it is answered.
var ErrSuspend = errors.New("toolgateway: suspend the run for approval")

// Suspended is the error Call returns for a call whose approver suspended the
// run. Request and Reasons are redacted, as the approver saw them.
type Suspended struct {
	// CallID identifies the call in the audit log; Resume takes it.
	CallID  string
	Request Request
	Reasons []string
}

func (s *Suspended) Error() string {
	return "tool " + s.Request.Tool + ": waiting for approval"
}

// Approval is an approver's answer.
type Approval struct {
	Approved bool
	// Approver names who decided.
	Approver string
	Reason   string
}
