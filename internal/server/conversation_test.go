package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/api"
	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/model/modeltest"
	"github.com/jangraefen/agenty/internal/policy"
)

// finish waits until the run has finished and returns how it ended.
func (f *fixture) finish(t *testing.T, runID string) api.Run {
	t.Helper()
	events := f.events(t, runID).rest()
	require.NotEmpty(t, events)
	return decodeAs[api.Run](t, events[len(events)-1])
}

// followUp follows up the run as user, expecting it to start.
func (f *fixture) followUp(t *testing.T, bearer, runID, input string) api.Run {
	t.Helper()
	var run api.Run
	require.Equal(t, http.StatusCreated, f.doAs(t, bearer, http.MethodPost, home+"/runs/"+runID+"/follow-up", api.FollowUp{Input: input}, &run))
	return run
}

func TestFollowUp_ContinuesTheConversation(t *testing.T) {
	f := newFixture(t, options{})
	f.putNotes(t)
	f.script(modeltest.CallTools(call("c1", "files_read", `{"path":"notes.md"}`)), modeltest.Reply("- milk"))
	first := f.startRun(t, "tidy my notes")
	require.Equal(t, api.RunStatusSucceeded, f.finish(t, first.ID).Status)
	m := f.script(modeltest.CallTools(call("c2", "files_write", `{"path":"notes.md"}`)), modeltest.Reply("sorted"))

	second := f.followUp(t, bobToken, first.ID, "and sort them")
	finished := f.finish(t, second.ID)

	assert.Equal(t, first.ID, first.ConversationID, "a conversation is named by its first run")
	assert.Empty(t, first.Follows)
	assert.Equal(t, first.ID, second.Follows)
	assert.Equal(t, first.ID, second.ConversationID)
	assert.Equal(t, "bob", second.StartedBy, "any member may follow up")
	assert.Equal(t, "and sort them", second.Input)
	assert.Equal(t, first.HarnessVersionID, second.HarnessVersionID)
	assert.Equal(t, api.RunStatusSucceeded, finished.Status)
	assert.Equal(t, "sorted", finished.Output)
	assert.Equal(t, 2, finished.Steps, "steps count this run's model calls only")

	requests := m.Requests()
	require.Len(t, requests, 2)
	want := []model.Message{
		{Role: model.RoleUser, Text: "tidy my notes"},
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{call("c1", "files_read", `{"path":"notes.md"}`)}},
		{Role: model.RoleUser, ToolResults: []model.ToolResult{{CallID: "c1", Content: `{"content":"- milk"}`}}},
		{Role: model.RoleAssistant, Text: "- milk"},
		{Role: model.RoleUser, Text: "and sort them"},
	}
	assert.Equal(t, want, requests[0].Messages, "the model sees the conversation so far, then the new input")
	assert.Equal(t, 1, f.write.Calls)

	var transcript []api.TranscriptMessage
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/runs/"+second.ID+"/transcript", nil, &transcript))
	require.Len(t, transcript, 4, "a run's transcript holds its own messages")
	assert.Equal(t, 0, transcript[0].Position)
	assert.Equal(t, "and sort them", transcript[0].Text)
	var audit []api.AuditRecord
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/runs/"+second.ID+"/audit", nil, &audit))
	require.Len(t, audit, 2, "a run's audit log holds its own calls")
	assert.Equal(t, "files_write", audit[0].Tool)

	for _, id := range []string{first.ID, second.ID} {
		var conversation []api.Run
		require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/runs/"+id+"/conversation", nil, &conversation))
		require.Len(t, conversation, 2)
		assert.Equal(t, first.ID, conversation[0].ID)
		assert.Equal(t, finished, conversation[1])
	}
}

func TestFollowUp_RunsTheConversationsHarnessVersion(t *testing.T) {
	f := newFixture(t, options{})
	f.putNotes(t)
	f.script(modeltest.Reply("ok"))
	first := f.startRun(t, "tidy my notes")
	f.finish(t, first.ID)
	changed := notes()
	changed.Instructions = "Shout."
	require.Equal(t, http.StatusOK, f.do(t, http.MethodPut, home+"/harnesses/notes", changed, nil))
	m := f.script(modeltest.Reply("ok"))

	second := f.followUp(t, aliceToken, first.ID, "again")
	f.finish(t, second.ID)

	assert.Equal(t, 1, second.HarnessVersion, "a conversation keeps the version it started with")
	require.Len(t, m.Requests(), 1)
	assert.Equal(t, "Tidy the notes.", m.Requests()[0].System)
}

func TestFollowUp_Rejects(t *testing.T) {
	f := newFixture(t, options{policy: []policy.Module{policy.RulesModule("central", `require_approval contains "writes need a human" if input.tool == "files_write"`)}})
	f.putNotes(t)
	f.script(modeltest.Reply("ok"))
	done := f.startRun(t, "one")
	f.finish(t, done.ID)
	f.script(modeltest.Reply("ok"))
	followed := f.followUp(t, aliceToken, done.ID, "two")
	f.finish(t, followed.ID)
	f.script(modeltest.Fail(assert.AnError))
	failed := f.startRun(t, "three")
	f.finish(t, failed.ID)
	f.script(modeltest.CallTools(call("c1", "files_write", `{}`)), modeltest.Reply("ok"))
	running := f.startRun(t, "four")
	events := f.events(t, running.ID)
	events.next()
	events.next()

	tests := []struct {
		name, path string
		body       any
		wantStatus int
		wantErr    string
	}{
		{"unknown run", home + "/runs/ghost/follow-up", api.FollowUp{Input: "x"}, http.StatusNotFound, "not found"},
		{"no input", home + "/runs/" + followed.ID + "/follow-up", api.FollowUp{}, http.StatusBadRequest, "input is required"},
		{"unknown field", home + "/runs/" + followed.ID + "/follow-up", `{"input":"x","bogus":1}`, http.StatusBadRequest, "bogus"},
		{"a run already followed up", home + "/runs/" + done.ID + "/follow-up", api.FollowUp{Input: "x"}, http.StatusConflict, "already followed"},
		{"a failed run", home + "/runs/" + failed.ID + "/follow-up", api.FollowUp{Input: "x"}, http.StatusConflict, "only a run that succeeded"},
		{"a running run", home + "/runs/" + running.ID + "/follow-up", api.FollowUp{Input: "x"}, http.StatusConflict, "only a run that succeeded"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp api.Error
			assert.Equal(t, tt.wantStatus, f.do(t, http.MethodPost, tt.path, tt.body, &resp))
			assert.Contains(t, resp.Error, tt.wantErr)
		})
	}
	var conversation []api.Run
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/runs/"+done.ID+"/conversation", nil, &conversation))
	assert.Len(t, conversation, 2, "no refused follow-up started a run")
	var ghost api.Error
	assert.Equal(t, http.StatusNotFound, f.do(t, http.MethodGet, home+"/runs/ghost/conversation", nil, &ghost))
}

// TestInvariant_FollowUpsNeverShowTheModelACredential guards trust-model
// guarantee 5 across runs: a follow-up sends the model the conversation as
// stored, so a credential a tool or the model echoed in an earlier run
// reaches the model only redacted.
func TestInvariant_FollowUpsNeverShowTheModelACredential(t *testing.T) {
	f := newFixture(t, options{})
	f.putNotes(t)
	f.read.Result = json.RawMessage(`{"token":"` + token + `"}`)
	f.script(
		modeltest.CallTools(call("c1", "files_read", `{"token":"`+token+`"}`)),
		modeltest.Step{Response: model.Message{
			Role:     model.RoleAssistant,
			Text:     "the token is " + token,
			Provider: &model.ProviderPart{Name: "scripted", Data: json.RawMessage(`{"thinking":"I saw ` + token + `"}`)},
		}},
	)
	first := f.startRun(t, "leak it")
	require.Equal(t, api.RunStatusSucceeded, f.finish(t, first.ID).Status)
	m := f.script(modeltest.Reply("ok"))

	second := f.followUp(t, aliceToken, first.ID, "say it again")
	f.finish(t, second.ID)

	require.Len(t, m.Requests(), 1)
	sent, err := json.Marshal(m.Requests()[0].Messages)
	require.NoError(t, err)
	assert.NotContains(t, string(sent), token)
	assert.Contains(t, string(sent), "[redacted]")
}
