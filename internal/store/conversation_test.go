package store_test

import (
	"context"
	"strings"
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

func TestConversations(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	v := newRun(t, s, "a1")
	require.NoError(t, s.FinishRun(ctx, "a1", store.RunSucceeded, "ok", 1, ""))
	createRun(t, s, store.NewRun{ID: "a2", HarnessVersionID: v.ID, Input: "more", StartedBy: "bob", Follows: "a1"})
	claim(t, s, "a2")
	require.NoError(t, s.FinishRun(ctx, "a2", store.RunSucceeded, "ok", 1, ""))
	createRun(t, s, store.NewRun{ID: "b1", HarnessVersionID: v.ID, Input: "bob's own", StartedBy: "bob"})
	claim(t, s, "b1")
	createRun(t, s, store.NewRun{ID: "c1", HarnessVersionID: v.ID, Input: strings.Repeat("é", 150), StartedBy: "alice"})
	claim(t, s, "c1")
	// A follow-up just sent, still queued, is its conversation's latest run.
	createRun(t, s, store.NewRun{ID: "a3", HarnessVersionID: v.ID, Input: "again", StartedBy: "alice", Follows: "a2"})
	work, err := s.PutHarness(ctx, "work", "alice", notes())
	require.NoError(t, err)
	createRun(t, s, store.NewRun{ID: "w1", HarnessVersionID: work.ID, Input: "elsewhere", StartedBy: "alice"})

	list := func(f store.ConversationFilter) []store.ConversationSummary {
		t.Helper()
		out, err := s.Conversations(ctx, f)
		require.NoError(t, err)
		return out
	}
	ids := func(list []store.ConversationSummary) []string {
		out := make([]string, len(list))
		for i, c := range list {
			out[i] = c.ID
		}
		return out
	}

	home := list(store.ConversationFilter{User: "alice", Workspaces: []string{ws}, Limit: 10})
	require.Equal(t, []string{"a1", "c1"}, ids(home), "one item per conversation alice started, latest activity first")
	a3, err := s.Run(ctx, ws, "a3")
	require.NoError(t, err)
	assert.Equal(t, store.ConversationSummary{ID: "a1", Workspace: ws, Harness: "notes", Title: "tidy", Status: store.RunQueued, UpdatedAt: a3.CreatedAt}, home[0],
		"the first run's input is the title, the latest run's creation the latest activity")
	assert.Equal(t, strings.Repeat("é", 100), home[1].Title, "a title is cut to 100 characters")

	for _, tt := range []struct {
		name   string
		filter store.ConversationFilter
		want   []string
	}{
		{"a page", store.ConversationFilter{User: "alice", Workspaces: []string{ws}, Limit: 1}, []string{"a1"}},
		{"the next page", store.ConversationFilter{User: "alice", Workspaces: []string{ws}, BeforeAt: home[0].UpdatedAt, BeforeID: "a1", Limit: 10}, []string{"c1"}},
		{"every workspace given", store.ConversationFilter{User: "alice", Workspaces: []string{ws, "work"}, Limit: 10}, []string{"w1", "a1", "c1"}},
		{"no workspace", store.ConversationFilter{User: "alice", Limit: 10}, []string{}},
		{"a follow-up does not make a conversation the follower's", store.ConversationFilter{User: "bob", Workspaces: []string{ws}, Limit: 10}, []string{"b1"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ids(list(tt.filter)))
		})
	}

	_, err = s.Conversations(ctx, store.ConversationFilter{User: "alice", Workspaces: []string{ws}})
	require.Error(t, err, "a limit is required")

	found, err := s.FindConversation(ctx, "alice", []string{ws}, "a1")
	require.NoError(t, err)
	assert.Equal(t, store.ConversationSummary{ID: "a1", Workspace: ws, Harness: "notes", Title: "tidy", Status: store.RunQueued}, found)
	_, err = s.FindConversation(ctx, "alice", []string{ws}, "a2")
	require.ErrorIs(t, err, store.ErrNotFound, "a later run does not name a conversation")
	_, err = s.FindConversation(ctx, "alice", []string{ws}, "w1")
	require.ErrorIs(t, err, store.ErrNotFound, "a conversation outside the given workspaces is not found")
	_, err = s.FindConversation(ctx, "bob", []string{ws}, "a1")
	require.ErrorIs(t, err, store.ErrNotFound, "another user's conversation is not found")
}

func TestConversations_PagesStayStableAcrossFollowUps(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	v := newRun(t, s, "x1")
	for _, id := range []string{"x2", "x3"} {
		createRun(t, s, store.NewRun{ID: id, HarnessVersionID: v.ID, Input: "tidy", StartedBy: "alice"})
		claim(t, s, id)
	}
	for _, id := range []string{"x1", "x2", "x3"} {
		require.NoError(t, s.FinishRun(ctx, id, store.RunSucceeded, "ok", 1, ""))
	}
	filter := store.ConversationFilter{User: "alice", Workspaces: []string{ws}, Limit: 2}
	first, err := s.Conversations(ctx, filter)
	require.NoError(t, err)
	require.Len(t, first, 2)
	require.Equal(t, "x2", first[1].ID)

	// The page's last conversation goes on before the next page is read.
	createRun(t, s, store.NewRun{ID: "x4", HarnessVersionID: v.ID, Input: "more", StartedBy: "alice", Follows: "x2"})

	filter.BeforeAt, filter.BeforeID = first[1].UpdatedAt, first[1].ID
	next, err := s.Conversations(ctx, filter)
	require.NoError(t, err)
	require.Len(t, next, 1, "the next page neither repeats nor skips a conversation")
	assert.Equal(t, "x1", next[0].ID)
}
