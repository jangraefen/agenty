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
// all records of one run share its RunID.
type Record struct {
	RunID    string
	CallID   string
	Event    Event
	Tool     string
	Args     json.RawMessage
	Decision Decision
	Reason   string
	// Approver names who approved or rejected the call, if anyone did.
	Approver string
	Result   json.RawMessage
	Err      string
}

// Audit stores audit records. If recording a decision or approval fails, the
// gateway does not execute the call; if recording a result fails, Call reports
// the error.
type Audit interface {
	Record(ctx context.Context, rec Record) error
}
