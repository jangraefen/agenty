// Package api holds the JSON types of Agenty's HTTP API, shared by the
// server and its clients.
//
// The API lives under /v1. Every request signs in with a user's bearer token,
// "Authorization: Bearer TOKEN". Harnesses and runs live in workspaces, under
// /v1/workspaces/{ws}; a workspace the user is not a member of is not found.
//
//	GET  /v1/me                                     the signed-in user and their workspaces
//	PUT  {ws}/harnesses/{name}                      store a harness as its next version
//	GET  {ws}/harnesses                             latest version of every harness
//	GET  {ws}/harnesses/{name}                      latest version of one harness
//	POST {ws}/runs                                  start a run
//	GET  {ws}/runs                                  the runs, newest first; see DefaultRunsLimit
//	GET  {ws}/runs/{id}                             a run
//	POST {ws}/runs/{id}/cancel                      cancel a running run
//	GET  {ws}/runs/{id}/audit                       a run's audit records
//	GET  {ws}/runs/{id}/transcript                  a run's conversation with the model
//	GET  {ws}/runs/{id}/events                      a run's events, as server-sent events
//	POST {ws}/runs/{id}/approvals/{approval}        answer an approval request
//	GET  {ws}/approvals                             the approval requests waiting for an answer
//
// Errors are returned as an Error with a 4xx or 5xx status: 401 without a
// valid token, 404 for anything not found, workspaces included.
package api

import (
	"encoding/json"
	"time"

	"github.com/jangraefen/agenty/internal/harness"
	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

// Error is the body of every error response.
type Error struct {
	Error string `json:"error"`
}

// Me is the signed-in user, and the workspaces they are a member of, sorted.
type Me struct {
	User       string   `json:"user"`
	Workspaces []string `json:"workspaces"`
}

// HarnessVersion is one stored version of a harness.
type HarnessVersion struct {
	ID        int64           `json:"id"`
	Version   int             `json:"version"`
	Harness   harness.Harness `json:"harness"`
	CreatedAt time.Time       `json:"created_at"`
}

// CreateRun is the body of POST {ws}/runs: the harness to run, by name, and
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
	// RunCancelled is a run a user cancelled.
	RunCancelled = "cancelled"
)

// GET {ws}/runs takes optional query parameters: harness and status select
// the runs of one harness or with one status; limit, at most MaxRunsLimit,
// is the page size; before continues a list after the run with that ID, as
// RunList.Next gives it.
const (
	DefaultRunsLimit = 50
	MaxRunsLimit     = 200
)

// RunList is a page of runs, newest first. Next, if set, is the before
// parameter of the next page.
type RunList struct {
	Runs []Run  `json:"runs"`
	Next string `json:"next,omitempty"`
}

// Run is a run of a harness version.
type Run struct {
	ID               string `json:"id"`
	HarnessVersionID int64  `json:"harness_version_id"`
	// Harness and HarnessVersion name the harness version the run runs.
	Harness        string `json:"harness"`
	HarnessVersion int    `json:"harness_version"`
	// StartedBy names the user who started the run.
	StartedBy  string     `json:"started_by"`
	Input      string     `json:"input"`
	Status     string     `json:"status"`
	Output     string     `json:"output"`
	Steps      int        `json:"steps"`
	Error      string     `json:"error,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// AuditRecord is a recorded audit record.
type AuditRecord struct {
	toolgateway.Record
	RecordedAt time.Time `json:"recorded_at"`
}

// TranscriptMessage is one message of a run's conversation with the model:
// the input, a model reply, or the results of the reply's tool calls.
// Secrets are redacted from it.
type TranscriptMessage struct {
	// Position is the message's index in the conversation.
	Position int `json:"position"`
	model.Message
	CreatedAt time.Time `json:"created_at"`
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
	RunID   string          `json:"run_id"`
	Harness string          `json:"harness"`
	Tool    string          `json:"tool"`
	Args    json.RawMessage `json:"args"`
	Reasons []string        `json:"reasons"`
	// ExpiresAt is when the request is rejected if no one answers it.
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Answer is the body of an answer to an approval request. The signed-in user
// is recorded in the audit log as the approver.
type Answer struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason,omitempty"`
}
