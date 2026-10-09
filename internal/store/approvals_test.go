package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/store"
	"github.com/jangraefen/agenty/internal/store/storetest"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

// suspendRun stores a running run r1 and suspends it at a call that waits
// for approval a1, which expires after ttl.
func suspendRun(t *testing.T, s *store.Store, ttl time.Duration) store.NewApproval {
	t.Helper()
	newRun(t, s, "r1")
	a := store.NewApproval{
		ID:        "a1",
		RunID:     "r1",
		CallID:    "call-1",
		Call:      1,
		Tool:      "files_write",
		Args:      json.RawMessage(`{"path":"notes.md"}`),
		Reasons:   []string{"writes need a human"},
		Results:   []model.ToolResult{{CallID: "c1", Content: `{"content":"- milk"}`}},
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(ttl),
	}
	require.NoError(t, s.SuspendRun(context.Background(), a))
	return a
}

func TestSuspendRun_StoresTheRequestAndWaits(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	want := suspendRun(t, s, time.Hour)

	run, err := s.Run(ctx, ws, "r1")
	require.NoError(t, err)
	assert.Equal(t, store.RunWaiting, run.Status)
	assert.False(t, run.Finished())
	pending, err := s.PendingApprovals(ctx, ws, "alice")
	require.NoError(t, err)
	require.Len(t, pending, 1)
	got := pending[0]
	assert.Equal(t, want.ID, got.ID)
	assert.Equal(t, want.RunID, got.RunID)
	assert.Equal(t, want.CallID, got.CallID)
	assert.Equal(t, want.Call, got.Call)
	assert.Equal(t, want.Tool, got.Tool)
	assert.JSONEq(t, string(want.Args), string(got.Args))
	assert.Equal(t, want.Reasons, got.Reasons)
	assert.Equal(t, want.Results, got.Results)
	assert.Equal(t, "notes", got.Harness)
	assert.Equal(t, store.ApprovalPending, got.Status)
	assert.WithinDuration(t, want.CreatedAt, got.CreatedAt, time.Millisecond)
	assert.WithinDuration(t, want.ExpiresAt, got.ExpiresAt, time.Millisecond)
	latest, ok, err := s.LatestApproval(ctx, "r1")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, got, latest)
	other, err := s.PendingApprovals(ctx, "work", "alice")
	require.NoError(t, err)
	assert.Empty(t, other, "a workspace sees its own requests only")
	theirs, err := s.PendingApprovals(ctx, ws, "bob")
	require.NoError(t, err)
	assert.Empty(t, theirs, "a user sees the requests of their own runs only")

	err = s.SuspendRun(ctx, store.NewApproval{ID: "a2", RunID: "r1", CallID: "call-2", Tool: "files_write", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)})
	require.ErrorIs(t, err, store.ErrNotFound, "only a running run is suspended")
	_, ok, err = s.LatestApproval(ctx, "ghost")
	require.NoError(t, err)
	assert.False(t, ok)
}

// TestSuspendRun_RecordsTheRequest: the audit log records a request as the
// run asks, in the same transaction, the run acting for its starter: which
// call waits, with what, why and until when.
func TestSuspendRun_RecordsTheRequest(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	want := suspendRun(t, s, time.Hour)

	requested, err := s.ListAuditEvents(ctx, store.EventFilter{Action: "approval.requested", Limit: 10})
	require.NoError(t, err)
	require.Len(t, requested, 1)
	e := requested[0]
	assert.Equal(t, "alice", e.Actor)
	assert.Equal(t, ws, e.Workspace)
	assert.Equal(t, "r1", e.RunID)
	var details struct {
		Approval  string          `json:"approval"`
		CallID    string          `json:"call_id"`
		Tool      string          `json:"tool"`
		Args      json.RawMessage `json:"args"`
		Reasons   []string        `json:"reasons"`
		ExpiresAt time.Time       `json:"expires_at"`
	}
	require.NoError(t, json.Unmarshal(e.Details, &details))
	assert.Equal(t, want.ID, details.Approval)
	assert.Equal(t, want.CallID, details.CallID)
	assert.Equal(t, want.Tool, details.Tool)
	assert.JSONEq(t, string(want.Args), string(details.Args))
	assert.Equal(t, want.Reasons, details.Reasons)
	assert.WithinDuration(t, want.ExpiresAt, details.ExpiresAt, time.Millisecond)
	assert.NotContains(t, string(e.Details), "milk", "the results of the calls before it are not part of the request")

	err = s.SuspendRun(ctx, store.NewApproval{ID: "a2", RunID: "r1", CallID: "call-2", Tool: "files_write", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)})
	require.ErrorIs(t, err, store.ErrNotFound)
	requested, err = s.ListAuditEvents(ctx, store.EventFilter{Action: "approval.requested", Limit: 10})
	require.NoError(t, err)
	assert.Len(t, requested, 1, "a request that is not stored is not recorded")
}

func TestAnswerApproval_QueuesTheRunOnce(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	suspendRun(t, s, time.Hour)

	answered, err := s.AnswerApproval(ctx, ws, "r1", "a1", store.Answer{Approved: true, Approver: "alice", Reason: "fine"})

	require.NoError(t, err)
	assert.True(t, answered)
	run, err := s.Run(ctx, ws, "r1")
	require.NoError(t, err)
	assert.Equal(t, store.RunQueued, run.Status, "the answer queues the run")
	latest, _, err := s.LatestApproval(ctx, "r1")
	require.NoError(t, err)
	assert.Equal(t, store.ApprovalApproved, latest.Status)
	assert.Equal(t, "alice", latest.Approver)
	assert.Equal(t, "fine", latest.Reason)
	pending, err := s.PendingApprovals(ctx, ws, "alice")
	require.NoError(t, err)
	assert.Empty(t, pending)
	for _, tt := range []struct{ name, workspace, run, id string }{
		{"answered already", ws, "r1", "a1"},
		{"another workspace", "work", "r1", "a1"},
		{"another run", ws, "r2", "a1"},
		{"no such request", ws, "r1", "ghost"},
	} {
		again, err := s.AnswerApproval(ctx, tt.workspace, tt.run, tt.id, store.Answer{Approver: "bob"})
		require.NoError(t, err, tt.name)
		assert.False(t, again, tt.name)
	}
	latest, _, err = s.LatestApproval(ctx, "r1")
	require.NoError(t, err)
	assert.Equal(t, "alice", latest.Approver, "an approval is answered once")
}

func TestAnswerApproval_RejectsAnExpiredRequest(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	suspendRun(t, s, -time.Second)

	answered, err := s.AnswerApproval(ctx, ws, "r1", "a1", store.Answer{Approved: true, Approver: "alice"})

	require.NoError(t, err)
	assert.False(t, answered, "an expired request is not answered")
}

func TestExpireApprovals(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	suspendRun(t, s, -time.Second)
	_, ok, err := s.NextApprovalExpiry(ctx)
	require.NoError(t, err)
	require.True(t, ok)

	n, err := s.ExpireApprovals(ctx, "no answer within 1h")

	require.NoError(t, err)
	assert.Equal(t, 1, n)
	latest, _, err := s.LatestApproval(ctx, "r1")
	require.NoError(t, err)
	assert.Equal(t, store.ApprovalRejected, latest.Status)
	assert.Equal(t, "no answer within 1h", latest.Reason)
	assert.Empty(t, latest.Approver)
	run, err := s.Run(ctx, ws, "r1")
	require.NoError(t, err)
	assert.Equal(t, store.RunQueued, run.Status, "the run resumes to tell the model")
	n, err = s.ExpireApprovals(ctx, "again")
	require.NoError(t, err)
	assert.Zero(t, n)
	_, ok, err = s.NextApprovalExpiry(ctx)
	require.NoError(t, err)
	assert.False(t, ok, "no request is pending")
}

func TestNextApprovalExpiry_IsTheEarliest(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	a := suspendRun(t, s, time.Hour)
	newRun(t, s, "r2")
	require.NoError(t, s.SuspendRun(ctx, store.NewApproval{ID: "a2", RunID: "r2", CallID: "call-2", Tool: "files_write", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(2 * time.Hour)}))

	next, ok, err := s.NextApprovalExpiry(ctx)

	require.NoError(t, err)
	require.True(t, ok)
	assert.WithinDuration(t, a.ExpiresAt, next, time.Millisecond)
}

func TestCancelIdleRun_WithdrawsItsRequest(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	suspendRun(t, s, time.Hour)

	cancelled, err := s.CancelIdleRun(ctx, "r1", "cancelled by bob", nil)

	require.NoError(t, err)
	assert.True(t, cancelled, "a waiting run is cancelled in the store")
	run, err := s.Run(ctx, ws, "r1")
	require.NoError(t, err)
	assert.Equal(t, store.RunCancelled, run.Status)
	latest, _, err := s.LatestApproval(ctx, "r1")
	require.NoError(t, err)
	assert.Equal(t, store.ApprovalWithdrawn, latest.Status)
	answered, err := s.AnswerApproval(ctx, ws, "r1", "a1", store.Answer{Approved: true})
	require.NoError(t, err)
	assert.False(t, answered, "a withdrawn request is not answered")
}

// TestCancelIdleRun_RecordsWhatDidNotRunBeforeItsEnd: what a cancelled run
// did not do is recorded in the cancel's own transaction, before the run's
// end, so its event stream, which ends with its end, holds it. What is
// recorded is read within that transaction, which sees the request
// withdrawn.
func TestCancelIdleRun_RecordsWhatDidNotRunBeforeItsEnd(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	suspendRun(t, s, time.Hour)
	failed := toolgateway.Record{RunID: "r1", CallID: "call-1", Event: toolgateway.EventApproval, Tool: "files_write", Decision: toolgateway.Deny, Reason: "approval failed: cancelled by bob"}
	results := store.NewMessage{Position: 0, Message: model.Message{Role: model.RoleUser, ToolResults: []model.ToolResult{{CallID: "call-1", Content: "not run", IsError: true}}}}

	cancelled, err := s.CancelIdleRun(ctx, "r1", "cancelled by bob", func(ctx context.Context, tx store.ClosingReader) (store.Closing, error) {
		a, ok, err := tx.LatestApproval(ctx, "r1")
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, store.ApprovalWithdrawn, a.Status)
		return store.Closing{Records: []toolgateway.Record{failed}, Messages: []store.NewMessage{results}}, nil
	})

	require.NoError(t, err)
	assert.True(t, cancelled)
	all, err := s.RunEvents(ctx, "r1", 0, 100)
	require.NoError(t, err)
	require.Len(t, all, 3)
	require.NotNil(t, all[1].Record)
	assert.Equal(t, failed, all[1].Record.Record)
	assert.True(t, all[2].Finished, "the run's end comes last")
	transcript, err := s.Transcript(ctx, "r1")
	require.NoError(t, err)
	require.Len(t, transcript, 1)
	assert.Equal(t, results.Message, transcript[0].Message)
}

// TestCancelIdleRun_GoesAheadWhenWhatDidNotRunCannotBeRead: stopping an
// agent comes first, so a run is cancelled even when what it did not do
// cannot be recorded; the error says so.
func TestCancelIdleRun_GoesAheadWhenWhatDidNotRunCannotBeRead(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	suspendRun(t, s, time.Hour)

	cancelled, err := s.CancelIdleRun(ctx, "r1", "cancelled by bob", func(context.Context, store.ClosingReader) (store.Closing, error) {
		return store.Closing{Records: []toolgateway.Record{{RunID: "r1", CallID: "x", Event: toolgateway.EventApproval, Decision: toolgateway.Deny}}}, errors.New("unreadable")
	})

	require.ErrorContains(t, err, "unreadable")
	assert.True(t, cancelled)
	run, err := s.Run(ctx, ws, "r1")
	require.NoError(t, err)
	assert.Equal(t, store.RunCancelled, run.Status)
	all, err := s.RunEvents(ctx, "r1", 0, 100)
	require.NoError(t, err)
	require.Len(t, all, 2, "the approval request and the run's end, nothing of what failed")
	assert.True(t, all[1].Finished)

	again, err := s.CancelIdleRun(ctx, "r1", "again", func(context.Context, store.ClosingReader) (store.Closing, error) {
		t.Error("a run that is not idle has nothing to close out")
		return store.Closing{}, nil
	})
	require.NoError(t, err)
	assert.False(t, again)
}

func TestIdleRuns_ListsQueuedAndWaitingRuns(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	suspendRun(t, s, time.Hour)
	v, err := s.PutHarness(ctx, ws, "alice", notes())
	require.NoError(t, err)
	createRun(t, s, store.NewRun{ID: "r2", HarnessVersionID: v.ID, Input: "x", StartedBy: "alice"})

	idle, err := s.IdleRuns(ctx)

	require.NoError(t, err)
	assert.Equal(t, []store.IdleRun{
		{ID: "r1", Status: store.RunWaiting, Workspace: ws, Harness: "notes", Owner: "alice"},
		{ID: "r2", Status: store.RunQueued, Workspace: ws, Harness: "notes", Owner: "alice"},
	}, idle)
}
