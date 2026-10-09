package toolgateway

import (
	"context"
	"encoding/json"
)

// Decision is the gateway's verdict on a tool call. Its values are the ones
// policy, the audit log and the API use, so the same word means the same
// thing everywhere.
type Decision string

// The three decisions. Policy layers combine to the strictest one,
// deny > require_approval > allow (guarantee 4).
const (
	Allow           Decision = "allow"
	Deny            Decision = "deny"
	RequireApproval Decision = "require_approval"
)

// Event says which step of a call a Record describes.
type Event string

// The events of a call, in the order they are recorded. The decision, and the
// approval where policy required one, are written before the tool runs, so a
// call that executed always has them.
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
//
// The gateway redacts every free-text field before a Record reaches Audit, so
// a store never holds a credential (guarantee 5). Records also serve as the
// run's memory: New restores a resumed run's call counts from them.
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
//
// The server's implementation stores records in PostgreSQL, where they are
// append-only and hash-chained (guarantee 9), and publishes each one to the
// run's subscribers. It must not return until the record is durable, as the
// gateway executes the call as soon as it returns.
type Audit interface {
	Record(ctx context.Context, rec Record) error
}
