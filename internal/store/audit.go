package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/jangraefen/agenty/internal/auditlog"
	"github.com/jangraefen/agenty/internal/store/db"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

var _ toolgateway.Audit = (*Store)(nil)

// toolActions are the actions of the tool gateway's records, by event.
var toolActions = map[toolgateway.Event]string{
	toolgateway.EventDecision: "tool.decision",
	toolgateway.EventApproval: "tool.approval",
	toolgateway.EventResult:   "tool.result",
}

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
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return time.Time{}, fmt.Errorf("store: audit: %w", err)
	}
	q := s.queries.WithTx(tx)
	owner, err := q.RunOwner(ctx, rec.RunID)
	if err != nil {
		return time.Time{}, errors.Join(fmt.Errorf("store: audit: %w", notFound("run "+rec.RunID, err)), tx.Rollback(ctx))
	}
	e, err := toolEvent(rec, owner.StartedBy, owner.Workspace)
	if err == nil {
		e, err = appendEvent(ctx, q, e)
	}
	if err != nil {
		return time.Time{}, errors.Join(err, tx.Rollback(ctx))
	}
	if err := tx.Commit(ctx); err != nil {
		return time.Time{}, fmt.Errorf("store: audit: %w", err)
	}
	return e.RecordedAt, nil
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
	events := make(map[string]toolgateway.Event, len(toolActions))
	for event, action := range toolActions {
		events[action] = event
	}
	out := make([]AuditRecord, len(rows))
	for i, row := range rows {
		var d toolDetails
		if err := json.Unmarshal(row.Details, &d); err != nil {
			return nil, fmt.Errorf("store: audit: %w", err)
		}
		out[i] = AuditRecord{
			Record: toolgateway.Record{
				RunID:    runID,
				CallID:   d.CallID,
				Event:    events[row.Action],
				Tool:     d.Tool,
				Args:     d.Args,
				Decision: d.Decision,
				Reason:   d.Reason,
				Approver: d.Approver,
				Result:   d.Result,
				Err:      d.Err,
			},
			RecordedAt: row.RecordedAt.UTC(),
		}
	}
	return out, nil
}

// AuditEvents returns up to limit events of the audit log after the one with
// the given id, in order: an export reads the log a page at a time.
func (s *Store) AuditEvents(ctx context.Context, after int64, limit int) ([]auditlog.Event, error) {
	if limit <= 0 || limit > 1<<20 {
		return nil, fmt.Errorf("store: audit: limit %d is out of range", limit)
	}
	rows, err := s.queries.AuditEventsAfter(ctx, db.AuditEventsAfterParams{ID: after, Limit: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("store: audit: %w", err)
	}
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
	return out, nil
}

// copyAuditRecords is migration 13: it copies every record of the table that
// held the tool gateway's records before the audit log held every event,
// into the log, in order, chained.
func copyAuditRecords(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT r.run_id, r.call_id, r.event, r.tool, r.args::text, r.decision, r.reason, r.approver,
		       r.result::text, r.error, r.recorded_at, runs.started_by, harness_versions.workspace
		FROM audit_records r
		JOIN runs ON runs.id = r.run_id
		JOIN harness_versions ON harness_versions.id = runs.harness_version_id
		ORDER BY r.id`)
	if err != nil {
		return err
	}
	var events []auditlog.Event
	for rows.Next() {
		var rec toolgateway.Record
		var args, result sql.NullString
		var at time.Time
		var starter, workspace string
		if err := rows.Scan(&rec.RunID, &rec.CallID, &rec.Event, &rec.Tool, &args, &rec.Decision, &rec.Reason,
			&rec.Approver, &result, &rec.Err, &at, &starter, &workspace); err != nil {
			return errors.Join(err, rows.Close())
		}
		rec.Args = json.RawMessage(args.String)
		rec.Result = json.RawMessage(result.String)
		e, err := toolEvent(rec, starter, workspace)
		if err != nil {
			return errors.Join(err, rows.Close())
		}
		e.RecordedAt = at.UTC().Truncate(time.Microsecond)
		events = append(events, e)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	var prev auditlog.Hash
	for i, e := range events {
		if e.Details, err = auditlog.Canonical(e.Details); err != nil {
			return err
		}
		e.ID = int64(i + 1)
		e.PrevHash = prev
		e.Hash = e.Sum()
		prev = e.Hash
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO audit_events (id, recorded_at, actor, action, workspace, run_id, target, details, prev_hash, hash)
			VALUES ($1, $2, $3, $4, $5, $6, '', $7, $8, $9)`,
			e.ID, e.RecordedAt, e.Actor, e.Action, e.Workspace, e.RunID, string(e.Details), e.PrevHash[:], e.Hash[:]); err != nil {
			return err
		}
	}
	return nil
}
