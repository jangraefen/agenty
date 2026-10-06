package toolgateway

import (
	"context"
	"encoding/json"
)

// Decision is the gateway's verdict on a tool call.
type Decision string

const (
	Allow           Decision = "allow"
	Deny            Decision = "deny"
	RequireApproval Decision = "require_approval"
)

// Event says which step of a call a Record describes.
type Event string

const (
	// EventDecision is recorded for every call, before anything executes.
	EventDecision Event = "decision"
	// EventApproval is recorded when policy required approval, before anything
	// executes.
	EventApproval Event = "approval"
	// EventResult is recorded after an allowed call has executed.
	EventResult Event = "result"
)

// Record is one audit entry. All records of one call share its CallID, and
// all records of one run share its RunID. Its JSON form is the audit log format.
type Record struct {
	RunID    string          `json:"run_id"`
	CallID   string          `json:"call_id"`
	Event    Event           `json:"event"`
	Tool     string          `json:"tool"`
	Args     json.RawMessage `json:"args,omitempty"`
	Decision Decision        `json:"decision"`
	Reason   string          `json:"reason,omitempty"`
	// Approver names who approved or rejected the call, if anyone did.
	Approver string          `json:"approver,omitempty"`
	Result   json.RawMessage `json:"result,omitempty"`
	Err      string          `json:"error,omitempty"`
}

// Audit stores audit records. If recording a decision or approval fails, the
// gateway does not execute the call; if recording a result fails, Call reports
// the error.
type Audit interface {
	Record(ctx context.Context, rec Record) error
}
