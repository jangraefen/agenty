package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/api"
	"github.com/jangraefen/agenty/internal/config"
	"github.com/jangraefen/agenty/internal/harness"
	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/model/anthropic/anthropictest"
	"github.com/jangraefen/agenty/internal/model/modeltest"
	"github.com/jangraefen/agenty/internal/policy"
	"github.com/jangraefen/agenty/internal/secret"
	"github.com/jangraefen/agenty/internal/server"
	"github.com/jangraefen/agenty/internal/store"
	"github.com/jangraefen/agenty/internal/store/storetest"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

func call(id, name, args string) model.ToolCall {
	return model.ToolCall{ID: id, Name: name, Args: json.RawMessage(args)}
}

func TestNew_RejectsInvalidConfig(t *testing.T) {
	s := storetest.New(t)
	r, err := secret.NewRedactor(nil)
	require.NoError(t, err)
	logger := slog.New(slog.DiscardHandler)
	tests := []struct {
		name    string
		cfg     server.Config
		wantErr string
	}{
		{"no store", server.Config{Operator: &config.Config{}, Resolved: &config.Resolved{Redactor: r}, Logger: logger}, "store is required"},
		{"no config", server.Config{Store: s, Logger: logger}, "config is required"},
		{"no logger", server.Config{Store: s, Operator: &config.Config{}, Resolved: &config.Resolved{Redactor: r}}, "logger is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := server.New(context.Background(), tt.cfg)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestHarnesses(t *testing.T) {
	f := newFixture(t, options{})

	var first, again, second api.HarnessVersion
	require.Equal(t, http.StatusOK, f.do(t, http.MethodPut, home+"/harnesses/notes", notes(), &first))
	require.Equal(t, http.StatusOK, f.do(t, http.MethodPut, home+"/harnesses/notes", notes(), &again))
	changed := notes()
	changed.Instructions = "Tidy and sort the notes."
	changed.Policy = []policy.Module{policy.RulesModule("notes (inline policy)", `deny contains "no" if input.tool == "files_delete"`)}
	require.Equal(t, http.StatusOK, f.do(t, http.MethodPut, home+"/harnesses/notes", changed, &second))

	assert.Equal(t, 1, first.Version)
	assert.Equal(t, first, again, "an unchanged harness is not a new version")
	assert.Equal(t, 2, second.Version)
	assert.Equal(t, changed, second.Harness)

	var got api.HarnessVersion
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/harnesses/notes", nil, &got))
	assert.Equal(t, second, got)
	var all []api.HarnessVersion
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/harnesses", nil, &all))
	assert.Equal(t, []api.HarnessVersion{second}, all)
}

func TestPutHarness_Rejects(t *testing.T) {
	invalid := notes()
	invalid.Limits.MaxSteps = 0
	badPolicy := notes()
	badPolicy.Policy = []policy.Module{policy.RulesModule("notes (inline policy)", "deny contains")}
	tests := []struct {
		name    string
		path    string
		body    any
		wantErr string
	}{
		{"name that does not match the path", home + "/harnesses/other", notes(), `harness name "notes" does not match the path`},
		{"invalid harness", home + "/harnesses/notes", invalid, "limits.max_steps"},
		{"policy that does not compile", home + "/harnesses/notes", badPolicy, "notes (inline policy)"},
		{"unknown field", home + "/harnesses/notes", `{"name":"notes","bogus":1}`, "bogus"},
		{"not JSON", home + "/harnesses/notes", `{`, "invalid request body"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, options{})
			var resp api.Error

			status := f.do(t, http.MethodPut, tt.path, tt.body, &resp)

			assert.Equal(t, http.StatusBadRequest, status)
			assert.Contains(t, resp.Error, tt.wantErr)
			var all []api.HarnessVersion
			require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/harnesses", nil, &all))
			assert.Empty(t, all, "nothing is stored")
		})
	}
}

func TestNotFound(t *testing.T) {
	f := newFixture(t, options{})
	for _, path := range []string{home + "/harnesses/ghost", home + "/runs/ghost", home + "/runs/ghost/audit", home + "/runs/ghost/events", home + "/runs/ghost/transcript"} {
		var resp api.Error
		assert.Equal(t, http.StatusNotFound, f.do(t, http.MethodGet, path, nil, &resp), path)
		assert.Contains(t, resp.Error, "not found", path)
	}
}

func TestRun_SucceedsAndStreamsItsEvents(t *testing.T) {
	f := newFixture(t, options{})
	f.putNotes(t)
	f.script(modeltest.CallTools(call("c1", "files_read", `{"path":"notes.md"}`)), modeltest.Reply("- milk"))

	run := f.startRun(t, "tidy my notes")
	events := f.events(t, run.ID).rest()

	assert.Equal(t, api.RunRunning, run.Status)
	assert.Equal(t, "alice", run.StartedBy, "the run records who started it")
	require.Len(t, events, 3)
	decision := decodeAs[toolgateway.Record](t, events[0])
	assert.Equal(t, api.EventAudit, events[0].name)
	assert.Equal(t, toolgateway.EventDecision, decision.Event)
	assert.Equal(t, run.ID, decision.RunID)
	result := decodeAs[toolgateway.Record](t, events[1])
	assert.Equal(t, toolgateway.EventResult, result.Event)
	assert.JSONEq(t, `{"content":"- milk"}`, string(result.Result))
	assert.Equal(t, api.EventFinished, events[2].name)
	finished := decodeAs[api.Run](t, events[2])
	assert.Equal(t, api.RunSucceeded, finished.Status)
	assert.Equal(t, "- milk", finished.Output)
	assert.Equal(t, 2, finished.Steps)
	assert.NotNil(t, finished.FinishedAt)

	var stored api.Run
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/runs/"+run.ID, nil, &stored))
	assert.Equal(t, finished, stored)
	var audit []api.AuditRecord
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/runs/"+run.ID+"/audit", nil, &audit))
	require.Len(t, audit, 2)
	assert.Equal(t, decision, audit[0].Record)
	assert.Equal(t, []string{"files"}, f.files.StartedAs)
	assert.Equal(t, 1, f.files.Closed, "the run's MCP servers stop when it ends")

	replay := f.events(t, run.ID).rest()
	assert.Equal(t, events, replay, "a finished run's events are replayed from the store")

	var transcript []api.TranscriptMessage
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/runs/"+run.ID+"/transcript", nil, &transcript))
	require.Len(t, transcript, 4)
	assert.Equal(t, model.Message{Role: model.RoleUser, Text: "tidy my notes"}, transcript[0].Message)
	assert.Equal(t, []model.ToolCall{{ID: "c1", Name: "files_read", Args: json.RawMessage(`{"path":"notes.md"}`)}}, transcript[1].ToolCalls)
	assert.Equal(t, []model.ToolResult{{CallID: "c1", Content: `{"content":"- milk"}`}}, transcript[2].ToolResults)
	assert.Equal(t, "- milk", transcript[3].Text)
	for i, m := range transcript {
		assert.Equal(t, i, m.Position)
		assert.False(t, m.CreatedAt.IsZero())
	}
}

func TestRun_Approvals(t *testing.T) {
	needsApproval := []policy.Module{policy.RulesModule("central", `require_approval contains "writes need a human" if input.tool == "files_write"`)}
	tests := []struct {
		name         string
		as           string
		answer       api.Answer
		wantDecision toolgateway.Decision
		wantApprover string
		wantReason   string
		wantWrites   int
	}{
		{"approved", aliceToken, api.Answer{Approved: true}, toolgateway.Allow, "alice", "approved through the API", 1},
		{"rejected with a reason, by another member", bobToken, api.Answer{Reason: "not today"}, toolgateway.Deny, "bob", "approval rejected: not today", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, options{policy: needsApproval})
			f.putNotes(t)
			f.script(modeltest.CallTools(call("c1", "files_write", `{"path":"notes.md","content":"- milk"}`)), modeltest.Reply("done"))
			run := f.startRun(t, "tidy my notes")
			events := f.events(t, run.ID)

			decision := decodeAs[toolgateway.Record](t, events.next())
			assert.Equal(t, toolgateway.RequireApproval, decision.Decision)
			e := events.next()
			require.Equal(t, api.EventApproval, e.name)
			req := decodeAs[api.ApprovalRequest](t, e)
			assert.Equal(t, "files_write", req.Tool)
			assert.Equal(t, "notes", req.Harness)
			assert.Equal(t, []string{"writes need a human"}, req.Reasons)
			assert.JSONEq(t, `{"path":"notes.md","content":"- milk"}`, string(req.Args))
			assert.Zero(t, f.write.Calls, "nothing runs before the answer")

			require.Equal(t, http.StatusNoContent, f.doAs(t, tt.as, http.MethodPost, home+"/runs/"+run.ID+"/approvals/"+req.ID, tt.answer, nil))

			approval := decodeAs[toolgateway.Record](t, events.next())
			assert.Equal(t, toolgateway.EventApproval, approval.Event)
			assert.Equal(t, tt.wantDecision, approval.Decision)
			assert.Equal(t, tt.wantApprover, approval.Approver, "the approver is the signed-in user")
			assert.Equal(t, tt.wantReason, approval.Reason)
			rest := events.rest()
			finished := decodeAs[api.Run](t, rest[len(rest)-1])
			assert.Equal(t, api.RunSucceeded, finished.Status, "a rejection is reported to the model, not a failure")
			assert.Equal(t, tt.wantWrites, f.write.Calls)

			var resp api.Error
			assert.Equal(t, http.StatusNotFound, f.do(t, http.MethodPost, home+"/runs/"+run.ID+"/approvals/"+req.ID, tt.answer, &resp), "an approval is answered once")
		})
	}
}

func TestAnswerApproval_Rejects(t *testing.T) {
	f := newFixture(t, options{})
	tests := []struct {
		name       string
		body       any
		wantStatus int
		wantErr    string
	}{
		{"not JSON", `{`, http.StatusBadRequest, "invalid request body"},
		{"unknown field, such as a claimed approver", `{"approved":true,"approver":"mallory"}`, http.StatusBadRequest, "approver"},
		{"no such run or approval", api.Answer{Approved: true}, http.StatusNotFound, "is not waiting for approval"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp api.Error
			assert.Equal(t, tt.wantStatus, f.do(t, http.MethodPost, home+"/runs/r1/approvals/a1", tt.body, &resp))
			assert.Contains(t, resp.Error, tt.wantErr)
		})
	}
}

func TestCreateRun_Rejects(t *testing.T) {
	unserved := notes()
	unserved.Name = "unserved"
	unserved.Tools = []string{"files_delete"}
	otherProvider := notes()
	otherProvider.Name = "other"
	otherProvider.Tools = nil
	otherProvider.Model.Provider = "openai"
	tests := []struct {
		name       string
		newModel   func(harness.Model) (model.Model, error)
		configured bool
		body       any
		wantStatus int
		wantErr    string
	}{
		{"unknown harness", nil, false, api.CreateRun{Harness: "ghost", Input: "x"}, http.StatusNotFound, "harness ghost: not found"},
		{"no input", nil, false, api.CreateRun{Harness: "notes"}, http.StatusBadRequest, "input is required"},
		{"unknown field", nil, false, `{"harness":"notes","input":"x","bogus":1}`, http.StatusBadRequest, "bogus"},
		{"grant no server serves", nil, false, api.CreateRun{Harness: "unserved", Input: "x"}, http.StatusUnprocessableEntity, "grant files_delete: server files has no such tool"},
		{"model that cannot be built", func(harness.Model) (model.Model, error) { return nil, errors.New("no model for " + token) }, false, api.CreateRun{Harness: "notes", Input: "x"}, http.StatusUnprocessableEntity, "cannot start run: no model for [redacted]"},
		{"unsupported provider", nil, true, api.CreateRun{Harness: "other", Input: "x"}, http.StatusUnprocessableEntity, `model provider "openai" is not supported`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, options{newModel: tt.newModel, configuredModel: tt.configured})
			for _, h := range []harness.Harness{notes(), unserved, otherProvider} {
				require.Equal(t, http.StatusOK, f.do(t, http.MethodPut, home+"/harnesses/"+h.Name, h, nil))
			}
			var resp api.Error

			status := f.do(t, http.MethodPost, home+"/runs", tt.body, &resp)

			assert.Equal(t, tt.wantStatus, status)
			assert.Contains(t, resp.Error, tt.wantErr)
			assert.NotContains(t, resp.Error, token)
		})
	}
}

func TestRun_ModelFailureFailsTheRun(t *testing.T) {
	f := newFixture(t, options{})
	f.putNotes(t)
	f.script(modeltest.Fail(errors.New("model down, key " + token)))

	run := f.startRun(t, "tidy my notes")
	events := f.events(t, run.ID).rest()

	require.Len(t, events, 1)
	finished := decodeAs[api.Run](t, events[0])
	assert.Equal(t, api.RunFailed, finished.Status)
	assert.Contains(t, finished.Error, "model down, key [redacted]")
}

// TestInvariant_ServerCredentialsNeverLeak guards trust-model guarantee 5 on
// the server: a credential a tool or the model echoes back never reaches a
// response, an event, the store (run, audit log or transcript) or the log.
func TestInvariant_ServerCredentialsNeverLeak(t *testing.T) {
	f := newFixture(t, options{policy: []policy.Module{policy.RulesModule("central", `require_approval contains "writes need a human" if input.tool == "files_write"`)}})
	f.putNotes(t)
	f.read.Result = json.RawMessage(`{"token":"` + token + `"}`)
	f.write.Err = errors.New("write failed with " + token)
	f.script(
		modeltest.CallTools(call("c1", "files_read", `{}`), call("c2", "files_write", `{"token":"`+token+`"}`)),
		modeltest.Step{Response: model.Message{
			Role:     model.RoleAssistant,
			Text:     "the token is " + token,
			Provider: &model.ProviderPart{Name: "scripted", Data: json.RawMessage(`{"thinking":"I saw ` + token + `"}`)},
		}},
	)

	run := f.startRun(t, "leak it")
	events := f.events(t, run.ID)
	var seen []string
	for {
		e := events.next()
		seen = append(seen, e.data)
		if e.name == api.EventApproval {
			req := decodeAs[api.ApprovalRequest](t, e)
			require.Equal(t, http.StatusNoContent, f.do(t, http.MethodPost, home+"/runs/"+run.ID+"/approvals/"+req.ID, api.Answer{Approved: true}, nil))
		}
		if e.name == api.EventFinished {
			break
		}
	}
	var stored api.Run
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/runs/"+run.ID, nil, &stored))
	var audit []api.AuditRecord
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/runs/"+run.ID+"/audit", nil, &audit))
	var transcript []api.TranscriptMessage
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/runs/"+run.ID+"/transcript", nil, &transcript))
	storedJSON, err := json.Marshal(stored)
	require.NoError(t, err)
	auditJSON, err := json.Marshal(audit)
	require.NoError(t, err)
	transcriptJSON, err := json.Marshal(transcript)
	require.NoError(t, err)

	assert.Equal(t, "the token is [redacted]", stored.Output)
	assert.Equal(t, "the token is [redacted]", transcript[len(transcript)-1].Text)
	assert.Contains(t, string(transcript[1].ToolCalls[1].Args), "[redacted]", "the model's own arguments are redacted too")
	require.NotNil(t, transcript[len(transcript)-1].Provider)
	assert.JSONEq(t, `{"thinking":"I saw [redacted]"}`, string(transcript[len(transcript)-1].Provider.Data), "so is the provider's form of a reply")
	for _, data := range append(seen, string(storedJSON), string(auditJSON), string(transcriptJSON), f.logs.String()) {
		assert.NotContains(t, data, token)
	}
	assert.Contains(t, strings.Join(seen, "\n"), "[redacted]")
}

func TestClose_FailsRunsWaitingForApproval(t *testing.T) {
	f := newFixture(t, options{policy: []policy.Module{policy.RulesModule("central", `require_approval contains "writes need a human" if input.tool == "files_write"`)}})
	f.putNotes(t)
	f.script(modeltest.CallTools(call("c1", "files_write", `{}`)), modeltest.Reply("done"))
	run := f.startRun(t, "tidy my notes")
	events := f.events(t, run.ID)
	events.next()
	require.Equal(t, api.EventApproval, events.next().name)

	f.server.Close()

	rest := events.rest()
	finished := decodeAs[api.Run](t, rest[len(rest)-1])
	assert.Equal(t, api.RunFailed, finished.Status)
	assert.Contains(t, finished.Error, "context canceled")
	assert.Zero(t, f.write.Calls)
	stored, err := f.store.Run(context.Background(), "home", run.ID)
	require.NoError(t, err)
	assert.Equal(t, store.RunFailed, stored.Status, "how the run ended is stored although the server is stopping")
}

func TestNew_FailsRunsOfAnEarlierServer(t *testing.T) {
	f := newFixture(t, options{})
	ctx := context.Background()
	v, err := f.store.PutHarness(ctx, "home", notes())
	require.NoError(t, err)
	require.NoError(t, f.store.CreateRun(ctx, store.NewRun{ID: "orphan", HarnessVersionID: v.ID, Input: "tidy", StartedBy: "alice"}))
	r, err := secret.NewRedactor(nil)
	require.NoError(t, err)
	logs := &syncBuffer{}

	_, err = server.New(ctx, server.Config{Store: f.store, Operator: &config.Config{}, Resolved: &config.Resolved{Redactor: r}, Logger: slog.New(slog.NewTextHandler(logs, nil))})

	require.NoError(t, err)
	orphan, err := f.store.Run(ctx, "home", "orphan")
	require.NoError(t, err)
	assert.Equal(t, store.RunFailed, orphan.Status)
	assert.Equal(t, "the server stopped before the run finished", orphan.Error)
	assert.Contains(t, logs.String(), "runs=1")
}

func TestRun_WithTheConfiguredAnthropicProvider(t *testing.T) {
	fake := anthropictest.New(t, anthropictest.Reply(t, "end_turn", anthropictest.TextBlock("hello")))
	s := storetest.New(t)
	r, err := secret.NewRedactor(nil)
	require.NoError(t, err)
	srv, err := server.New(context.Background(), server.Config{
		Store: s,
		Operator: &config.Config{
			Provider:   config.Provider{Anthropic: &config.Anthropic{MaxTokens: 1024, BaseURL: fake.URL}},
			Workspaces: map[string]config.Workspace{"home": {Members: []string{"alice"}}},
		},
		Resolved: &config.Resolved{AnthropicAPIKey: "sk-ant-test-0123456789", Redactor: r, UserTokens: userTokens},
		Logger:   slog.New(slog.DiscardHandler),
	})
	require.NoError(t, err)
	t.Cleanup(srv.Close)
	h := notes()
	h.Tools = nil
	v, err := s.PutHarness(context.Background(), "home", h)
	require.NoError(t, err)
	f := &fixture{store: s, server: srv}
	f.http = newHTTP(t, srv)

	run := f.startRun(t, "hi")
	events := f.events(t, run.ID).rest()

	finished := decodeAs[api.Run](t, events[len(events)-1])
	assert.Equal(t, api.RunSucceeded, finished.Status)
	assert.Equal(t, "hello", finished.Output)
	assert.Equal(t, v.ID, finished.HarnessVersionID)
	require.Len(t, fake.Requests(), 1)
}

func TestRun_GatewayStopsAToolServerThatDoesNotStop(t *testing.T) {
	f := newFixture(t, options{})
	f.files.CloseErr = errors.New("still running")
	f.putNotes(t)
	f.script(modeltest.Reply("done"))

	run := f.startRun(t, "tidy")
	events := f.events(t, run.ID).rest()

	finished := decodeAs[api.Run](t, events[len(events)-1])
	assert.Equal(t, api.RunFailed, finished.Status, "a server that does not stop fails the run")
	assert.Contains(t, finished.Error, "still running")
	assert.Equal(t, "done", finished.Output)
}

// TestInvariant_ServerRefusesNonLocalRequests guards the checks that keep
// web pages from using the API: a request must be for a local host, so a
// DNS-rebound page cannot use the API as its own, and a page may call it only
// from a configured origin. Cross-site forms, which cannot send JSON, are
// refused by the content type.
func TestInvariant_ServerRefusesNonLocalRequests(t *testing.T) {
	f := newFixture(t, options{})
	f.putNotes(t)
	local := strings.TrimPrefix(f.http.URL, "http://")
	body := `{"harness":"notes","input":"tidy"}`
	tests := []struct {
		name        string
		host        string
		origin      string
		contentType string
		wantStatus  int
	}{
		{"foreign host, as after DNS rebinding", "attacker.example:8080", "", "application/json", http.StatusForbidden},
		{"foreign host on a loopback port", "attacker.example:" + strings.Split(local, ":")[1], "", "application/json", http.StatusForbidden},
		{"foreign origin, as from a cross-site form", local, "http://attacker.example", "text/plain", http.StatusForbidden},
		{"foreign origin with JSON", local, "http://attacker.example", "application/json", http.StatusForbidden},
		{"another local origin", local, "http://localhost:1234", "application/json", http.StatusForbidden},
		{"the API's own origin", local, "http://" + local, "application/json", http.StatusForbidden},
		{"configured origin over another scheme", local, "https://localhost:5173", "application/json", http.StatusForbidden},
		{"opaque origin", local, "null", "application/json", http.StatusForbidden},
		{"not JSON", local, "", "text/plain", http.StatusUnsupportedMediaType},
		{"form", local, "", "application/x-www-form-urlencoded", http.StatusUnsupportedMediaType},
		{"no content type", local, "", "", http.StatusUnsupportedMediaType},
		{"form from the configured origin", local, origin, "text/plain", http.StatusUnsupportedMediaType},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, f.http.URL+home+"/runs", strings.NewReader(body))
			require.NoError(t, err)
			req.Host = tt.host
			req.Header.Set("Authorization", "Bearer "+aliceToken)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())

			assert.Equal(t, tt.wantStatus, resp.StatusCode)
			if tt.origin != origin {
				assert.Empty(t, resp.Header.Get("Access-Control-Allow-Origin"), "no other page may read the response")
			}
		})
	}
	n, err := f.store.FailRunningRuns(context.Background(), "check")
	require.NoError(t, err)
	assert.Zero(t, n, "no refused request started a run")

	for _, ok := range []struct{ host, origin string }{
		{local, ""},
		{local, origin},
		{"localhost:" + strings.Split(local, ":")[1], ""},
	} {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, f.http.URL+home+"/harnesses", nil)
		require.NoError(t, err)
		req.Host = ok.host
		req.Header.Set("Authorization", "Bearer "+aliceToken)
		if ok.origin != "" {
			req.Header.Set("Origin", ok.origin)
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		assert.Equal(t, http.StatusOK, resp.StatusCode, "%+v", ok)
		assert.Equal(t, ok.origin, resp.Header.Get("Access-Control-Allow-Origin"), "only the configured origin's pages may read the response")
		assert.Contains(t, resp.Header.Values("Vary"), "Origin")
	}
}

func TestCORS_Preflight(t *testing.T) {
	f := newFixture(t, options{})
	tests := []struct {
		name       string
		origin     string
		wantStatus int
		wantAllow  string
	}{
		{"configured origin", origin, http.StatusNoContent, origin},
		{"other origin", "http://attacker.example", http.StatusForbidden, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(context.Background(), http.MethodOptions, f.http.URL+home+"/runs", nil)
			require.NoError(t, err)
			req.Header.Set("Origin", tt.origin)
			req.Header.Set("Access-Control-Request-Method", http.MethodPost)
			req.Header.Set("Access-Control-Request-Headers", "authorization, content-type")

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())

			assert.Equal(t, tt.wantStatus, resp.StatusCode, "a preflight carries no token, and needs none")
			assert.Equal(t, tt.wantAllow, resp.Header.Get("Access-Control-Allow-Origin"))
			if tt.wantAllow != "" {
				assert.Equal(t, "GET, POST, PUT", resp.Header.Get("Access-Control-Allow-Methods"))
				assert.Equal(t, "Authorization, Content-Type", resp.Header.Get("Access-Control-Allow-Headers"))
			}
		})
	}
}

// TestInvariant_ServerRequiresSignIn guards the API's access control: every
// route refuses a request without a configured user's bearer token, before
// it does anything.
func TestInvariant_ServerRequiresSignIn(t *testing.T) {
	f := newFixture(t, options{})
	f.putNotes(t)
	routes := []struct{ method, path, body string }{
		{http.MethodGet, "/v1/me", ""},
		{http.MethodPut, home + "/harnesses/notes", `{}`},
		{http.MethodGet, home + "/harnesses", ""},
		{http.MethodGet, home + "/harnesses/notes", ""},
		{http.MethodPost, home + "/runs", `{"harness":"notes","input":"tidy"}`},
		{http.MethodGet, home + "/runs/r1", ""},
		{http.MethodGet, home + "/runs/r1/audit", ""},
		{http.MethodGet, home + "/runs/r1/transcript", ""},
		{http.MethodGet, home + "/runs/r1/events", ""},
		{http.MethodPost, home + "/runs/r1/approvals/a1", `{"approved":true}`},
		{http.MethodGet, home + "/runs", ""},
		{http.MethodPost, home + "/runs/r1/cancel", ""},
		{http.MethodGet, home + "/approvals", ""},
		{http.MethodGet, "/v1/workspaces/ghost/harnesses", ""},
		{http.MethodGet, "/v1/no-such-route", ""},
		{http.MethodGet, "/", ""},
	}
	credentials := []struct{ name, header string }{
		{"no token", ""},
		{"unknown token", "Bearer " + strings.Repeat("x", len(aliceToken))},
		{"a token's prefix", "Bearer " + aliceToken[:len(aliceToken)-1]},
		{"a token with more after it", "Bearer " + aliceToken + "x"},
		{"empty token", "Bearer "},
		{"another scheme", "Basic " + aliceToken},
		{"the token alone", aliceToken},
	}
	for _, r := range routes {
		for _, cred := range credentials {
			t.Run(r.method+" "+r.path+" with "+cred.name, func(t *testing.T) {
				var body io.Reader
				if r.body != "" {
					body = strings.NewReader(r.body)
				}
				req, err := http.NewRequestWithContext(context.Background(), r.method, f.http.URL+r.path, body)
				require.NoError(t, err)
				req.Header.Set("Content-Type", "application/json")
				if cred.header != "" {
					req.Header.Set("Authorization", cred.header)
				}

				resp, err := http.DefaultClient.Do(req)
				require.NoError(t, err)
				defer func() { assert.NoError(t, resp.Body.Close()) }()
				var e api.Error
				require.NoError(t, json.NewDecoder(resp.Body).Decode(&e))

				assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
				assert.True(t, strings.HasPrefix(resp.Header.Get("WWW-Authenticate"), "Bearer"))
				assert.NotContains(t, e.Error, aliceToken[:8], "the error does not repeat the token")
			})
		}
	}
	n, err := f.store.FailRunningRuns(context.Background(), "check")
	require.NoError(t, err)
	assert.Zero(t, n, "no refused request started a run")
	var all []api.HarnessVersion
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/harnesses", nil, &all))
	assert.Len(t, all, 1, "no refused request stored a harness")
	assert.Contains(t, f.logs.String(), "status=401")
	for _, tok := range []string{aliceToken, bobToken, carolToken} {
		assert.NotContains(t, f.logs.String(), tok)
	}
}

func TestMe(t *testing.T) {
	f := newFixture(t, options{})
	tests := []struct {
		token string
		want  api.Me
	}{
		{aliceToken, api.Me{User: "alice", Workspaces: []string{"home"}}},
		{bobToken, api.Me{User: "bob", Workspaces: []string{"home", "work"}}},
		{carolToken, api.Me{User: "carol", Workspaces: []string{}}},
	}
	for _, tt := range tests {
		t.Run(tt.want.User, func(t *testing.T) {
			var got api.Me
			require.Equal(t, http.StatusOK, f.doAs(t, tt.token, http.MethodGet, "/v1/me", nil, &got))
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestInvariant_WorkspacesAreSeparate guards workspace membership: a user
// sees and changes nothing in a workspace they are not a member of, and a
// run is found only in its own workspace, so its ID alone gives no access.
func TestInvariant_WorkspacesAreSeparate(t *testing.T) {
	f := newFixture(t, options{policy: []policy.Module{policy.RulesModule("central", `require_approval contains "writes need a human" if input.tool == "files_write"`)}})
	f.putNotes(t)
	f.script(modeltest.CallTools(call("c1", "files_write", `{}`)), modeltest.Reply("done"))
	run := f.startRun(t, "tidy my notes")
	events := f.events(t, run.ID)
	events.next()
	req := decodeAs[api.ApprovalRequest](t, events.next())
	work := "/v1/workspaces/work"

	t.Run("non-members", func(t *testing.T) {
		for _, r := range []struct {
			token, method, path string
			body                any
		}{
			{carolToken, http.MethodGet, home + "/harnesses", nil},
			{carolToken, http.MethodGet, home + "/harnesses/notes", nil},
			{carolToken, http.MethodPut, home + "/harnesses/notes", notes()},
			{carolToken, http.MethodPost, home + "/runs", api.CreateRun{Harness: "notes", Input: "x"}},
			{carolToken, http.MethodGet, home + "/runs/" + run.ID, nil},
			{carolToken, http.MethodGet, home + "/runs/" + run.ID + "/audit", nil},
			{carolToken, http.MethodGet, home + "/runs/" + run.ID + "/transcript", nil},
			{carolToken, http.MethodGet, home + "/runs/" + run.ID + "/events", nil},
			{carolToken, http.MethodPost, home + "/runs/" + run.ID + "/approvals/" + req.ID, api.Answer{Approved: true}},
			{carolToken, http.MethodGet, home + "/runs", nil},
			{carolToken, http.MethodPost, home + "/runs/" + run.ID + "/cancel", nil},
			{carolToken, http.MethodGet, home + "/approvals", nil},
			{aliceToken, http.MethodGet, work + "/harnesses", nil},
			{aliceToken, http.MethodPut, work + "/harnesses/notes", notes()},
			{aliceToken, http.MethodGet, "/v1/workspaces/ghost/harnesses", nil},
		} {
			var e api.Error
			assert.Equal(t, http.StatusNotFound, f.doAs(t, r.token, r.method, r.path, r.body, &e), "%s %s", r.method, r.path)
			assert.Contains(t, e.Error, "workspace", "%s %s", r.method, r.path)
		}
	})
	t.Run("a member of another workspace", func(t *testing.T) {
		for _, path := range []string{"/runs/" + run.ID, "/runs/" + run.ID + "/audit", "/runs/" + run.ID + "/transcript", "/runs/" + run.ID + "/events", "/harnesses/notes"} {
			var e api.Error
			assert.Equal(t, http.StatusNotFound, f.doAs(t, bobToken, http.MethodGet, work+path, nil, &e), path)
		}
		assert.Equal(t, http.StatusNotFound, f.doAs(t, bobToken, http.MethodPost, work+"/runs/"+run.ID+"/approvals/"+req.ID, api.Answer{Approved: true}, nil))
		assert.Equal(t, http.StatusNotFound, f.doAs(t, bobToken, http.MethodPost, work+"/runs", api.CreateRun{Harness: "notes", Input: "x"}, nil))
		assert.Equal(t, http.StatusNotFound, f.doAs(t, bobToken, http.MethodPost, work+"/runs/"+run.ID+"/cancel", nil, nil))
		var all []api.HarnessVersion
		require.Equal(t, http.StatusOK, f.doAs(t, bobToken, http.MethodGet, work+"/harnesses", nil, &all))
		assert.Empty(t, all)
		var runs api.RunList
		require.Equal(t, http.StatusOK, f.doAs(t, bobToken, http.MethodGet, work+"/runs", nil, &runs))
		assert.Empty(t, runs.Runs)
		var approvals []api.ApprovalRequest
		require.Equal(t, http.StatusOK, f.doAs(t, bobToken, http.MethodGet, work+"/approvals", nil, &approvals))
		assert.Empty(t, approvals)
		var mine []api.ApprovalRequest
		require.Equal(t, http.StatusOK, f.doAs(t, bobToken, http.MethodGet, home+"/approvals", nil, &mine))
		assert.Len(t, mine, 1, "the request is listed in its own workspace")
	})

	assert.Zero(t, f.write.Calls, "no outsider answered the approval")
	require.Equal(t, http.StatusNoContent, f.do(t, http.MethodPost, home+"/runs/"+run.ID+"/approvals/"+req.ID, api.Answer{}, nil))
	rest := events.rest()
	assert.Equal(t, api.RunSucceeded, decodeAs[api.Run](t, rest[len(rest)-1]).Status)
}

func TestCreateRun_RefusedAfterClose(t *testing.T) {
	f := newFixture(t, options{})
	f.putNotes(t)
	f.server.Close()

	var resp api.Error
	status := f.do(t, http.MethodPost, home+"/runs", api.CreateRun{Harness: "notes", Input: "tidy"}, &resp)

	assert.Equal(t, http.StatusServiceUnavailable, status)
	assert.Contains(t, resp.Error, "server is stopping")
	n, err := f.store.FailRunningRuns(context.Background(), "check")
	require.NoError(t, err)
	assert.Zero(t, n, "no run was stored")
}

func TestUnknownRoute_NotFoundAfterSignIn(t *testing.T) {
	f := newFixture(t, options{})
	var resp api.Error

	assert.Equal(t, http.StatusNotFound, f.do(t, http.MethodGet, "/v1/no-such-route", nil, &resp))
	assert.Equal(t, "GET /v1/no-such-route: no such route", resp.Error)
}

// runToEnd starts a run that replies at once and waits until it finished.
func (f *fixture) runToEnd(t *testing.T, bearer, input string) api.Run {
	t.Helper()
	f.script(modeltest.Reply("done"))
	var run api.Run
	require.Equal(t, http.StatusCreated, f.doAs(t, bearer, http.MethodPost, home+"/runs", api.CreateRun{Harness: "notes", Input: input}, &run))
	f.events(t, run.ID).rest()
	return run
}

func TestListRuns(t *testing.T) {
	f := newFixture(t, options{})
	f.putNotes(t)
	r1 := f.runToEnd(t, aliceToken, "one")
	r2 := f.runToEnd(t, bobToken, "two")
	f.script(modeltest.Fail(errors.New("model down")))
	r3 := f.startRun(t, "three")
	f.events(t, r3.ID).rest()

	var all api.RunList
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/runs", nil, &all))
	ids := func(l api.RunList) []string {
		out := []string{}
		for _, r := range l.Runs {
			out = append(out, r.ID)
		}
		return out
	}
	assert.Equal(t, []string{r3.ID, r2.ID, r1.ID}, ids(all), "newest first")
	assert.Empty(t, all.Next, "a short page is the last")
	assert.Equal(t, "notes", all.Runs[0].Harness)
	assert.Equal(t, 1, all.Runs[0].HarnessVersion)
	assert.Equal(t, "bob", all.Runs[1].StartedBy)
	assert.Equal(t, api.RunFailed, all.Runs[0].Status)

	var page1, page2 api.RunList
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/runs?limit=2", nil, &page1))
	assert.Equal(t, []string{r3.ID, r2.ID}, ids(page1))
	assert.Equal(t, r2.ID, page1.Next)
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/runs?limit=2&before="+page1.Next, nil, &page2))
	assert.Equal(t, []string{r1.ID}, ids(page2))

	var failed, ofOther api.RunList
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/runs?status=failed", nil, &failed))
	assert.Equal(t, []string{r3.ID}, ids(failed))
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/runs?harness=other", nil, &ofOther))
	assert.Equal(t, []string{}, ids(ofOther))

	for _, q := range []string{"limit=0", "limit=201", "limit=x", "status=done"} {
		var resp api.Error
		assert.Equal(t, http.StatusBadRequest, f.do(t, http.MethodGet, home+"/runs?"+q, nil, &resp), q)
		assert.NotEmpty(t, resp.Error, q)
	}
}

func TestCancelRun(t *testing.T) {
	f := newFixture(t, options{policy: []policy.Module{policy.RulesModule("central", `require_approval contains "writes need a human" if input.tool == "files_write"`)}})
	f.putNotes(t)
	f.script(modeltest.CallTools(call("c1", "files_write", `{}`)), modeltest.Reply("done"))
	run := f.startRun(t, "tidy my notes")
	events := f.events(t, run.ID)
	events.next()
	require.Equal(t, api.EventApproval, events.next().name)

	require.Equal(t, http.StatusAccepted, f.doAs(t, bobToken, http.MethodPost, home+"/runs/"+run.ID+"/cancel", nil, nil), "any member may cancel")

	rest := events.rest()
	finished := decodeAs[api.Run](t, rest[len(rest)-1])
	assert.Equal(t, api.RunCancelled, finished.Status)
	assert.Equal(t, "cancelled by bob", finished.Error)
	assert.Zero(t, f.write.Calls, "the call waiting for approval never runs")
	var audit []api.AuditRecord
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/runs/"+run.ID+"/audit", nil, &audit))
	require.Len(t, audit, 2)
	assert.Equal(t, toolgateway.Deny, audit[1].Decision)
	assert.Equal(t, "approval failed: cancelled by bob", audit[1].Reason, "the audit log says who cancelled")
	var stored api.Run
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/runs/"+run.ID, nil, &stored))
	assert.Equal(t, finished, stored)
	var pending []api.ApprovalRequest
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/approvals", nil, &pending))
	assert.Empty(t, pending, "a cancelled run waits for nothing")

	var resp api.Error
	assert.Equal(t, http.StatusConflict, f.do(t, http.MethodPost, home+"/runs/"+run.ID+"/cancel", nil, &resp))
	assert.Contains(t, resp.Error, "already finished")
	assert.Equal(t, http.StatusNotFound, f.do(t, http.MethodPost, home+"/runs/ghost/cancel", nil, &resp))
}

func TestCancelRun_RunningOnAnotherServer(t *testing.T) {
	f := newFixture(t, options{})
	v, err := f.store.PutHarness(context.Background(), "home", notes())
	require.NoError(t, err)
	require.NoError(t, f.store.CreateRun(context.Background(), store.NewRun{ID: "elsewhere", HarnessVersionID: v.ID, Input: "x", StartedBy: "alice"}))
	var resp api.Error

	assert.Equal(t, http.StatusConflict, f.do(t, http.MethodPost, home+"/runs/elsewhere/cancel", nil, &resp))
	assert.Contains(t, resp.Error, "running on another server")
}

func TestApprovals_ListedUntilAnswered(t *testing.T) {
	f := newFixture(t, options{
		policy:          []policy.Module{policy.RulesModule("central", `require_approval contains "writes need a human" if input.tool == "files_write"`)},
		approvalTimeout: 10 * time.Minute,
	})
	f.putNotes(t)
	f.script(modeltest.CallTools(call("c1", "files_write", `{"path":"a.md"}`)), modeltest.Reply("done"))
	run := f.startRun(t, "tidy my notes")
	events := f.events(t, run.ID)
	events.next()
	req := decodeAs[api.ApprovalRequest](t, events.next())

	var pending []api.ApprovalRequest
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/approvals", nil, &pending))

	require.Len(t, pending, 1)
	assert.Equal(t, req.ID, pending[0].ID)
	assert.Equal(t, run.ID, pending[0].RunID)
	assert.Equal(t, "files_write", pending[0].Tool)
	assert.Equal(t, 10*time.Minute, pending[0].ExpiresAt.Sub(pending[0].CreatedAt))
	assert.Equal(t, run.ID, req.RunID, "the event carries the same request")

	require.Equal(t, http.StatusNoContent, f.do(t, http.MethodPost, home+"/runs/"+run.ID+"/approvals/"+req.ID, api.Answer{Approved: true}, nil))
	events.rest()
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/approvals", nil, &pending))
	assert.Empty(t, pending)
}

func TestApprovals_TimeOut(t *testing.T) {
	f := newFixture(t, options{
		policy:          []policy.Module{policy.RulesModule("central", `require_approval contains "writes need a human" if input.tool == "files_write"`)},
		approvalTimeout: 50 * time.Millisecond,
	})
	f.putNotes(t)
	f.script(modeltest.CallTools(call("c1", "files_write", `{}`)), modeltest.Reply("done"))
	run := f.startRun(t, "tidy my notes")

	events := f.events(t, run.ID).rest()

	var approval toolgateway.Record
	for _, e := range events {
		if e.name == api.EventAudit {
			if r := decodeAs[toolgateway.Record](t, e); r.Event == toolgateway.EventApproval {
				approval = r
			}
		}
	}
	assert.Equal(t, toolgateway.Deny, approval.Decision)
	assert.Equal(t, "approval rejected: no answer within 50ms", approval.Reason)
	assert.Empty(t, approval.Approver)
	assert.Zero(t, f.write.Calls)
	finished := decodeAs[api.Run](t, events[len(events)-1])
	assert.Equal(t, api.RunSucceeded, finished.Status, "a timed-out approval is a rejection, reported to the model")
}
