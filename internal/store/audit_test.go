package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the pgx driver for database/sql
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/auditlog"
	"github.com/jangraefen/agenty/internal/store"
	"github.com/jangraefen/agenty/internal/store/storetest"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

// export writes the whole audit log as an export.
func export(t *testing.T, s *store.Store) []byte {
	t.Helper()
	var b bytes.Buffer
	for after := int64(0); ; {
		page, err := s.AuditEvents(context.Background(), after, math.MaxInt64, 2)
		require.NoError(t, err)
		if len(page) == 0 {
			return b.Bytes()
		}
		for _, e := range page {
			require.NoError(t, auditlog.WriteLine(&b, e))
			after = e.ID
		}
	}
}

func record(t *testing.T, s *store.Store, rec toolgateway.Record) {
	t.Helper()
	require.NoError(t, s.Record(context.Background(), rec))
}

func TestAuditEvents_ToolRecords(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	v := newRun(t, s, "r1")
	createRun(t, s, store.NewRun{ID: "r2", HarnessVersionID: v.ID, Input: "x", StartedBy: "bob"})
	record(t, s, toolgateway.Record{RunID: "r1", CallID: "c1", Event: toolgateway.EventDecision, Tool: "files_write", Args: json.RawMessage(`{"path":"a"}`), Decision: toolgateway.RequireApproval, Reason: "writes need a human"})
	record(t, s, toolgateway.Record{RunID: "r1", CallID: "c1", Event: toolgateway.EventApproval, Tool: "files_write", Decision: toolgateway.Allow, Approver: "carol"})
	record(t, s, toolgateway.Record{RunID: "r2", CallID: "c2", Event: toolgateway.EventApproval, Tool: "files_write", Decision: toolgateway.Deny, Reason: "approval expired"})
	record(t, s, toolgateway.Record{RunID: "r1", CallID: "c1", Event: toolgateway.EventResult, Tool: "files_write", Decision: toolgateway.Allow, Result: json.RawMessage(`{"ok":true}`)})

	all, err := s.AuditEvents(ctx, 0, math.MaxInt64, 100)
	require.NoError(t, err)
	type summary struct{ actor, action, workspace, run string }
	var got []summary
	var events []auditlog.Event
	for i, e := range all {
		assert.Equal(t, int64(i+1), e.ID, "ids run from 1 without gaps")
		if strings.HasPrefix(e.Action, "tool.") {
			got = append(got, summary{e.Actor, e.Action, e.Workspace, e.RunID})
			events = append(events, e)
		}
	}
	assert.Equal(t, []summary{
		{"alice", "tool.decision", ws, "r1"},
		{"carol", "tool.approval", ws, "r1"},
		{"", "tool.approval", ws, "r2"},
		{"alice", "tool.result", ws, "r1"},
	}, got, "a run acts for its starter; an approval is its approver's, or no one's when it expired")
	assert.JSONEq(t, `{"call_id":"c1","tool":"files_write","args":{"path":"a"},"decision":"require_approval","reason":"writes need a human"}`, string(events[0].Details))

	records, err := s.AuditRecords(ctx, "r1")
	require.NoError(t, err)
	require.Len(t, records, 3, "a run's records are its tool events only")
	assert.Equal(t, toolgateway.Record{RunID: "r1", CallID: "c1", Event: toolgateway.EventApproval, Tool: "files_write", Decision: toolgateway.Allow, Approver: "carol"}, records[1].Record)
	assert.JSONEq(t, `{"ok":true}`, string(records[2].Result))
	assert.Nil(t, records[1].Args, "a field the record did not have stays absent")

	_, err = auditlog.Verify(bytes.NewReader(export(t, s)))
	require.NoError(t, err)

	_, err = s.AuditEvents(ctx, 0, math.MaxInt64, 0)
	require.Error(t, err)
}

// TestInvariant_AuditLogIsTamperEvident guards trust-model guarantee 9: the
// database refuses to change, remove or truncate an event, and a change made
// around that, with its triggers disabled, shows in the chain: one event
// changed or removed fails verification, and a rewrite that computes every
// hash anew fails against an anchor kept from an earlier export.
func TestInvariant_AuditLogIsTamperEvident(t *testing.T) {
	ctx := context.Background()
	s, url := storetest.NewWithURL(t)
	newRun(t, s, "r1")
	for _, call := range []string{"c1", "c2", "c3"} {
		record(t, s, toolgateway.Record{RunID: "r1", CallID: call, Event: toolgateway.EventDecision, Tool: "files_read", Decision: toolgateway.Allow})
	}
	before := export(t, s)
	sum, err := auditlog.Verify(bytes.NewReader(before))
	require.NoError(t, err)
	anchor := auditlog.Anchor{ID: sum.Last, Hash: sum.LastHash}

	conn, err := pgx.Connect(ctx, url)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, conn.Close(context.Background())) })
	for _, statement := range []string{
		`UPDATE audit_events SET actor = 'mallory' WHERE id = 2`,
		`DELETE FROM audit_events WHERE id = 3`,
		`TRUNCATE audit_events`,
	} {
		_, err := conn.Exec(ctx, statement)
		require.ErrorContains(t, err, "append-only", statement)
	}
	assert.Equal(t, before, export(t, s), "nothing changed")

	_, err = conn.Exec(ctx, `ALTER TABLE audit_events DISABLE TRIGGER USER`)
	require.NoError(t, err, "the table's owner can disable the triggers")
	_, err = conn.Exec(ctx, `UPDATE audit_events SET actor = 'mallory' WHERE id = 2`)
	require.NoError(t, err)
	_, err = auditlog.Verify(bytes.NewReader(export(t, s)))
	require.ErrorContains(t, err, "event 2", "a changed event fails")

	_, err = conn.Exec(ctx, `DELETE FROM audit_events WHERE id = 2`)
	require.NoError(t, err)
	_, err = auditlog.Verify(bytes.NewReader(export(t, s)))
	require.ErrorContains(t, err, "event 3", "a removed event fails")

	// The limit: the newest events removed leave a chain that verifies on
	// its own; only an anchor at or past them shows it.
	_, err = conn.Exec(ctx, `DELETE FROM audit_events WHERE id >= 2`)
	require.NoError(t, err)
	_, err = auditlog.Verify(bytes.NewReader(export(t, s)))
	require.NoError(t, err, "a log without its newest events verifies on its own")
	_, err = auditlog.Verify(bytes.NewReader(export(t, s)), anchor)
	require.ErrorContains(t, err, "anchor", "but not against an anchor kept from before")

	// A forger who knows the format writes the chain anew.
	_, err = conn.Exec(ctx, `DELETE FROM audit_events`)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, `ALTER TABLE audit_events ENABLE TRIGGER USER`)
	require.NoError(t, err)
	record(t, s, toolgateway.Record{RunID: "r1", CallID: "c1", Event: toolgateway.EventDecision, Tool: "files_read", Decision: toolgateway.Deny})
	for _, call := range []string{"c2", "c3"} {
		record(t, s, toolgateway.Record{RunID: "r1", CallID: call, Event: toolgateway.EventDecision, Tool: "files_read", Decision: toolgateway.Allow})
	}
	forged := export(t, s)
	_, err = auditlog.Verify(bytes.NewReader(forged))
	require.NoError(t, err, "a consistent rewrite verifies on its own")
	_, err = auditlog.Verify(bytes.NewReader(forged), anchor)
	require.ErrorContains(t, err, "anchor", "but not against an anchor kept elsewhere")
}

func TestCopyAuditRecords(t *testing.T) {
	ctx := context.Background()
	s, url := storetest.NewWithURL(t)
	v := newRun(t, s, "r1")
	createRun(t, s, store.NewRun{ID: "r2", HarnessVersionID: v.ID, Input: "x", StartedBy: "bob"})
	sqlDB, err := sql.Open("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
	// The log as migration 12 left it, empty: the runs above are events now.
	_, err = sqlDB.ExecContext(ctx, `
		ALTER TABLE audit_events DISABLE TRIGGER USER;
		DELETE FROM audit_events;
		ALTER TABLE audit_events ENABLE TRIGGER USER`)
	require.NoError(t, err)
	// The table as migration 1 made it, and records as the store kept them.
	_, err = sqlDB.ExecContext(ctx, `
		CREATE TABLE audit_records (
		    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
		    run_id      text        NOT NULL REFERENCES runs (id),
		    call_id     text        NOT NULL,
		    event       text        NOT NULL,
		    tool        text        NOT NULL,
		    args        json,
		    decision    text        NOT NULL,
		    reason      text        NOT NULL DEFAULT '',
		    approver    text        NOT NULL DEFAULT '',
		    result      json,
		    error       text        NOT NULL DEFAULT '',
		    recorded_at timestamptz NOT NULL DEFAULT now()
		);
		INSERT INTO audit_records (run_id, call_id, event, tool, args, decision, reason) VALUES
		    ('r1', 'c1', 'decision', 'files_write', '{"b": 1, "a": "<x>"}', 'require_approval', 'writes need a human');
		INSERT INTO audit_records (run_id, call_id, event, tool, decision, approver) VALUES
		    ('r1', 'c1', 'approval', 'files_write', 'allow', 'carol');
		INSERT INTO audit_records (run_id, call_id, event, tool, decision, reason) VALUES
		    ('r2', 'c2', 'approval', 'files_write', 'deny', 'approval expired');
		INSERT INTO audit_records (run_id, call_id, event, tool, decision, result, error) VALUES
		    ('r1', 'c1', 'result', 'files_write', 'allow', '{"ok": true}', '')`)
	require.NoError(t, err)

	// Pages of 3, so the chain carries over from one page to the next.
	t.Cleanup(store.SetCopyPage(3))
	tx, err := sqlDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.NoError(t, store.CopyAuditRecords(ctx, tx))
	require.NoError(t, tx.Commit())

	records, err := s.AuditRecords(ctx, "r1")
	require.NoError(t, err)
	require.Len(t, records, 3)
	canonical, err := auditlog.Canonical([]byte(`{"b": 1, "a": "<x>"}`))
	require.NoError(t, err)
	assert.Equal(t, string(canonical), string(records[0].Args), "copied in the canonical form, in its order")
	assert.Equal(t, "writes need a human", records[0].Reason)
	assert.Equal(t, "carol", records[1].Approver)
	assert.Nil(t, records[1].Args, "a NULL column stays absent")
	assert.JSONEq(t, `{"ok":true}`, string(records[2].Result))
	events, err := s.AuditEvents(ctx, 0, math.MaxInt64, 10)
	require.NoError(t, err)
	require.Len(t, events, 4)
	assert.Empty(t, events[2].Actor, "an expired approval is no one's")
	sum, err := auditlog.Verify(bytes.NewReader(export(t, s)))
	require.NoError(t, err, "the copied records are chained")
	assert.Equal(t, int64(4), sum.Last)

	record(t, s, toolgateway.Record{RunID: "r2", CallID: "c3", Event: toolgateway.EventDecision, Tool: "files_read", Decision: toolgateway.Allow})
	events, err = s.AuditEvents(ctx, 4, math.MaxInt64, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "bob", events[0].Actor)
	_, err = auditlog.Verify(bytes.NewReader(export(t, s)))
	require.NoError(t, err, "and the log goes on from them")
}

// TestRecord_ConcurrentAppendsStayLinear: appends from many workers at once
// are serialised, so the chain neither forks nor skips an id.
func TestRecord_ConcurrentAppendsStayLinear(t *testing.T) {
	s := storetest.New(t)
	newRun(t, s, "r1")
	const n = 24
	errs := make(chan error, n)
	for i := range n {
		go func() {
			_, err := s.RecordAt(context.Background(), toolgateway.Record{RunID: "r1", CallID: fmt.Sprint("c", i), Event: toolgateway.EventDecision, Tool: "files_read", Decision: toolgateway.Allow})
			errs <- err
		}()
	}
	for range n {
		require.NoError(t, <-errs)
	}
	sum, err := auditlog.Verify(bytes.NewReader(export(t, s)))
	require.NoError(t, err, "one chain, without gaps")
	records, err := s.AuditRecords(context.Background(), "r1")
	require.NoError(t, err)
	assert.Len(t, records, n, "every append made it")
	assert.Equal(t, int64(sum.Events), sum.Last)
}

// events returns "actor action workspace run target details" for every
// event of the audit log after the given one.
func events(t *testing.T, s *store.Store, after int64) []string {
	t.Helper()
	page, err := s.AuditEvents(context.Background(), after, math.MaxInt64, 100)
	require.NoError(t, err)
	out := make([]string, len(page))
	for i, e := range page {
		out[i] = strings.Join([]string{e.Actor, e.Action, e.Workspace, e.RunID, e.Target, string(e.Details)}, " ")
	}
	return out
}

func TestAuditEvents_RunsAndHarnesses(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	v, err := s.PutHarness(ctx, ws, "alice", notes())
	require.NoError(t, err)
	_, err = s.PutHarness(ctx, ws, "bob", notes())
	require.NoError(t, err, "an unchanged harness is no change")
	changed := notes()
	changed.Instructions = "Tidy them well."
	_, err = s.PutHarness(ctx, ws, "bob", changed)
	require.NoError(t, err)
	createRun(t, s, store.NewRun{ID: "r1", HarnessVersionID: v.ID, Input: "a private prompt", StartedBy: "alice"})
	claim(t, s, "r1")
	require.NoError(t, s.FinishRun(ctx, "r1", store.RunSucceeded, "a private answer", 2, ""))
	createRun(t, s, store.NewRun{ID: "r2", HarnessVersionID: v.ID, Input: "x", StartedBy: "alice", Follows: "r1"})
	cancelled, err := s.CancelIdleRun(ctx, "r2", "cancelled by alice")
	require.NoError(t, err)
	require.True(t, cancelled)
	createRun(t, s, store.NewRun{ID: "r3", HarnessVersionID: v.ID, Input: "x", StartedBy: "bob"})
	claim(t, s, "r3")
	n, err := s.FailRunningRuns(ctx, "the server stopped")
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
	_, err = s.AppendEvent(ctx, auditlog.Event{Actor: "dana", Action: "audit.read", Details: json.RawMessage(`{"read":"runs"}`)})
	require.NoError(t, err)

	assert.Equal(t, []string{
		`alice harness.changed home  notes {"version":1}`,
		`bob harness.changed home  notes {"version":2}`,
		`alice run.started home r1  {"harness":"notes","version":1}`,
		` run.finished home r1  {"status":"succeeded","steps":2}`,
		`alice run.started home r2  {"harness":"notes","version":1,"follows":"r1"}`,
		` run.finished home r2  {"status":"cancelled","steps":0,"error":"cancelled by alice"}`,
		`bob run.started home r3  {"harness":"notes","version":1}`,
		` run.finished home r3  {"status":"failed","steps":0,"error":"the server stopped"}`,
		`dana audit.read    {"read":"runs"}`,
	}, events(t, s, 0), "what was said in a run is never part of its events")
	_, err = auditlog.Verify(bytes.NewReader(export(t, s)))
	require.NoError(t, err)
}
