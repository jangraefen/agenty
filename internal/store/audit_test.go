package store_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

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
	require.ErrorContains(t, err, fmt.Sprint("anchor ", anchor.ID), "but not against an anchor kept elsewhere")
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
	cancelled, err := s.CancelIdleRun(ctx, "r2", "cancelled by alice", nil)
	require.NoError(t, err)
	require.True(t, cancelled)
	createRun(t, s, store.NewRun{ID: "r3", HarnessVersionID: v.ID, Input: "x", StartedBy: "bob"})
	claim(t, s, "r3")
	n, err := s.FailRunningRuns(ctx, "the server stopped")
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
	_, err = s.AppendEvent(ctx, auditlog.Event{Action: "server.started", Details: json.RawMessage(`{}`)})
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
		` server.started    {}`,
	}, events(t, s, 0), "what was said in a run is never part of its events")
	_, err = auditlog.Verify(bytes.NewReader(export(t, s)))
	require.NoError(t, err)
}

// TestAppends_ReadCommittedWhateverTheDefault: appends read committed data
// even where the database's default isolation is stricter, so concurrent
// changes never collide on the log's next id.
func TestAppends_ReadCommittedWhateverTheDefault(t *testing.T) {
	ctx := context.Background()
	s, url := storetest.NewWithURL(t)
	v, err := s.PutHarness(ctx, ws, "alice", notes())
	require.NoError(t, err)
	conn, err := pgx.Connect(ctx, url)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, conn.Close(context.Background())) })
	_, err = conn.Exec(ctx, `DO $$ BEGIN EXECUTE format('ALTER DATABASE %I SET default_transaction_isolation = %L', current_database(), 'repeatable read'); END $$`)
	require.NoError(t, err)
	strict, err := store.Open(ctx, url)
	require.NoError(t, err)
	t.Cleanup(strict.Close)

	const n = 16
	errs := make(chan error, n)
	for i := range n {
		go func() {
			_, err := strict.CreateRun(ctx, store.NewRun{ID: fmt.Sprint("r", i), HarnessVersionID: v.ID, Input: "x", StartedBy: "alice"})
			errs <- err
		}()
	}
	for range n {
		require.NoError(t, <-errs)
	}
	_, err = auditlog.Verify(bytes.NewReader(export(t, strict)))
	require.NoError(t, err)
}

func TestListAuditEvents(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	v, err := s.PutHarness(ctx, ws, "alice", notes())
	require.NoError(t, err)
	_, err = s.PutHarness(ctx, "work", "bob", notes())
	require.NoError(t, err)
	createRun(t, s, store.NewRun{ID: "r1", HarnessVersionID: v.ID, Input: "x", StartedBy: "alice"})
	record(t, s, toolgateway.Record{RunID: "r1", CallID: "c1", Event: toolgateway.EventDecision, Tool: "files_read", Decision: toolgateway.Allow})
	_, err = s.AppendEvent(ctx, auditlog.Event{Action: "server.started", Details: json.RawMessage(`{}`)})
	require.NoError(t, err)

	list := func(f store.EventFilter) []string {
		t.Helper()
		page, err := s.ListAuditEvents(ctx, f)
		require.NoError(t, err)
		out := make([]string, len(page))
		for i, e := range page {
			out[i] = fmt.Sprint(e.ID, " ", e.Action)
		}
		return out
	}
	for _, tt := range []struct {
		name   string
		filter store.EventFilter
		want   []string
	}{
		{"all, newest first", store.EventFilter{Limit: 10}, []string{"5 server.started", "4 tool.decision", "3 run.started", "2 harness.changed", "1 harness.changed"}},
		{"a page", store.EventFilter{Limit: 2}, []string{"5 server.started", "4 tool.decision"}},
		{"the next page", store.EventFilter{Limit: 2, Before: 4}, []string{"3 run.started", "2 harness.changed"}},
		{"by an actor", store.EventFilter{Limit: 10, Actor: "alice"}, []string{"4 tool.decision", "3 run.started", "1 harness.changed"}},
		{"in a workspace", store.EventFilter{Limit: 10, Workspace: "work"}, []string{"2 harness.changed"}},
		{"with an action", store.EventFilter{Limit: 10, Workspace: ws, Action: "harness.changed"}, []string{"1 harness.changed"}},
		{"about a run", store.EventFilter{Limit: 10, RunID: "r1"}, []string{"4 tool.decision", "3 run.started"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, list(tt.filter))
		})
	}
	_, err = s.ListAuditEvents(ctx, store.EventFilter{})
	require.ErrorContains(t, err, "limit")

	ids := func(page []auditlog.Event, err error) []string {
		t.Helper()
		require.NoError(t, err)
		out := make([]string, len(page))
		for i, e := range page {
			out[i] = fmt.Sprint(e.ID, " ", e.Action)
		}
		return out
	}
	assert.Equal(t, []string{"4 tool.decision", "3 run.started", "1 harness.changed"}, ids(s.ActorEvents(ctx, "alice", 0, 10)))
	assert.Equal(t, []string{"3 run.started", "1 harness.changed"}, ids(s.ActorEvents(ctx, "alice", 4, 10)), "before an event")
	assert.Equal(t, []string{"1 harness.changed"}, ids(s.WorkspaceEvents(ctx, ws, "harness.changed", 0, 10)))
	assert.Empty(t, ids(s.WorkspaceEvents(ctx, ws, "harness.changed", 1, 10)))
	_, err = s.ActorEvents(ctx, "alice", 0, 0)
	require.Error(t, err)
	_, err = s.WorkspaceEvents(ctx, ws, "harness.changed", 0, 0)
	require.Error(t, err)
}

// TestRunEvents_FollowARunAfterACursor: a run's event stream is read from
// the audit log: the tool gateway's records, its approval requests and its
// end, in order, after the event a reader saw last, a page at a time. The
// other events of the run, and those of other runs, are not part of it.
func TestRunEvents_FollowARunAfterACursor(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	newRun(t, s, "r1")
	record(t, s, toolgateway.Record{RunID: "r1", CallID: "c1", Event: toolgateway.EventDecision, Tool: "files_write", Decision: toolgateway.RequireApproval})
	want := store.NewApproval{
		ID: "a1", RunID: "r1", CallID: "c1", Tool: "files_write", Args: json.RawMessage(`{"path":"notes.md"}`),
		Reasons: []string{"writes need a human"}, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
	}
	require.NoError(t, s.SuspendRun(ctx, want))
	newRun(t, s, "r2")
	record(t, s, toolgateway.Record{RunID: "r2", CallID: "c2", Event: toolgateway.EventDecision, Tool: "files_read", Decision: toolgateway.Allow})
	cancelled, err := s.CancelIdleRun(ctx, "r1", "cancelled by alice", nil)
	require.NoError(t, err)
	require.True(t, cancelled)

	all, err := s.RunEvents(ctx, "r1", 0, 100)
	require.NoError(t, err)
	require.Len(t, all, 3)
	require.NotNil(t, all[0].Record)
	assert.Equal(t, toolgateway.EventDecision, all[0].Record.Event)
	assert.Equal(t, "c1", all[0].Record.CallID)
	assert.Nil(t, all[0].Approval)
	assert.False(t, all[0].Finished)
	require.NotNil(t, all[1].Approval)
	assert.Nil(t, all[1].Record)
	assert.Equal(t, "a1", all[1].Approval.ID)
	assert.Equal(t, "notes", all[1].Approval.Harness)
	assert.Equal(t, store.ApprovalWithdrawn, all[1].Approval.Status, "a request comes as it is now")
	assert.True(t, all[2].Finished)
	assert.Nil(t, all[2].Record)
	assert.Nil(t, all[2].Approval)
	assert.Less(t, all[0].ID, all[1].ID)
	assert.Less(t, all[1].ID, all[2].ID)

	rest, err := s.RunEvents(ctx, "r1", all[0].ID, 100)
	require.NoError(t, err)
	assert.Equal(t, all[1:], rest)
	page, err := s.RunEvents(ctx, "r1", 0, 1)
	require.NoError(t, err)
	assert.Equal(t, all[:1], page)
	none, err := s.RunEvents(ctx, "ghost", 0, 100)
	require.NoError(t, err)
	assert.Empty(t, none)
	_, err = s.RunEvents(ctx, "r1", 0, 0)
	require.Error(t, err)
}
