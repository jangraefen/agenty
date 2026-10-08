package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/auditlog"
	"github.com/jangraefen/agenty/internal/harness"
	"github.com/jangraefen/agenty/internal/policy"
	"github.com/jangraefen/agenty/internal/store"
	"github.com/jangraefen/agenty/internal/store/storetest"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

// ws is the workspace the tests store harnesses in.
const ws = "home"

func notes() harness.Harness {
	return harness.Harness{
		Name:         "notes",
		Instructions: "Tidy the notes.",
		Model:        harness.Model{Provider: "anthropic", Name: "claude-sonnet-5-5"},
		Tools:        []string{"files_read", "files_write"},
		Limits:       harness.Limits{MaxSteps: 5, MaxToolCalls: 10},
		Policy:       []policy.Module{policy.RulesModule("notes (inline policy)", `deny contains "no" if input.tool == "files_delete"`)},
	}
}

func TestOpen_MigratesAgain(t *testing.T) {
	_, url := storetest.NewWithURL(t)

	s, err := store.Open(context.Background(), url)

	require.NoError(t, err, "opening a migrated database applies nothing new")
	s.Close()
}

func TestOpen_Errors(t *testing.T) {
	_, err := store.Open(context.Background(), "not a url ::")
	require.Error(t, err)

	_, err = store.Open(context.Background(), "postgres://nobody@127.0.0.1:1/none?connect_timeout=1")
	require.ErrorContains(t, err, "migrate")
}

func TestPutHarness_StoresVersions(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	h := notes()

	first, err := s.PutHarness(ctx, ws, "alice", h)
	require.NoError(t, err)
	again, err := s.PutHarness(ctx, ws, "alice", h)
	require.NoError(t, err)
	h.Instructions = "Tidy the notes, and sort them."
	second, err := s.PutHarness(ctx, ws, "alice", h)
	require.NoError(t, err)

	assert.Equal(t, 1, first.Version)
	assert.Equal(t, first, again, "an unchanged harness is not stored again")
	assert.Equal(t, 2, second.Version)
	assert.NotEqual(t, first.ID, second.ID)
	assert.Equal(t, ws, second.Workspace)

	latest, err := s.Harness(ctx, ws, "notes")
	require.NoError(t, err)
	assert.Equal(t, second, latest)
	assert.Equal(t, h, latest.Harness, "the harness round-trips, policy modules included")

	old, err := s.HarnessVersionByID(ctx, first.ID)
	require.NoError(t, err)
	assert.Equal(t, notes(), old.Harness, "earlier versions stay as they were")
}

func TestPutHarness_EmptyAndMissingListsAreTheSame(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	h := notes()
	h.Tools, h.Policy = nil, nil
	first, err := s.PutHarness(ctx, ws, "alice", h)
	require.NoError(t, err)

	h.Tools, h.Policy = []string{}, []policy.Module{}
	again, err := s.PutHarness(ctx, ws, "alice", h)

	require.NoError(t, err)
	assert.Equal(t, first.Version, again.Version)
}

func TestPutHarness_ConcurrentPutsGetTheirOwnVersions(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	const n = 8
	versions := make(chan int, n)
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			h := notes()
			h.Instructions = fmt.Sprintf("Tidy the notes, take %d.", i)
			v, err := s.PutHarness(ctx, ws, "alice", h)
			errs <- err
			versions <- v.Version
		})
	}
	wg.Wait()
	close(errs)
	close(versions)

	for err := range errs {
		require.NoError(t, err)
	}
	var got []int
	for v := range versions {
		got = append(got, v)
	}
	assert.ElementsMatch(t, []int{1, 2, 3, 4, 5, 6, 7, 8}, got)
}

func TestPutHarness_RejectsInvalidHarnesses(t *testing.T) {
	s := storetest.New(t)
	h := notes()
	h.Limits.MaxSteps = 0

	_, err := s.PutHarness(context.Background(), ws, "alice", h)

	require.ErrorContains(t, err, "limits.max_steps")
	_, err = s.Harness(context.Background(), ws, "notes")
	require.ErrorIs(t, err, store.ErrNotFound, "nothing is stored")
}

func TestHarnesses_ListsTheLatestVersionOfEach(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	h := notes()
	_, err := s.PutHarness(ctx, ws, "alice", h)
	require.NoError(t, err)
	h.Instructions = "v2"
	_, err = s.PutHarness(ctx, ws, "alice", h)
	require.NoError(t, err)
	other := notes()
	other.Name = "agenda"
	_, err = s.PutHarness(ctx, ws, "alice", other)
	require.NoError(t, err)

	all, err := s.Harnesses(ctx, ws)

	require.NoError(t, err)
	require.Len(t, all, 2)
	assert.Equal(t, "agenda", all[0].Harness.Name)
	assert.Equal(t, "notes", all[1].Harness.Name)
	assert.Equal(t, 2, all[1].Version)
}

func TestHarness_NotFound(t *testing.T) {
	s := storetest.New(t)

	_, err := s.Harness(context.Background(), ws, "ghost")
	require.ErrorIs(t, err, store.ErrNotFound)
	_, err = s.HarnessVersionByID(context.Background(), 42)
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestWorkspaces_AreSeparate(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	v := newRun(t, s, "r1")
	other := notes()
	other.Instructions = "Something else."

	theirs, err := s.PutHarness(ctx, "work", "alice", other)
	require.NoError(t, err)

	assert.Equal(t, 1, theirs.Version, "a harness name is versioned per workspace")
	assert.Equal(t, "work", theirs.Workspace)
	mine, err := s.Harness(ctx, ws, "notes")
	require.NoError(t, err)
	assert.Equal(t, v, mine, "another workspace's harness of the same name is a different harness")
	all, err := s.Harnesses(ctx, "work")
	require.NoError(t, err)
	assert.Equal(t, []store.HarnessVersion{theirs}, all)
	_, err = s.Harness(ctx, "empty", "notes")
	require.ErrorIs(t, err, store.ErrNotFound)
	none, err := s.Harnesses(ctx, "empty")
	require.NoError(t, err)
	assert.Empty(t, none)
	_, err = s.Run(ctx, "work", "r1")
	require.ErrorIs(t, err, store.ErrNotFound, "a run is found only in its harness's workspace")
}

// newRun stores the notes harness and a run of it.
// newRun stores a run of the notes harness and claims it, so it is running.
func newRun(t *testing.T, s *store.Store, id string) store.HarnessVersion {
	t.Helper()
	v, err := s.PutHarness(context.Background(), ws, "alice", notes())
	require.NoError(t, err)
	createRun(t, s, store.NewRun{ID: id, HarnessVersionID: v.ID, Input: "tidy", StartedBy: "alice"})
	claim(t, s, id)
	return v
}

// createRun stores r, which must succeed.
func createRun(t *testing.T, s *store.Store, r store.NewRun) store.Run {
	t.Helper()
	run, err := s.CreateRun(context.Background(), r)
	require.NoError(t, err)
	return run
}

// createErr returns the error of storing r.
func createErr(s *store.Store, r store.NewRun) error {
	_, err := s.CreateRun(context.Background(), r)
	return err
}

// claim claims the oldest queued run, which must be the run id.
func claim(t *testing.T, s *store.Store, id string) {
	t.Helper()
	claimed, ok, err := s.ClaimRun(context.Background())
	require.NoError(t, err)
	require.True(t, ok, "no run is queued")
	require.Equal(t, id, claimed.ID)
}

func TestRuns_Lifecycle(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	v, err := s.PutHarness(ctx, ws, "alice", notes())
	require.NoError(t, err)
	created := createRun(t, s, store.NewRun{ID: "r1", HarnessVersionID: v.ID, Input: "tidy", StartedBy: "alice"})
	queued, err := s.Run(ctx, ws, "r1")
	require.NoError(t, err)
	assert.Equal(t, queued, created, "a run is returned as stored")
	assert.Equal(t, store.RunQueued, queued.Status, "a run is stored as queued")
	assert.Nil(t, queued.FinishedAt)
	require.ErrorIs(t, s.SetRunDigests(ctx, "r1", "d", "h"), store.ErrNotFound, "a queued run has sent the model nothing")
	require.ErrorIs(t, s.FinishRun(ctx, "r1", store.RunSucceeded, "", 0, ""), store.ErrNotFound, "a queued run has not run")

	claimed, ok, err := s.ClaimRun(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, store.ClaimedRun{ID: "r1", Workspace: ws}, claimed)
	require.NoError(t, s.SetRunDigests(ctx, "r1", "d1", "h1"))

	running, err := s.Run(ctx, ws, "r1")
	require.NoError(t, err)
	assert.Equal(t, store.RunRunning, running.Status)
	assert.Equal(t, "d1", running.PromptDigest)
	assert.Equal(t, "h1", running.HistoryDigest)
	assert.Equal(t, v.ID, running.HarnessVersionID)
	assert.Equal(t, "tidy", running.Input)
	assert.Equal(t, "alice", running.StartedBy)
	assert.Equal(t, "notes", running.Harness)
	assert.Equal(t, 1, running.HarnessVersion)
	assert.Nil(t, running.FinishedAt)

	require.NoError(t, s.FinishRun(ctx, "r1", store.RunSucceeded, "done", 3, ""))
	finished, err := s.Run(ctx, ws, "r1")
	require.NoError(t, err)
	assert.Equal(t, store.RunSucceeded, finished.Status)
	assert.Equal(t, "done", finished.Output)
	assert.Equal(t, 3, finished.Steps)
	assert.NotNil(t, finished.FinishedAt)

	err = s.FinishRun(ctx, "r1", store.RunFailed, "", 3, "again")
	require.ErrorIs(t, err, store.ErrNotFound, "a finished run stays finished")
}

func TestRuns_Errors(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	newRun(t, s, "r1")

	require.Error(t, createErr(s, store.NewRun{ID: "r1", HarnessVersionID: 1, Input: "again", StartedBy: "alice"}), "run IDs are unique")
	require.Error(t, createErr(s, store.NewRun{ID: "r2", HarnessVersionID: 999, Input: "x", StartedBy: "alice"}), "a run needs a stored harness version")
	require.ErrorContains(t, s.FinishRun(ctx, "r1", store.RunRunning, "", 0, ""), "cannot finish as running")
	require.ErrorContains(t, s.FinishRun(ctx, "r1", store.RunQueued, "", 0, ""), "cannot finish as queued")
	require.ErrorIs(t, s.FinishRun(ctx, "ghost", store.RunFailed, "", 0, ""), store.ErrNotFound)
	_, err := s.Run(ctx, ws, "ghost")
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestRuns_Cancelled(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	newRun(t, s, "r1")

	require.NoError(t, s.FinishRun(ctx, "r1", store.RunCancelled, "", 1, "cancelled by alice"))

	r, err := s.Run(ctx, ws, "r1")
	require.NoError(t, err)
	assert.Equal(t, store.RunCancelled, r.Status)
	assert.Equal(t, "cancelled by alice", r.Error)
}

func TestAuditRuns(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	v := newRun(t, s, "r1")
	other := notes()
	other.Name = "agenda"
	ov, err := s.PutHarness(ctx, ws, "alice", other)
	require.NoError(t, err)
	theirs, err := s.PutHarness(ctx, "work", "alice", notes())
	require.NoError(t, err)
	for _, r := range []store.NewRun{
		{ID: "r2", HarnessVersionID: ov.ID, Input: "x", StartedBy: "bob"},
		{ID: "w1", HarnessVersionID: theirs.ID, Input: "x", StartedBy: "bob"},
		{ID: "r3", HarnessVersionID: v.ID, Input: "x", StartedBy: "alice"},
		{ID: "r4", HarnessVersionID: ov.ID, Input: "x", StartedBy: "alice"},
	} {
		createRun(t, s, r)
	}
	claim(t, s, "r2")
	claim(t, s, "w1")
	claim(t, s, "r3")
	require.NoError(t, s.FinishRun(ctx, "r3", store.RunSucceeded, "ok", 1, ""))

	ids := func(f store.RunFilter) []string {
		t.Helper()
		runs, err := s.AuditRuns(ctx, f)
		require.NoError(t, err)
		out := make([]string, len(runs))
		for i, r := range runs {
			out[i] = r.ID
		}
		return out
	}

	tests := []struct {
		name   string
		filter store.RunFilter
		want   []string
	}{
		{"every workspace's, newest first", store.RunFilter{Limit: 10}, []string{"r4", "r3", "w1", "r2", "r1"}},
		{"a page", store.RunFilter{Limit: 2}, []string{"r4", "r3"}},
		{"the next page", store.RunFilter{Limit: 2, Before: "r3"}, []string{"w1", "r2"}},
		{"after the last", store.RunFilter{Limit: 2, Before: "r1"}, []string{}},
		{"of one workspace", store.RunFilter{Limit: 10, Workspace: "work"}, []string{"w1"}},
		{"of one harness", store.RunFilter{Limit: 10, Harness: "agenda"}, []string{"r4", "r2"}},
		{"of one harness, in every workspace", store.RunFilter{Limit: 10, Harness: "notes"}, []string{"r3", "w1", "r1"}},
		{"started by one user", store.RunFilter{Limit: 10, StartedBy: "bob"}, []string{"w1", "r2"}},
		{"with one status", store.RunFilter{Limit: 10, Status: store.RunSucceeded}, []string{"r3"}},
		{"all of them", store.RunFilter{Limit: 10, Workspace: ws, Harness: "notes", StartedBy: "alice", Status: store.RunRunning}, []string{"r1"}},
		{"before a run that does not exist", store.RunFilter{Limit: 10, Before: "ghost"}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ids(tt.filter))
		})
	}
	runs, err := s.AuditRuns(ctx, store.RunFilter{Limit: 1})
	require.NoError(t, err)
	assert.Equal(t, "agenda", runs[0].Harness, "a listed run names its harness")
	assert.Equal(t, ws, runs[0].Workspace, "and its workspace")
	_, err = s.AuditRuns(ctx, store.RunFilter{})
	require.ErrorContains(t, err, "limit")

	found, err := s.AuditRun(ctx, "w1")
	require.NoError(t, err)
	assert.Equal(t, "work", found.Workspace, "a run is found in any workspace")
	assert.Equal(t, "bob", found.StartedBy)
	_, err = s.AuditRun(ctx, "ghost")
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestOwnRun(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	v := newRun(t, s, "a1")
	require.NoError(t, s.FinishRun(ctx, "a1", store.RunSucceeded, "ok", 1, ""))
	createRun(t, s, store.NewRun{ID: "a2", HarnessVersionID: v.ID, Input: "more", StartedBy: "alice", Follows: "a1"})

	for _, id := range []string{"a1", "a2"} {
		run, err := s.OwnRun(ctx, ws, "alice", id)
		require.NoError(t, err, id)
		assert.Equal(t, id, run.ID)
	}
	for _, tt := range []struct{ name, workspace, user, id string }{
		{"another member's", ws, "bob", "a1"},
		{"another member's follow-up", ws, "bob", "a2"},
		{"in another workspace", "work", "alice", "a1"},
		{"that does not exist", ws, "alice", "ghost"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.OwnRun(ctx, tt.workspace, tt.user, tt.id)
			require.ErrorIs(t, err, store.ErrNotFound)
		})
	}
}

func TestFailRunningRuns(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	newRun(t, s, "r1")
	createRun(t, s, store.NewRun{ID: "r2", HarnessVersionID: 1, Input: "tidy", StartedBy: "alice"})
	claim(t, s, "r2")
	require.NoError(t, s.FinishRun(ctx, "r2", store.RunSucceeded, "ok", 1, ""))
	createRun(t, s, store.NewRun{ID: "r3", HarnessVersionID: 1, Input: "tidy", StartedBy: "alice"})

	n, err := s.FailRunningRuns(ctx, "server restarted")

	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
	r1, err := s.Run(ctx, ws, "r1")
	require.NoError(t, err)
	assert.Equal(t, store.RunFailed, r1.Status)
	assert.Equal(t, "server restarted", r1.Error)
	r2, err := s.Run(ctx, ws, "r2")
	require.NoError(t, err)
	assert.Equal(t, store.RunSucceeded, r2.Status, "finished runs are left alone")
	r3, err := s.Run(ctx, ws, "r3")
	require.NoError(t, err)
	assert.Equal(t, store.RunQueued, r3.Status, "queued runs wait for the next server")
}

func TestClaimRun_ClaimsOldestFirst(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	v, err := s.PutHarness(ctx, ws, "alice", notes())
	require.NoError(t, err)
	theirs, err := s.PutHarness(ctx, "work", "alice", notes())
	require.NoError(t, err)
	for _, r := range []store.NewRun{
		{ID: "r1", HarnessVersionID: v.ID, Input: "x", StartedBy: "alice"},
		{ID: "w1", HarnessVersionID: theirs.ID, Input: "x", StartedBy: "bob"},
		{ID: "r2", HarnessVersionID: v.ID, Input: "x", StartedBy: "alice"},
	} {
		createRun(t, s, r)
	}
	queued, err := s.IdleRuns(ctx)
	require.NoError(t, err)
	assert.Equal(t, []store.IdleRun{
		{ID: "r1", Status: store.RunQueued, Workspace: ws, Harness: "notes", Owner: "alice"},
		{ID: "w1", Status: store.RunQueued, Workspace: "work", Harness: "notes", Owner: "bob"},
		{ID: "r2", Status: store.RunQueued, Workspace: ws, Harness: "notes", Owner: "alice"},
	}, queued, "with who started each run's conversation")

	var got []store.ClaimedRun
	for {
		claimed, ok, err := s.ClaimRun(ctx)
		require.NoError(t, err)
		if !ok {
			break
		}
		got = append(got, claimed)
	}

	assert.Equal(t, []store.ClaimedRun{{ID: "r1", Workspace: ws}, {ID: "w1", Workspace: "work"}, {ID: "r2", Workspace: ws}}, got)
	queued, err = s.IdleRuns(ctx)
	require.NoError(t, err)
	assert.Empty(t, queued)
}

func TestClaimRun_ConcurrentClaimsClaimEachRunOnce(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	v, err := s.PutHarness(ctx, ws, "alice", notes())
	require.NoError(t, err)
	const runs = 20
	for i := range runs {
		createRun(t, s, store.NewRun{ID: fmt.Sprintf("r%02d", i), HarnessVersionID: v.ID, Input: "x", StartedBy: "alice"})
	}

	var (
		mu      sync.Mutex
		claimed []string
		errs    []error
		wg      sync.WaitGroup
	)
	for range 8 {
		wg.Go(func() {
			for {
				c, ok, err := s.ClaimRun(ctx)
				mu.Lock()
				switch {
				case err != nil:
					errs = append(errs, err)
				case ok:
					claimed = append(claimed, c.ID)
				}
				mu.Unlock()
				if err != nil || !ok {
					return
				}
			}
		})
	}
	wg.Wait()
	require.Empty(t, errs)

	slices.Sort(claimed)
	want := make([]string, runs)
	for i := range want {
		want[i] = fmt.Sprintf("r%02d", i)
	}
	assert.Equal(t, want, claimed, "every run is claimed, and once")
}

// TestClaimRun_SkipsARunAnotherClaimHolds: a claim never waits for another:
// it takes the next queued run while the oldest is locked.
func TestClaimRun_SkipsARunAnotherClaimHolds(t *testing.T) {
	ctx := context.Background()
	s, url := storetest.NewWithURL(t)
	v, err := s.PutHarness(ctx, ws, "alice", notes())
	require.NoError(t, err)
	createRun(t, s, store.NewRun{ID: "r1", HarnessVersionID: v.ID, Input: "x", StartedBy: "alice"})
	createRun(t, s, store.NewRun{ID: "r2", HarnessVersionID: v.ID, Input: "x", StartedBy: "alice"})
	conn, err := pgx.Connect(ctx, url)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, conn.Close(ctx)) })
	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, tx.Rollback(ctx)) })
	_, err = tx.Exec(ctx, "SELECT id FROM runs WHERE id = 'r1' FOR NO KEY UPDATE")
	require.NoError(t, err)
	timeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	claimed, ok, err := s.ClaimRun(timeout)

	require.NoError(t, err, "the claim does not wait for the lock")
	require.True(t, ok)
	assert.Equal(t, "r2", claimed.ID)
}

func TestCancelIdleRun(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	newRun(t, s, "running")
	createRun(t, s, store.NewRun{ID: "queued", HarnessVersionID: 1, Input: "x", StartedBy: "alice"})

	cancelled, err := s.CancelIdleRun(ctx, "queued", "cancelled by bob")
	require.NoError(t, err)
	assert.True(t, cancelled)
	r, err := s.Run(ctx, ws, "queued")
	require.NoError(t, err)
	assert.Equal(t, store.RunCancelled, r.Status)
	assert.Equal(t, "cancelled by bob", r.Error)
	assert.NotNil(t, r.FinishedAt)
	_, ok, err := s.ClaimRun(ctx)
	require.NoError(t, err)
	assert.False(t, ok, "a cancelled run is not claimed")

	for _, id := range []string{"queued", "running", "ghost"} {
		cancelled, err := s.CancelIdleRun(ctx, id, "again")
		require.NoError(t, err)
		assert.False(t, cancelled, "only a queued run is cancelled in the store: %s", id)
	}
	r, err = s.Run(ctx, ws, "running")
	require.NoError(t, err)
	assert.Equal(t, store.RunRunning, r.Status)
}

func TestRecord_StoresTheAuditLog(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	newRun(t, s, "r1")
	records := []toolgateway.Record{
		{RunID: "r1", CallID: "c1", Event: toolgateway.EventDecision, Tool: "files_write", Args: json.RawMessage(`{"path":"a.md"}`), Decision: toolgateway.RequireApproval, Reason: "policy: writes need a human"},
		{RunID: "r1", CallID: "c1", Event: toolgateway.EventApproval, Tool: "files_write", Args: json.RawMessage(`{"path":"a.md"}`), Decision: toolgateway.Allow, Reason: "approved", Approver: "alice"},
		{RunID: "r1", CallID: "c1", Event: toolgateway.EventResult, Tool: "files_write", Args: json.RawMessage(`{"path":"a.md"}`), Decision: toolgateway.Allow, Reason: "approved", Approver: "alice", Result: json.RawMessage(`{"ok":true}`)},
		{RunID: "r1", CallID: "c2", Event: toolgateway.EventResult, Tool: "files_read", Decision: toolgateway.Allow, Err: "no such file"},
	}
	for _, rec := range records {
		require.NoError(t, s.Record(ctx, rec))
	}

	got, err := s.AuditRecords(ctx, "r1")

	require.NoError(t, err)
	require.Len(t, got, len(records))
	for i, rec := range records {
		assert.False(t, got[i].RecordedAt.IsZero())
		assert.Equal(t, rec.RunID, got[i].RunID)
		assert.Equal(t, rec.CallID, got[i].CallID)
		assert.Equal(t, rec.Event, got[i].Event)
		assert.Equal(t, rec.Decision, got[i].Decision)
		assert.Equal(t, rec.Reason, got[i].Reason)
		assert.Equal(t, rec.Approver, got[i].Approver)
		assert.Equal(t, rec.Err, got[i].Err)
		if rec.Args == nil {
			assert.Nil(t, got[i].Args)
		} else {
			assert.JSONEq(t, string(rec.Args), string(got[i].Args))
		}
	}
	assert.JSONEq(t, `{"ok":true}`, string(got[2].Result))
}

// TestRecord_NeverFailsOnContent: PostgreSQL rejects NUL bytes and invalid
// UTF-8 in text, and the \u0000 escape in jsonb. A result is recorded after
// its call ran, so the record must be kept whatever a tool or model returned.
func TestRecord_NeverFailsOnContent(t *testing.T) {
	tests := []struct {
		name       string
		rec        toolgateway.Record
		wantArgs   string
		wantResult string
		wantErr    string
		wantReason string
	}{
		{
			name:     "arguments that are not JSON become a JSON string",
			rec:      toolgateway.Record{Args: json.RawMessage(`{"path":`)},
			wantArgs: `"{\"path\":"`,
		},
		{
			name:       "an escaped NUL in JSON is kept",
			rec:        toolgateway.Record{Args: json.RawMessage(`{"a":"\u0000"}`), Result: json.RawMessage(`{"content":"bin\u0000ary"}`)},
			wantArgs:   `{"a":"\u0000"}`,
			wantResult: `{"content":"bin\u0000ary"}`,
		},
		{
			name:       "a raw NUL makes JSON invalid, so it is kept escaped in a string",
			rec:        toolgateway.Record{Result: json.RawMessage("\"bin\x00ary\"")},
			wantResult: `"\"bin\u0000ary\""`,
		},
		{
			name:       "invalid UTF-8 in JSON is replaced",
			rec:        toolgateway.Record{Result: json.RawMessage("\"caf\xff\"")},
			wantResult: "\"caf\ufffd\"",
		},
		{
			name:       "NUL and invalid UTF-8 in text are replaced",
			rec:        toolgateway.Record{Err: "read \x00 failed: \xff", Reason: "policy: \x00"},
			wantErr:    "read \ufffd failed: \ufffd",
			wantReason: "policy: \ufffd",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			s := storetest.New(t)
			newRun(t, s, "r1")
			rec := tt.rec
			rec.RunID, rec.CallID, rec.Event, rec.Tool, rec.Decision = "r1", "c1", toolgateway.EventResult, "files_read", toolgateway.Allow

			require.NoError(t, s.Record(ctx, rec), "recording never fails on content")

			got, err := s.AuditRecords(ctx, "r1")
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, tt.wantArgs, string(got[0].Args))
			assert.Equal(t, tt.wantResult, string(got[0].Result))
			assert.Equal(t, tt.wantErr, got[0].Err)
			assert.Equal(t, tt.wantReason, got[0].Reason)
		})
	}
}

func TestRecord_KeepsJSONInItsOrder(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	newRun(t, s, "r1")
	args := `{"b": 1, "a": 2, "a": "<3>"}`

	require.NoError(t, s.Record(ctx, toolgateway.Record{RunID: "r1", CallID: "c1", Event: toolgateway.EventDecision, Tool: "files_read", Args: json.RawMessage(args), Decision: toolgateway.Allow}))

	got, err := s.AuditRecords(ctx, "r1")
	require.NoError(t, err)
	canonical, err := auditlog.Canonical([]byte(args))
	require.NoError(t, err)
	assert.Equal(t, string(canonical), string(got[0].Args),
		"the audit keeps key order and duplicate keys as the model sent them, in the canonical form the log's hashes cover")
}

// TestRecord_FailsClosed: a record the store cannot keep is an error, so the
// gateway does not execute the call (trust-model guarantee 6).
func TestRecord_FailsClosed(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	newRun(t, s, "r1")

	err := s.Record(ctx, toolgateway.Record{RunID: "ghost", CallID: "c1", Event: toolgateway.EventDecision, Tool: "files_read", Decision: toolgateway.Allow})
	require.ErrorContains(t, err, "audit", "records belong to a stored run")

	s.Close()
	err = s.Record(ctx, toolgateway.Record{RunID: "r1", CallID: "c1", Event: toolgateway.EventDecision, Tool: "files_read", Decision: toolgateway.Allow})
	require.Error(t, err, "an unreachable database is an error")
	_, err = s.AuditRecords(ctx, "r1")
	require.Error(t, err)
	_, err = s.Harnesses(ctx, ws)
	require.Error(t, err)
	_, err = s.FailRunningRuns(ctx, "x")
	require.Error(t, err)
	_, err = s.PutHarness(ctx, ws, "alice", notes())
	require.Error(t, err)
	require.Error(t, s.FinishRun(ctx, "r1", store.RunFailed, "", 0, ""))
	require.Error(t, createErr(s, store.NewRun{ID: "r9", HarnessVersionID: 1, Input: "", StartedBy: "alice"}))
	_, _, err = s.ClaimRun(ctx)
	require.Error(t, err)
	_, err = s.IdleRuns(ctx)
	require.Error(t, err)
	_, err = s.CancelIdleRun(ctx, "r1", "x")
	require.Error(t, err)
	require.Error(t, s.SuspendRun(ctx, store.NewApproval{ID: "a1", RunID: "r1"}))
	_, err = s.PendingApprovals(ctx, ws, "alice")
	require.Error(t, err)
	_, _, err = s.LatestApproval(ctx, "r1")
	require.Error(t, err)
	_, err = s.AnswerApproval(ctx, ws, "r1", "a1", store.Answer{})
	require.Error(t, err)
	_, err = s.ExpireApprovals(ctx, "x")
	require.Error(t, err)
	_, _, err = s.NextApprovalExpiry(ctx)
	require.Error(t, err)
	require.Error(t, s.SetRunDigests(ctx, "r1", "d", "h"))
	_, err = s.Run(ctx, ws, "r1")
	require.Error(t, err)
	assert.NotErrorIs(t, err, store.ErrNotFound)
}

func TestRecordAt_ReturnsWhenItRecorded(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	newRun(t, s, "r1")

	at, err := s.RecordAt(ctx, toolgateway.Record{RunID: "r1", CallID: "c1", Event: toolgateway.EventDecision, Tool: "files_read", Decision: toolgateway.Allow})

	require.NoError(t, err)
	records, err := s.AuditRecords(ctx, "r1")
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, records[0].RecordedAt, at)
}
