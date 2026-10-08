package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
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

	events, err := s.AuditEvents(ctx, 0, math.MaxInt64, 10)
	require.NoError(t, err)
	require.Len(t, events, 4)
	type summary struct{ actor, action, workspace, run string }
	got := make([]summary, len(events))
	for i, e := range events {
		got[i] = summary{e.Actor, e.Action, e.Workspace, e.RunID}
		assert.Equal(t, int64(i+1), e.ID, "ids run from 1 without gaps")
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

	sum, err := auditlog.Verify(bytes.NewReader(export(t, s)))
	require.NoError(t, err)
	assert.Equal(t, 4, sum.Events)

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
	require.ErrorContains(t, err, "anchor 3", "but not against an anchor kept elsewhere")
}

func TestCopyAuditRecords(t *testing.T) {
	ctx := context.Background()
	s, url := storetest.NewWithURL(t)
	v := newRun(t, s, "r1")
	createRun(t, s, store.NewRun{ID: "r2", HarnessVersionID: v.ID, Input: "x", StartedBy: "bob"})
	sqlDB, err := sql.Open("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
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
	require.NoError(t, err)
	assert.Equal(t, int64(1), sum.First)
	assert.Equal(t, n, sum.Events)
}
