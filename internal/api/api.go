// Package api holds the JSON types of Agenty's HTTP API, shared by the
// server and its clients.
//
// The API lives under /v1:
//
//	PUT  /v1/harnesses/{name}                   store a harness as its next version
//	GET  /v1/harnesses                          latest version of every harness
//	GET  /v1/harnesses/{name}                   latest version of one harness
//	POST /v1/runs                               start a run
//	GET  /v1/runs/{id}                          a run
//	GET  /v1/runs/{id}/audit                    a run's audit records
//	GET  /v1/runs/{id}/events                   a run's events, as server-sent events
//	POST /v1/runs/{id}/approvals/{approval}     answer an approval request
//
// Errors are returned as an Error with a 4xx or 5xx status.
package api

import (
	"encoding/json"
	"time"

	"github.com/jangraefen/agenty/internal/harness"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

// Error is the body of every error response.
type Error struct {
	Error string `json:"error"`
}

// HarnessVersion is one stored version of a harness.
type HarnessVersion struct {
	ID        int64           `json:"id"`
	Version   int             `json:"version"`
	Harness   harness.Harness `json:"harness"`
	CreatedAt time.Time       `json:"created_at"`
}

// CreateRun is the body of POST /v1/runs: the harness to run, by name, and
// the input to run it on. The run uses the harness's latest version.
type CreateRun struct {
	Harness string `json:"harness"`
	Input   string `json:"input"`
}

// Run statuses.
const (
	RunRunning   = "running"
	RunSucceeded = "succeeded"
	RunFailed    = "failed"
)

// Run is a run of a harness version.
type Run struct {
	ID               string     `json:"id"`
	HarnessVersionID int64      `json:"harness_version_id"`
	Input            string     `json:"input"`
	Status           string     `json:"status"`
	Output           string     `json:"output"`
	Steps            int        `json:"steps"`
	Error            string     `json:"error,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
}

// AuditRecord is a recorded audit record.
type AuditRecord struct {
	toolgateway.Record
	RecordedAt time.Time `json:"recorded_at"`
}

// Event names of the server-sent events of a run. Every event's data is JSON:
// a toolgateway.Record for EventAudit, an ApprovalRequest for EventApproval,
// and a Run for EventFinished, which is always the last event.
const (
	EventAudit    = "audit"
	EventApproval = "approval"
	EventFinished = "finished"
)

// ApprovalRequest asks a person to approve a call that policy marked as
// requiring approval. Secrets are already redacted from it.
type ApprovalRequest struct {
	// ID identifies the request when answering it.
	ID      string          `json:"id"`
	Harness string          `json:"harness"`
	Tool    string          `json:"tool"`
	Args    json.RawMessage `json:"args"`
	Reasons []string        `json:"reasons"`
}

// Answer is the body of an answer to an approval request.
type Answer struct {
	Approved bool `json:"approved"`
	// Approver names who answered. It is required and recorded in the audit
	// log. Until the API has sign-in, it is whatever the client says.
	Approver string `json:"approver"`
	Reason   string `json:"reason,omitempty"`
}
