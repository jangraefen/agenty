package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/jangraefen/agenty/internal/auditlog"
	"github.com/jangraefen/agenty/internal/must"
	"github.com/jangraefen/agenty/internal/store/db"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

var _ toolgateway.Audit = (*Store)(nil)

// toolActions are the actions of the tool gateway's records, by event, and
// toolEvents the events by action.
var (
	toolActions = map[toolgateway.Event]string{
		toolgateway.EventDecision: "tool.decision",
		toolgateway.EventApproval: "tool.approval",
		toolgateway.EventResult:   "tool.result",
	}
	toolEvents = func() map[string]toolgateway.Event {
		out := make(map[string]toolgateway.Event, len(toolActions))
		for event, action := range toolActions {
			out[action] = event
		}
		return out
	}()
)

// toolDetails are the details of a tool event: its record but for the run
// and the event, which the event holds itself.
type toolDetails struct {
	CallID   string               `json:"call_id"`
	Tool     string               `json:"tool"`
	Args     json.RawMessage      `json:"args,omitempty"`
	Decision toolgateway.Decision `json:"decision"`
	Reason   string               `json:"reason,omitempty"`
	Approver string               `json:"approver,omitempty"`
	Result   json.RawMessage      `json:"result,omitempty"`
	Err      string               `json:"error,omitempty"`
}

// toolEvent is the audit event of a tool gateway record of a run that
// starter started in workspace. An approval's actor is whoever answered it,
// no one when it expired or was withdrawn; the run acts for its starter
// otherwise.
func toolEvent(rec toolgateway.Record, starter, workspace string) (auditlog.Event, error) {
	action, ok := toolActions[rec.Event]
	if !ok {
		return auditlog.Event{}, fmt.Errorf("store: audit: unknown event %q", rec.Event)
	}
	details, err := json.Marshal(toolDetails{
		CallID:   text(rec.CallID),
		Tool:     text(rec.Tool),
		Args:     jsonText(rec.Args),
		Decision: toolgateway.Decision(text(string(rec.Decision))),
		Reason:   text(rec.Reason),
		Approver: text(rec.Approver),
		Result:   jsonText(rec.Result),
		Err:      text(rec.Err),
	})
	if err != nil {
		return auditlog.Event{}, fmt.Errorf("store: audit: %w", err)
	}
	actor := starter
	if rec.Event == toolgateway.EventApproval {
		actor = rec.Approver
	}
	return auditlog.Event{
		Actor:     text(actor),
		Action:    action,
		Workspace: workspace,
		RunID:     text(rec.RunID),
		Details:   details,
	}, nil
}

// appendEvent appends e to the audit log in q's transaction, as the last
// thing that transaction does: it holds the log's lock until it ends. It
// sets e's id, time and hashes. The transaction must read committed data,
// PostgreSQL's default, so it sees the event appended before it.
func appendEvent(ctx context.Context, q *db.Queries, e auditlog.Event) (auditlog.Event, error) {
	details, err := auditlog.Canonical(e.Details)
	if err != nil {
		return auditlog.Event{}, err
	}
	if !bytes.HasPrefix(details, []byte("{")) {
		return auditlog.Event{}, fmt.Errorf("store: audit: %s: details must be a JSON object", e.Action)
	}
	e.Details = details
	if err := q.LockAuditLog(ctx); err != nil {
		return auditlog.Event{}, fmt.Errorf("store: audit: %w", err)
	}
	last, err := q.LastAuditEvent(ctx)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return auditlog.Event{}, fmt.Errorf("store: audit: %w", err)
	default:
		e.ID = last.ID
		copy(e.PrevHash[:], last.Hash)
	}
	e.ID++
	e.RecordedAt = time.Now().UTC().Truncate(time.Microsecond)
	e.Hash = e.Sum()
	if err := q.InsertAuditEvent(ctx, db.InsertAuditEventParams{
		ID:         e.ID,
		RecordedAt: e.RecordedAt,
		Actor:      e.Actor,
		Action:     e.Action,
		Workspace:  e.Workspace,
		RunID:      optional(e.RunID),
		Target:     e.Target,
		Details:    e.Details,
		PrevHash:   e.PrevHash[:],
		Hash:       e.Hash[:],
	}); err != nil {
		return auditlog.Event{}, fmt.Errorf("store: audit: %w", err)
	}
	return e, nil
}

// Record appends rec to the audit log. The record's run must exist. An error
// means the record may not be stored, and the gateway then does not execute
// the call. Content never makes recording fail: PostgreSQL rejects NUL bytes
// and invalid UTF-8 in text, so those are replaced, and anything that is not
// valid JSON, such as malformed arguments from a model, is stored as a JSON
// string.
func (s *Store) Record(ctx context.Context, rec toolgateway.Record) error {
	_, err := s.RecordAt(ctx, rec)
	return err
}

// RecordAt is Record, and returns when the record was recorded.
func (s *Store) RecordAt(ctx context.Context, rec toolgateway.Record) (time.Time, error) {
	appended, err := s.withEvents(ctx, func(q *db.Queries) ([]auditlog.Event, error) {
		owner, err := q.RunOwner(ctx, rec.RunID)
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf("run %s: %w", rec.RunID, ErrNotFound)
		}
		if err != nil {
			return nil, fmt.Errorf("store: audit: %w", err)
		}
		e, err := toolEvent(rec, owner.StartedBy, owner.Workspace)
		return []auditlog.Event{e}, err
	})
	if err != nil {
		return time.Time{}, err
	}
	return appended[0].RecordedAt, nil
}

// AuditRecord is a stored audit record.
type AuditRecord struct {
	toolgateway.Record
	RecordedAt time.Time
}

// AuditRecords returns what the tool gateway recorded for a run, in order.
func (s *Store) AuditRecords(ctx context.Context, runID string) ([]AuditRecord, error) {
	rows, err := s.queries.ToolEventsOfRun(ctx, pgtype.Text{String: runID, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("store: audit: %w", err)
	}
	out := make([]AuditRecord, len(rows))
	for i, row := range rows {
		if out[i], err = toolRecord(runID, row.Action, row.Details, row.RecordedAt); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// toolRecord is the tool gateway record of the run's event with action and
// details.
func toolRecord(runID, action string, details []byte, at time.Time) (AuditRecord, error) {
	var d toolDetails
	if err := json.Unmarshal(details, &d); err != nil {
		return AuditRecord{}, fmt.Errorf("store: audit: %w", err)
	}
	return AuditRecord{
		Record: toolgateway.Record{
			RunID:    runID,
			CallID:   d.CallID,
			Event:    toolEvents[action],
			Tool:     d.Tool,
			Args:     d.Args,
			Decision: d.Decision,
			Reason:   d.Reason,
			Approver: d.Approver,
			Result:   d.Result,
			Err:      d.Err,
		},
		RecordedAt: at.UTC(),
	}, nil
}

// RunEvent is an event of a run's event stream, read from the audit log:
// one of a record of the tool gateway, a request the run waited for, as it
// is now, or the run's end.
type RunEvent struct {
	// ID is the event's id in the audit log: the events after it follow.
	ID       int64
	Record   *AuditRecord
	Approval *Approval
	Finished bool
}

// RunEvents returns up to limit events of the run's event stream after the
// one with id after, in order.
func (s *Store) RunEvents(ctx context.Context, runID string, after int64, limit int) ([]RunEvent, error) {
	if limit <= 0 || limit > math.MaxInt32 {
		return nil, fmt.Errorf("store: audit: limit %d is out of range", limit)
	}
	rows, err := s.queries.RunEventsAfter(ctx, db.RunEventsAfterParams{RunID: pgtype.Text{String: runID, Valid: true}, After: after, MaxRows: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("store: audit: %w", err)
	}
	out := make([]RunEvent, len(rows))
	for i, row := range rows {
		out[i].ID = row.ID
		switch row.Action {
		case "run.finished":
			out[i].Finished = true
		case "approval.requested":
			var d struct {
				Approval string `json:"approval"`
			}
			if err := json.Unmarshal(row.Details, &d); err != nil {
				return nil, fmt.Errorf("store: audit: %w", err)
			}
			a, err := s.approval(ctx, runID, d.Approval)
			if err != nil {
				return nil, err
			}
			out[i].Approval = &a
		default:
			rec, err := toolRecord(runID, row.Action, row.Details, row.RecordedAt)
			if err != nil {
				return nil, err
			}
			out[i].Record = &rec
		}
	}
	return out, nil
}

// LastAuditEventID returns the id of the audit log's latest event, 0 before
// the first: an export reads up to it.
func (s *Store) LastAuditEventID(ctx context.Context) (int64, error) {
	last, err := s.queries.LastAuditEvent(ctx)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return 0, nil
	case err != nil:
		return 0, fmt.Errorf("store: audit: %w", err)
	}
	return last.ID, nil
}

// AuditEvents returns up to limit events of the audit log after the one with
// id after and up to the one with id last, in order: an export reads the log
// a page at a time.
func (s *Store) AuditEvents(ctx context.Context, after, last int64, limit int) ([]auditlog.Event, error) {
	if limit <= 0 || limit > 1<<20 {
		return nil, fmt.Errorf("store: audit: limit %d is out of range", limit)
	}
	rows, err := s.queries.AuditEventsAfter(ctx, db.AuditEventsAfterParams{After: after, Last: last, MaxRows: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("store: audit: %w", err)
	}
	return events(rows), nil
}

// EventFilter selects events of the audit log to list, for auditors.
type EventFilter struct {
	// Actor, Workspace, Action and RunID, when set, select the events by
	// that actor, in that workspace, with that action or about that run.
	Actor     string
	Workspace string
	Action    string
	RunID     string
	// Before, when set, lists the events before the one with that id.
	Before int64
	// Limit is the most events to return; it must be greater than 0.
	Limit int
}

// ListAuditEvents lists the events of the audit log that f selects, newest
// first.
func (s *Store) ListAuditEvents(ctx context.Context, f EventFilter) ([]auditlog.Event, error) {
	if f.Limit <= 0 || f.Limit > math.MaxInt32 {
		return nil, fmt.Errorf("store: audit: limit %d is out of range", f.Limit)
	}
	rows, err := s.queries.ListAuditEvents(ctx, db.ListAuditEventsParams{
		Actor:     optional(f.Actor),
		Workspace: optional(f.Workspace),
		Action:    optional(f.Action),
		RunID:     optional(f.RunID),
		Before:    pgtype.Int8{Int64: f.Before, Valid: f.Before > 0},
		MaxRows:   int32(f.Limit),
	})
	if err != nil {
		return nil, fmt.Errorf("store: audit: %w", err)
	}
	return events(rows), nil
}

// ActorEvents lists up to limit events by actor, newest first, before the
// one with id before (0 for the newest).
func (s *Store) ActorEvents(ctx context.Context, actor string, before int64, limit int) ([]auditlog.Event, error) {
	if limit <= 0 || limit > math.MaxInt32 {
		return nil, fmt.Errorf("store: audit: limit %d is out of range", limit)
	}
	rows, err := s.queries.ListActorEvents(ctx, db.ListActorEventsParams{Actor: actor, Before: before, MaxRows: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("store: audit: %w", err)
	}
	return events(rows), nil
}

// WorkspaceEvents lists up to limit events of workspace with action, newest
// first, before the one with id before (0 for the newest).
func (s *Store) WorkspaceEvents(ctx context.Context, workspace, action string, before int64, limit int) ([]auditlog.Event, error) {
	if limit <= 0 || limit > math.MaxInt32 {
		return nil, fmt.Errorf("store: audit: limit %d is out of range", limit)
	}
	rows, err := s.queries.ListWorkspaceEvents(ctx, db.ListWorkspaceEventsParams{Workspace: workspace, Action: action, Before: before, MaxRows: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("store: audit: %w", err)
	}
	return events(rows), nil
}

func events(rows []db.AuditEvent) []auditlog.Event {
	out := make([]auditlog.Event, len(rows))
	for i, row := range rows {
		out[i] = auditlog.Event{
			ID:         row.ID,
			RecordedAt: row.RecordedAt.UTC(),
			Actor:      row.Actor,
			Action:     row.Action,
			Workspace:  row.Workspace,
			RunID:      row.RunID.String,
			Target:     row.Target,
			Details:    row.Details,
		}
		copy(out[i].PrevHash[:], row.PrevHash)
		copy(out[i].Hash[:], row.Hash)
	}
	return out
}

// withEvents runs change in a transaction and appends the audit events it
// returns at its end, as the last thing the transaction does. It returns
// them as appended.
func (s *Store) withEvents(ctx context.Context, change func(*db.Queries) ([]auditlog.Event, error)) ([]auditlog.Event, error) {
	var appended []auditlog.Event
	err := s.inTx(ctx, func(q *db.Queries) error {
		events, err := change(q)
		if err != nil {
			return err
		}
		for _, e := range events {
			if e, err = appendEvent(ctx, q, e); err != nil {
				return err
			}
			appended = append(appended, e)
		}
		return nil
	})
	return appended, err
}

// AppendEvent appends an event of the server's own, such as its start, or a
// request recorded before it takes effect, and returns it as appended.
func (s *Store) AppendEvent(ctx context.Context, e auditlog.Event) (auditlog.Event, error) {
	appended, err := s.withEvents(ctx, func(*db.Queries) ([]auditlog.Event, error) {
		return []auditlog.Event{e}, nil
	})
	if err != nil {
		return auditlog.Event{}, err
	}
	return appended[0], nil
}

// details is the JSON of an event's details.
func details(v any) json.RawMessage {
	return must.Value(json.Marshal(v))
}

// runStarted is the event of a run started in workspace. It names the
// harness version and the run it follows, not the run's input.
func runStarted(r Run, workspace string) auditlog.Event {
	return auditlog.Event{
		Actor:     r.StartedBy,
		Action:    "run.started",
		Workspace: workspace,
		RunID:     r.ID,
		Details: details(struct {
			Harness string `json:"harness"`
			Version int    `json:"version"`
			Follows string `json:"follows,omitempty"`
		}{r.Harness, r.HarnessVersion, r.Follows}),
	}
}

// runFinished is the event of the run of workspace that just ended with
// status. It names how the run ended, not its output; the server ends a run,
// so it has no actor.
func runFinished(workspace, id string, status RunStatus, steps int32, runErr string) auditlog.Event {
	return auditlog.Event{
		Action:    "run.finished",
		Workspace: workspace,
		RunID:     id,
		Details: details(struct {
			Status RunStatus `json:"status"`
			Steps  int32     `json:"steps"`
			Error  string    `json:"error,omitempty"`
		}{status, steps, runErr}),
	}
}

// approvalRequested is the event of a request the run of workspace, which
// starter started, makes as it suspends: which call waits, with what
// arguments, redacted, why, and until when. The results of the calls before
// it are not part of it; their own records are.
func approvalRequested(a NewApproval, starter, workspace string) auditlog.Event {
	return auditlog.Event{
		Actor:     starter,
		Action:    "approval.requested",
		Workspace: workspace,
		RunID:     a.RunID,
		Details: details(struct {
			Approval  string          `json:"approval"`
			CallID    string          `json:"call_id"`
			Tool      string          `json:"tool"`
			Args      json.RawMessage `json:"args,omitempty"`
			Reasons   []string        `json:"reasons"`
			ExpiresAt time.Time       `json:"expires_at"`
		}{a.ID, text(a.CallID), text(a.Tool), jsonText(a.Args), nonNil(a.Reasons), a.ExpiresAt.UTC()}),
	}
}

// harnessChanged is the event of a harness's new version, stored by user.
func harnessChanged(workspace, user, name string, version int32) auditlog.Event {
	return auditlog.Event{
		Actor:     user,
		Action:    "harness.changed",
		Workspace: workspace,
		Target:    name,
		Details: details(struct {
			Version int32 `json:"version"`
		}{version}),
	}
}

// wrapRun is err, if any, as an error of the run with the given ID.
func wrapRun(id string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("store: run %s: %w", id, err)
}
