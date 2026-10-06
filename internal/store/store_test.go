package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/harness"
	"github.com/jangraefen/agenty/internal/policy"
	"github.com/jangraefen/agenty/internal/store"
	"github.com/jangraefen/agenty/internal/store/storetest"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

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

	first, err := s.PutHarness(ctx, h)
	require.NoError(t, err)
	again, err := s.PutHarness(ctx, h)
	require.NoError(t, err)
	h.Instructions = "Tidy the notes, and sort them."
	second, err := s.PutHarness(ctx, h)
	require.NoError(t, err)

	assert.Equal(t, 1, first.Version)
	assert.Equal(t, first, again, "an unchanged harness is not stored again")
	assert.Equal(t, 2, second.Version)
	assert.NotEqual(t, first.ID, second.ID)

	latest, err := s.Harness(ctx, "notes")
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
	first, err := s.PutHarness(ctx, h)
	require.NoError(t, err)

	h.Tools, h.Policy = []string{}, []policy.Module{}
	again, err := s.PutHarness(ctx, h)

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
			v, err := s.PutHarness(ctx, h)
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

	_, err := s.PutHarness(context.Background(), h)

	require.ErrorContains(t, err, "limits.max_steps")
	_, err = s.Harness(context.Background(), "notes")
	require.ErrorIs(t, err, store.ErrNotFound, "nothing is stored")
}

func TestHarnesses_ListsTheLatestVersionOfEach(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	h := notes()
	_, err := s.PutHarness(ctx, h)
	require.NoError(t, err)
	h.Instructions = "v2"
	_, err = s.PutHarness(ctx, h)
	require.NoError(t, err)
	other := notes()
	other.Name = "agenda"
	_, err = s.PutHarness(ctx, other)
	require.NoError(t, err)

	all, err := s.Harnesses(ctx)

	require.NoError(t, err)
	require.Len(t, all, 2)
	assert.Equal(t, "agenda", all[0].Harness.Name)
	assert.Equal(t, "notes", all[1].Harness.Name)
	assert.Equal(t, 2, all[1].Version)
}

func TestHarness_NotFound(t *testing.T) {
	s := storetest.New(t)

	_, err := s.Harness(context.Background(), "ghost")
	require.ErrorIs(t, err, store.ErrNotFound)
	_, err = s.HarnessVersionByID(context.Background(), 42)
	require.ErrorIs(t, err, store.ErrNotFound)
}

// newRun stores the notes harness and a run of it.
func newRun(t *testing.T, s *store.Store, id string) store.HarnessVersion {
	t.Helper()
	v, err := s.PutHarness(context.Background(), notes())
	require.NoError(t, err)
	require.NoError(t, s.CreateRun(context.Background(), id, v.ID, "tidy"))
	return v
}

func TestRuns_Lifecycle(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	v := newRun(t, s, "r1")

	running, err := s.Run(ctx, "r1")
	require.NoError(t, err)
	assert.Equal(t, store.RunRunning, running.Status)
	assert.Equal(t, v.ID, running.HarnessVersionID)
	assert.Equal(t, "tidy", running.Input)
	assert.Nil(t, running.FinishedAt)

	require.NoError(t, s.FinishRun(ctx, "r1", store.RunSucceeded, "done", 3, ""))
	finished, err := s.Run(ctx, "r1")
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

	require.Error(t, s.CreateRun(ctx, "r1", 1, "again"), "run IDs are unique")
	require.Error(t, s.CreateRun(ctx, "r2", 999, "x"), "a run needs a stored harness version")
	require.ErrorContains(t, s.FinishRun(ctx, "r1", store.RunRunning, "", 0, ""), "cannot finish as running")
	require.ErrorIs(t, s.FinishRun(ctx, "ghost", store.RunFailed, "", 0, ""), store.ErrNotFound)
	_, err := s.Run(ctx, "ghost")
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestFailRunningRuns(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	newRun(t, s, "r1")
	require.NoError(t, s.CreateRun(ctx, "r2", 1, "tidy"))
	require.NoError(t, s.FinishRun(ctx, "r2", store.RunSucceeded, "ok", 1, ""))

	n, err := s.FailRunningRuns(ctx, "server restarted")

	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
	r1, err := s.Run(ctx, "r1")
	require.NoError(t, err)
	assert.Equal(t, store.RunFailed, r1.Status)
	assert.Equal(t, "server restarted", r1.Error)
	r2, err := s.Run(ctx, "r2")
	require.NoError(t, err)
	assert.Equal(t, store.RunSucceeded, r2.Status, "finished runs are left alone")
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

func TestRecord_KeepsJSONAsWritten(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	newRun(t, s, "r1")
	args := `{"b": 1, "a": 2, "a": 3}`

	require.NoError(t, s.Record(ctx, toolgateway.Record{RunID: "r1", CallID: "c1", Event: toolgateway.EventDecision, Tool: "files_read", Args: json.RawMessage(args), Decision: toolgateway.Allow}))

	got, err := s.AuditRecords(ctx, "r1")
	require.NoError(t, err)
	assert.Equal(t, args, string(got[0].Args), "the audit keeps key order, spacing and duplicate keys as the model sent them")
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
	require.ErrorContains(t, err, "audit", "an unreachable database is an error")
	_, err = s.AuditRecords(ctx, "r1")
	require.Error(t, err)
	_, err = s.Harnesses(ctx)
	require.Error(t, err)
	_, err = s.FailRunningRuns(ctx, "x")
	require.Error(t, err)
	_, err = s.PutHarness(ctx, notes())
	require.Error(t, err)
	require.Error(t, s.FinishRun(ctx, "r1", store.RunFailed, "", 0, ""))
	require.Error(t, s.CreateRun(ctx, "r9", 1, ""))
	_, err = s.Run(ctx, "r1")
	require.Error(t, err)
	assert.NotErrorIs(t, err, store.ErrNotFound)
}
