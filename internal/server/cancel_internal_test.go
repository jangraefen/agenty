package server

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/config"
	"github.com/jangraefen/agenty/internal/harness"
	"github.com/jangraefen/agenty/internal/secret"
	"github.com/jangraefen/agenty/internal/store"
	"github.com/jangraefen/agenty/internal/store/storetest"
)

// TestCancelRun_AFinishedRunWithAHubIsNotCancelled: a run that finished
// before its cancel read it, though its hub is not let go of yet, is not
// cancelled: the cancel is refused, and no request is recorded for it.
func TestCancelRun_AFinishedRunWithAHubIsNotCancelled(t *testing.T) {
	const token = "alice-token-0123456789abcdef0123456789"
	ctx := context.Background()
	st := storetest.New(t)
	r, err := secret.NewRedactor([]string{token})
	require.NoError(t, err)
	s, err := New(ctx, Config{
		Store: st,
		Operator: &config.Config{
			Users:      map[string]config.User{"alice": {}},
			Workspaces: map[string]config.Workspace{"home": {Members: []string{"alice"}}},
		},
		Resolved: &config.Resolved{Redactor: r, UserTokens: map[string]string{"alice": token}},
		Logger:   slog.New(slog.DiscardHandler),
	})
	require.NoError(t, err)
	t.Cleanup(s.Close)
	v, err := st.PutHarness(ctx, "home", "alice", harness.Harness{
		Name:         "notes",
		Instructions: "Tidy the notes.",
		Model:        harness.Model{Provider: "anthropic", Name: "claude-test"},
		Limits:       harness.Limits{MaxSteps: 1, MaxToolCalls: 1},
	})
	require.NoError(t, err)
	_, err = st.CreateRun(ctx, store.NewRun{ID: "r1", HarnessVersionID: v.ID, Input: "tidy", StartedBy: "alice"})
	require.NoError(t, err)
	_, ok, err := st.ClaimRun(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, st.FinishRun(ctx, "r1", store.RunSucceeded, "done", 1, ""))
	_, err = s.register("r1", "home", "notes")
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/workspaces/home/runs/r1/cancel", nil)
	req.Host = "127.0.0.1"
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	s.Handler().ServeHTTP(resp, req)

	assert.Equal(t, http.StatusConflict, resp.Code)
	assert.Contains(t, resp.Body.String(), "has already finished")
	events, err := st.ListAuditEvents(ctx, store.EventFilter{RunID: "r1", Action: "run.cancel_requested", Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, events, "no cancel is requested of a finished run")
	run, err := st.Run(ctx, "home", "r1")
	require.NoError(t, err)
	assert.Equal(t, store.RunSucceeded, run.Status)
}
