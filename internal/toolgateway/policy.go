package toolgateway

import (
	"context"
	"encoding/json"
	"maps"
)

// Policy decides on granted tool calls. A Policy can only tighten: it may
// deny or require approval, never grant a tool (guarantee 4). The gateway
// asks it only after the grant and limit checks pass, and denies the call on
// any error. internal/policy implements it with OPA; the interface keeps OPA
// out of this package and lets tests use gatewaytest.Policy.
type Policy interface {
	Evaluate(ctx context.Context, req Request) (Verdict, error)
}

// Request describes one granted call to whoever decides on it: policy, and a
// person when policy asks for their approval. Its JSON form is the input document
// policy is written against.
//
// Args are the model's arguments as given, unredacted, because policy must
// see what the tool would receive; a copy shown to a person is redacted (see
// Suspended).
type Request struct {
	RunID   string          `json:"run_id"`
	Harness string          `json:"harness"`
	Tool    string          `json:"tool"`
	Args    json.RawMessage `json:"args"`
	// Calls counts the tool calls the run executed before this one.
	Calls CallCounts `json:"calls"`
}

// CallCounts counts executed tool calls, in total and per tool, so policy can
// write rate-style rules such as "at most three deletions per run".
type CallCounts struct {
	Total  int            `json:"total"`
	ByTool map[string]int `json:"by_tool"`
}

// clone copies c with its own ByTool map, so a Request handed to policy
// cannot see, or change, counts the gateway updates later.
func (c CallCounts) clone() CallCounts {
	c.ByTool = maps.Clone(c.ByTool)
	return c
}

// Verdict is a policy's decision on a call and the reasons for it. The
// reasons are recorded in the audit log and shown, redacted, to the model on
// a deny and to the approver on require_approval.
type Verdict struct {
	Decision Decision
	Reasons  []string
}

// Suspended is the error Call returns for a call that policy marks as
// requiring approval: the run stops at the call, which Resume runs once a
// person has answered it. Request and Reasons are redacted, as the approver
// sees them.
//
// It is an error, not a result, so a run that ignores it cannot mistake a
// waiting call for one that ran: the agent wraps it in its own Suspended
// error and the server stores an approval request from it.
type Suspended struct {
	// CallID identifies the call in the audit log; Resume takes it.
	CallID  string
	Request Request
	Reasons []string
}

// Error names the tool that waits. It holds no arguments, so the message is
// safe to log.
func (s *Suspended) Error() string {
	return "tool " + s.Request.Tool + ": waiting for approval"
}

// Approval is a person's answer to a call that waited for approval. Resume
// records it with the call; anything but Approved denies the call.
type Approval struct {
	Approved bool
	// Approver names who decided.
	Approver string
	Reason   string
}
