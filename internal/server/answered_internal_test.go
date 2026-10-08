package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/harness"
	"github.com/jangraefen/agenty/internal/store"
	"github.com/jangraefen/agenty/internal/store/storetest"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

// TestAnswered_TakesUpOnlyAnAnswerNotUsedYet: a run is resumed only for an
// approval that was answered, approved or rejected, and whose answer its
// audit log does not hold yet, so no answer runs a call twice; it resumes
// with that audit log, which its gateway restores its call counts from.
func TestAnswered_TakesUpOnlyAnAnswerNotUsedYet(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	v, err := st.PutHarness(ctx, "home", "alice", harness.Harness{
		Name:         "notes",
		Instructions: "Tidy the notes.",
		Model:        harness.Model{Provider: "anthropic", Name: "claude-test"},
		Limits:       harness.Limits{MaxSteps: 1, MaxToolCalls: 2},
	})
	require.NoError(t, err)
	_, err = st.CreateRun(ctx, store.NewRun{ID: "r1", HarnessVersionID: v.ID, Input: "tidy", StartedBy: "alice"})
	require.NoError(t, err)
	// c0 waited and was answered, c1 waits.
	for _, rec := range []toolgateway.Record{
		{RunID: "r1", CallID: "c0", Event: toolgateway.EventDecision, Tool: "files_write", Decision: toolgateway.RequireApproval},
		{RunID: "r1", CallID: "c0", Event: toolgateway.EventApproval, Tool: "files_write", Decision: toolgateway.Allow, Approver: "alice"},
		{RunID: "r1", CallID: "c1", Event: toolgateway.EventDecision, Tool: "files_write", Decision: toolgateway.RequireApproval},
	} {
		require.NoError(t, st.Record(ctx, rec))
	}
	s := &Server{cfg: Config{Store: st}}
	approval := func(call string, status store.ApprovalStatus) store.Approval {
		return store.Approval{NewApproval: store.NewApproval{RunID: "r1", CallID: call}, Status: status}
	}

	for _, tt := range []struct {
		name     string
		approval store.Approval
		wantErr  string
	}{
		{"pending", approval("c1", store.ApprovalPending), "while its approval request is pending"},
		{"withdrawn", approval("c1", store.ApprovalWithdrawn), "while its approval request is withdrawn"},
		{"used already", approval("c0", store.ApprovalApproved), "for an approval it used"},
		{"approved", approval("c1", store.ApprovalApproved), ""},
		{"rejected", approval("c1", store.ApprovalRejected), ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			audit, err := s.answered(ctx, tt.approval)

			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				assert.Nil(t, audit)
				return
			}
			require.NoError(t, err)
			calls := make([]string, len(audit))
			for i, rec := range audit {
				calls[i] = rec.CallID + " " + string(rec.Event)
			}
			assert.Equal(t, []string{"c0 decision", "c0 approval", "c1 decision"}, calls, "the run resumes with its audit log")
		})
	}
}
