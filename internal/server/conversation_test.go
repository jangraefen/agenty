package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/api"
	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/model/modeltest"
	"github.com/jangraefen/agenty/internal/policy"
	"github.com/jangraefen/agenty/internal/store"
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

func TestFollowUp_RunsTheHarnessLatestVersion(t *testing.T) {
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

	assert.Equal(t, 2, second.HarnessVersion, "a follow-up runs the harness as it is now")
	require.Len(t, m.Requests(), 1)
	assert.Equal(t, "Shout.", m.Requests()[0].System)
	assert.Len(t, m.Requests()[0].Messages, 3, "with the conversation so far")
}

// TestInvariant_FollowUpsCannotRevive guards a trust-model guarantee: a
// follow-up runs the harness's latest version, so a grant or rule a builder
// takes away no longer applies to any conversation, however old.
func TestInvariant_FollowUpsCannotRevive(t *testing.T) {
	f := newFixture(t, options{})
	f.putNotes(t)
	f.script(modeltest.CallTools(call("c1", "files_write", `{"path":"notes.md"}`)), modeltest.Reply("written"))
	first := f.startRun(t, "write my notes")
	require.Equal(t, api.RunStatusSucceeded, f.finish(t, first.ID).Status)
	require.Equal(t, 1, f.write.Calls)
	tightened := notes()
	tightened.Tools = []string{"files_read"}
	require.Equal(t, http.StatusOK, f.do(t, http.MethodPut, home+"/harnesses/notes", tightened, nil))
	m := f.script(modeltest.CallTools(call("c2", "files_write", `{"path":"notes.md"}`)), modeltest.Reply("could not"))

	second := f.followUp(t, aliceToken, first.ID, "write them again")
	f.finish(t, second.ID)

	assert.Equal(t, 1, f.write.Calls, "the grant taken away in version 2 does not apply to the follow-up")
	requests := m.Requests()
	require.Len(t, requests, 2)
	var offered []string
	for _, def := range requests[0].Tools {
		offered = append(offered, def.Name)
	}
	assert.NotContains(t, offered, "files_write")
	var audit []api.AuditRecord
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/runs/"+second.ID+"/audit", nil, &audit))
	require.NotEmpty(t, audit)
	assert.Equal(t, "files_write", audit[0].Tool)
	assert.Equal(t, api.DecisionDeny, audit[0].Decision)
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
	f.script(modeltest.CallTools(call("c2", "files_write", `{}`)))
	cancelled := f.startRun(t, "five")
	waiting := f.events(t, cancelled.ID)
	waiting.next()
	waiting.next()
	require.Equal(t, http.StatusAccepted, f.do(t, http.MethodPost, home+"/runs/"+cancelled.ID+"/cancel", nil, nil))
	require.Equal(t, api.RunStatusCancelled, f.finish(t, cancelled.ID).Status)

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
		{"a cancelled run", home + "/runs/" + cancelled.ID + "/follow-up", api.FollowUp{Input: "x"}, http.StatusConflict, "only a run that succeeded"},
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

// TestInvariant_FollowUpsRedactWithTodaysSecrets guards trust-model
// guarantee 5 for transcripts stored before a secret was configured: a
// follow-up redacts the history again, with the secrets known now.
func TestInvariant_FollowUpsRedactWithTodaysSecrets(t *testing.T) {
	f := newFixture(t, options{})
	f.putNotes(t)
	f.script(modeltest.Reply("ok"))
	first := f.startRun(t, "remember it")
	require.Equal(t, api.RunStatusSucceeded, f.finish(t, first.ID).Status)
	// As a transcript stored before the token was a configured secret has it.
	ctx := context.Background()
	require.NoError(t, f.store.AppendMessage(ctx, first.ID, store.NewMessage{Position: 2, Message: model.Message{Role: model.RoleUser, Text: "the token is " + token}}))
	require.NoError(t, f.store.AppendMessage(ctx, first.ID, store.NewMessage{Position: 3, Message: model.Message{Role: model.RoleAssistant, Text: "noted: " + token}}))
	f.script(thought("two"))
	second := f.followUp(t, aliceToken, first.ID, "what was it?")
	f.finish(t, second.ID)
	m := f.script(modeltest.Reply("ok"))

	third := f.followUp(t, aliceToken, second.ID, "and again?")
	f.finish(t, third.ID)

	require.Len(t, m.Requests(), 1)
	sent, err := json.Marshal(m.Requests()[0].Messages)
	require.NoError(t, err)
	assert.NotContains(t, string(sent), token)
	assert.Contains(t, string(sent), "the token is [redacted]")
	assert.Equal(t, []string{"two"}, replayed(m.Requests()[0].Messages),
		"the first run's messages are sent changed; the second run was sent them as they are now")
}

// TestFollowUp_RacingFollowUpsDoNotBranch follows up one run many times at
// once: exactly one follow-up starts, and the others are told to follow up
// the conversation's latest run instead.
func TestFollowUp_RacingFollowUpsDoNotBranch(t *testing.T) {
	f := newFixture(t, options{})
	f.putNotes(t)
	f.script(modeltest.Reply("ok"))
	first := f.startRun(t, "one")
	f.finish(t, first.ID)
	const racers = 8
	steps := make([]modeltest.Step, racers)
	for i := range steps {
		steps[i] = modeltest.Reply("ok")
	}
	f.script(steps...)

	statuses := make([]int, racers)
	errs := make([]api.Error, racers)
	var wg sync.WaitGroup
	for i := range racers {
		wg.Go(func() {
			statuses[i] = f.do(t, http.MethodPost, home+"/runs/"+first.ID+"/follow-up", api.FollowUp{Input: "two"}, &errs[i])
		})
	}
	wg.Wait()

	var started int
	for i, status := range statuses {
		switch status {
		case http.StatusCreated:
			started++
		case http.StatusConflict:
			assert.Contains(t, errs[i].Error, "is already followed up")
			assert.NotContains(t, errs[i].Error, "store:", "the store's own wording stays out of the answer")
		default:
			t.Errorf("follow-up %d: status %d", i, status)
		}
	}
	assert.Equal(t, 1, started)
	var conversation []api.Run
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/runs/"+first.ID+"/conversation", nil, &conversation))
	assert.Len(t, conversation, 2)
}

// thought is a reply with a provider form, as Anthropic's thinking blocks are
// kept.
func thought(text string) modeltest.Step {
	return modeltest.Step{Response: model.Message{
		Role: model.RoleAssistant, Text: text,
		Provider: &model.ProviderPart{Name: "scripted", Data: json.RawMessage(`{"thinking":"` + text + `"}`)},
	}}
}

// replayed lists, for each reply the model is sent again, the text of those
// sent in their provider form.
func replayed(messages []model.Message) []string {
	out := []string{}
	for _, m := range messages {
		if m.Role == model.RoleAssistant && m.Provider != nil {
			out = append(out, m.Text)
		}
	}
	return out
}

func TestFollowUp_ReplaysRepliesInTheirProviderForm(t *testing.T) {
	f := newFixture(t, options{})
	f.putNotes(t)
	f.script(thought("one"))
	first := f.startRun(t, "one")
	f.finish(t, first.ID)
	f.script(thought("two"))
	second := f.followUp(t, aliceToken, first.ID, "two")
	f.finish(t, second.ID)
	m := f.script(modeltest.Reply("ok"))

	third := f.followUp(t, aliceToken, second.ID, "three")
	f.finish(t, third.ID)

	require.Len(t, m.Requests(), 1)
	assert.Equal(t, []string{"one", "two"}, replayed(m.Requests()[0].Messages), "nothing changed, so every reply is sent as the provider gave it")
}

// TestFollowUp_DropsTheProviderFormOfRunsWithAnotherPrompt: a provider may
// bind a reply's reasoning to the instructions and tools it was given, so a
// reply given under others is sent without its provider form, as is every
// reply before it, and the replies since keep theirs.
func TestFollowUp_DropsTheProviderFormOfRunsWithAnotherPrompt(t *testing.T) {
	f := newFixture(t, options{})
	f.putNotes(t)
	f.script(thought("one"))
	first := f.startRun(t, "one")
	f.finish(t, first.ID)
	changed := notes()
	changed.Instructions = "Shout."
	require.Equal(t, http.StatusOK, f.do(t, http.MethodPut, home+"/harnesses/notes", changed, nil))
	m := f.script(thought("two"))
	second := f.followUp(t, aliceToken, first.ID, "two")
	f.finish(t, second.ID)
	m3 := f.script(modeltest.Reply("ok"))

	third := f.followUp(t, aliceToken, second.ID, "three")
	f.finish(t, third.ID)

	require.Len(t, m.Requests(), 1)
	assert.Empty(t, replayed(m.Requests()[0].Messages), "the first reply was given under other instructions")
	require.Len(t, m3.Requests(), 1)
	assert.Equal(t, []string{"two"}, replayed(m3.Requests()[0].Messages))
	assert.Len(t, m3.Requests()[0].Messages, 5, "the replies themselves are all sent")
}

// TestFollowUp_DropsTheProviderFormOfAlteredRuns: redaction makes what is
// stored differ from what the model saw, so a run with a redacted message is
// sent without provider forms, as is every run before it.
func TestFollowUp_DropsTheProviderFormOfAlteredRuns(t *testing.T) {
	f := newFixture(t, options{})
	f.putNotes(t)
	f.script(thought("one"))
	first := f.startRun(t, "one")
	f.finish(t, first.ID)
	f.script(thought("two"))
	// The gateway redacts what tools return, but a user may paste a secret.
	second := f.followUp(t, aliceToken, first.ID, "use "+token)
	f.finish(t, second.ID)
	f.script(thought("three"))
	third := f.followUp(t, aliceToken, second.ID, "three")
	f.finish(t, third.ID)
	m := f.script(modeltest.Reply("ok"))

	fourth := f.followUp(t, aliceToken, third.ID, "four")
	f.finish(t, fourth.ID)

	require.Len(t, m.Requests(), 1)
	assert.Equal(t, []string{"three"}, replayed(m.Requests()[0].Messages))
}
