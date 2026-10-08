package store_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/store"
	"github.com/jangraefen/agenty/internal/store/storetest"
)

func TestRuns_Conversation(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	v := newRun(t, s, "r1")
	require.NoError(t, s.FinishRun(ctx, "r1", store.RunSucceeded, "ok", 1, ""))
	createRun(t, s, store.NewRun{ID: "r2", HarnessVersionID: v.ID, Input: "and then?", StartedBy: "bob", Follows: "r1"})
	claim(t, s, "r2")
	require.NoError(t, s.FinishRun(ctx, "r2", store.RunSucceeded, "ok", 1, ""))
	createRun(t, s, store.NewRun{ID: "r3", HarnessVersionID: v.ID, Input: "thanks", StartedBy: "alice", Follows: "r2"})
	claim(t, s, "r3")
	require.NoError(t, s.SetRunDigests(ctx, "r3", "d3", "h3"))
	newRun(t, s, "other")

	for _, id := range []string{"r1", "r2", "r3"} {
		runs, err := s.Conversation(ctx, ws, id)
		require.NoError(t, err)
		ids := make([]string, len(runs))
		for i, r := range runs {
			ids[i] = r.ID
		}
		assert.Equal(t, []string{"r1", "r2", "r3"}, ids, "every run of a conversation finds all of it, oldest first")
	}

	first, err := s.Run(ctx, ws, "r1")
	require.NoError(t, err)
	assert.Equal(t, "r1", first.ConversationID, "a conversation is named by its first run")
	assert.Empty(t, first.Follows)
	third, err := s.Run(ctx, ws, "r3")
	require.NoError(t, err)
	assert.Equal(t, "r1", third.ConversationID)
	assert.Equal(t, "r2", third.Follows)
	assert.Equal(t, "thanks", third.Input)
	assert.Equal(t, "d3", third.PromptDigest)
	assert.Equal(t, "h3", third.HistoryDigest)
	assert.Empty(t, first.PromptDigest, "a run stored without a prompt digest has none")

	alone, err := s.Conversation(ctx, ws, "other")
	require.NoError(t, err)
	require.Len(t, alone, 1)
	assert.Equal(t, "other", alone[0].ConversationID)
}

func TestRuns_ConversationErrors(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	v := newRun(t, s, "r1")
	require.NoError(t, s.FinishRun(ctx, "r1", store.RunSucceeded, "ok", 1, ""))
	createRun(t, s, store.NewRun{ID: "r2", HarnessVersionID: v.ID, Input: "x", StartedBy: "bob", Follows: "r1"})

	_, err := s.CreateRun(ctx, store.NewRun{ID: "r3", HarnessVersionID: v.ID, Input: "y", StartedBy: "alice", Follows: "r1"})
	require.ErrorIs(t, err, store.ErrConflict, "a run is followed by one run at most, so a conversation never branches")
	_, err = s.Run(ctx, ws, "r3")
	require.ErrorIs(t, err, store.ErrNotFound)

	_, err = s.CreateRun(ctx, store.NewRun{ID: "r4", HarnessVersionID: v.ID, Input: "y", StartedBy: "alice", Follows: "ghost"})
	require.Error(t, err, "a run follows a stored run")

	_, err = s.Conversation(ctx, ws, "ghost")
	require.ErrorIs(t, err, store.ErrNotFound)
	_, err = s.Conversation(ctx, "work", "r1")
	require.ErrorIs(t, err, store.ErrNotFound, "a conversation of another workspace is not found")
}
