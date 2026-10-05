package anthropic_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/model/anthropic"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

const testKey = "sk-ant-test-key-0123456789"

func newModel(t *testing.T, api *fakeAPI) *anthropic.Model {
	t.Helper()
	m, err := anthropic.New(anthropic.Config{APIKey: testKey, Model: "claude-test", MaxTokens: 1024, BaseURL: api.srv.URL})
	require.NoError(t, err)
	return m
}

func TestNew_RejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     anthropic.Config
		wantErr string
	}{
		{"no API key", anthropic.Config{Model: "m", MaxTokens: 1}, "api key is required"},
		{"no model", anthropic.Config{APIKey: testKey, MaxTokens: 1}, "model is required"},
		{"no max tokens", anthropic.Config{APIKey: testKey, Model: "m"}, "max tokens must be greater than 0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := anthropic.New(tt.cfg)
			require.ErrorContains(t, err, tt.wantErr)
			assert.Nil(t, m)
		})
	}
}

// TestGenerate_SendsTheConversation pins the request the adapter sends for a
// conversation with tool calls, tool results and tools.
func TestGenerate_SendsTheConversation(t *testing.T) {
	api := newFakeAPI(t, reply(t, "end_turn", textBlock("done")))
	req := model.Request{
		System: "Triage the ticket.",
		Messages: []model.Message{
			{Role: model.RoleUser, Text: "ticket 7"},
			{Role: model.RoleAssistant, Text: "Reading it.", ToolCalls: []model.ToolCall{
				{ID: "c1", Name: "tickets_read", Args: json.RawMessage(`{"id":7}`)},
				{ID: "c2", Name: "tickets_label"},
			}},
			{Role: model.RoleUser, ToolResults: []model.ToolResult{
				{CallID: "c1", Content: `{"title":"Printer on fire"}`},
				{CallID: "c2", Content: "label service down", IsError: true},
			}},
		},
		Tools: []toolgateway.Definition{
			{Name: "tickets_read", Description: "Reads a ticket.", InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"integer"}},"required":["id"],"additionalProperties":false}`)},
			{Name: "tickets_label", InputSchema: json.RawMessage(`{"type":"object"}`)},
		},
	}

	_, err := newModel(t, api).Generate(context.Background(), req)

	require.NoError(t, err)
	reqs := api.recorded()
	require.Len(t, reqs, 1)
	assert.JSONEq(t, `{
		"model": "claude-test",
		"max_tokens": 1024,
		"cache_control": {"type": "ephemeral"},
		"system": [{"type": "text", "text": "Triage the ticket."}],
		"messages": [
			{"role": "user", "content": [{"type": "text", "text": "ticket 7"}]},
			{"role": "assistant", "content": [
				{"type": "text", "text": "Reading it."},
				{"type": "tool_use", "id": "c1", "name": "tickets_read", "input": {"id": 7}},
				{"type": "tool_use", "id": "c2", "name": "tickets_label", "input": {}}
			]},
			{"role": "user", "content": [
				{"type": "tool_result", "tool_use_id": "c1", "content": [{"type": "text", "text": "{\"title\":\"Printer on fire\"}"}], "is_error": false},
				{"type": "tool_result", "tool_use_id": "c2", "content": [{"type": "text", "text": "label service down"}], "is_error": true}
			]}
		],
		"tools": [
			{"name": "tickets_read", "description": "Reads a ticket.", "input_schema": {"type": "object", "properties": {"id": {"type": "integer"}}, "required": ["id"], "additionalProperties": false}},
			{"name": "tickets_label", "input_schema": {"type": "object"}}
		]
	}`, string(reqs[0].Body))
}

func TestGenerate_LeavesOutEmptySystemAndTools(t *testing.T) {
	api := newFakeAPI(t, reply(t, "end_turn", textBlock("hi")))

	_, err := newModel(t, api).Generate(context.Background(), model.Request{Messages: []model.Message{{Role: model.RoleUser, Text: "hello"}}})

	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(api.recorded()[0].Body, &body))
	assert.NotContains(t, body, "system")
	assert.NotContains(t, body, "tools")
}

func TestGenerate_Replies(t *testing.T) {
	tests := []struct {
		name string
		resp func(*testing.T) response
		want model.Message
	}{
		{
			name: "final answer",
			resp: func(t *testing.T) response { return reply(t, "end_turn", textBlock("Labelled as urgent.")) },
			want: model.Message{Role: model.RoleAssistant, Text: "Labelled as urgent."},
		},
		{
			name: "text blocks are joined",
			resp: func(t *testing.T) response { return reply(t, "end_turn", textBlock("First."), textBlock("Second.")) },
			want: model.Message{Role: model.RoleAssistant, Text: "First.\nSecond."},
		},
		{
			name: "tool calls keep their ids, names and input",
			resp: func(t *testing.T) response {
				return reply(t, "tool_use", textBlock("Let me look."), toolUseBlock("toolu_1", "tickets_read", map[string]any{"id": 7}), toolUseBlock("toolu_2", "tickets_label", map[string]any{}))
			},
			want: model.Message{Role: model.RoleAssistant, Text: "Let me look.", ToolCalls: []model.ToolCall{
				{ID: "toolu_1", Name: "tickets_read", Args: json.RawMessage(`{"id":7}`)},
				{ID: "toolu_2", Name: "tickets_label", Args: json.RawMessage(`{}`)},
			}},
		},
		{
			name: "stop sequence is a normal end",
			resp: func(t *testing.T) response { return reply(t, "stop_sequence", textBlock("Done")) },
			want: model.Message{Role: model.RoleAssistant, Text: "Done"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := newFakeAPI(t, tt.resp(t))

			got, err := newModel(t, api).Generate(context.Background(), model.Request{Messages: []model.Message{{Role: model.RoleUser, Text: "ticket 7"}}})

			require.NoError(t, err)
			assert.Equal(t, tt.want.Role, got.Role)
			assert.Equal(t, tt.want.Text, got.Text)
			require.Len(t, got.ToolCalls, len(tt.want.ToolCalls))
			for i, c := range tt.want.ToolCalls {
				assert.Equal(t, c.ID, got.ToolCalls[i].ID)
				assert.Equal(t, c.Name, got.ToolCalls[i].Name)
				assert.JSONEq(t, string(c.Args), string(got.ToolCalls[i].Args))
			}
		})
	}
}

// TestGenerate_UnusableRepliesAreErrors checks that the adapter fails closed:
// a reply the agent cannot use ends the run instead of passing as an answer.
func TestGenerate_UnusableRepliesAreErrors(t *testing.T) {
	tests := []struct {
		name    string
		resp    func(*testing.T) response
		wantIs  error
		wantErr string
	}{
		{"truncated by max tokens", func(t *testing.T) response { return reply(t, "max_tokens", textBlock("Half an ans")) }, anthropic.ErrTruncated, ""},
		{"refusal", func(t *testing.T) response { return reply(t, "refusal") }, anthropic.ErrRefused, ""},
		{"context window exceeded", func(t *testing.T) response { return reply(t, "model_context_window_exceeded") }, anthropic.ErrTruncated, ""},
		{"paused turn", func(t *testing.T) response { return reply(t, "pause_turn") }, nil, `stop reason "pause_turn"`},
		{"unsupported content block", func(t *testing.T) response {
			return reply(t, "end_turn", map[string]any{"type": "redacted_thinking", "data": "x"})
		}, nil, `content block "redacted_thinking"`},
		{"API error", func(*testing.T) response {
			return response{status: http.StatusBadRequest, body: `{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long"}}`}
		}, nil, "prompt is too long"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := newFakeAPI(t, tt.resp(t))

			got, err := newModel(t, api).Generate(context.Background(), model.Request{Messages: []model.Message{{Role: model.RoleUser, Text: "ticket 7"}}})

			require.Error(t, err)
			if tt.wantIs != nil {
				require.ErrorIs(t, err, tt.wantIs)
			}
			require.ErrorContains(t, err, tt.wantErr)
			assert.Zero(t, got)
		})
	}
}

func TestGenerate_InvalidConversationsAreErrors(t *testing.T) {
	tests := []struct {
		name    string
		msgs    []model.Message
		wantErr string
	}{
		{"empty message", []model.Message{{Role: model.RoleUser}}, "message 0 is empty"},
		{"unknown role", []model.Message{{Role: "system", Text: "x"}}, `message 0 has unknown role "system"`},
		{"invalid tool call input", []model.Message{
			{Role: model.RoleUser, Text: "x"},
			{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "c1", Name: "t", Args: json.RawMessage(`{not json`)}}},
		}, "message 1: tool call c1: input is not valid JSON"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := newFakeAPI(t)

			_, err := newModel(t, api).Generate(context.Background(), model.Request{Messages: tt.msgs})

			require.ErrorContains(t, err, tt.wantErr)
			assert.Empty(t, api.recorded(), "an invalid conversation is never sent")
		})
	}
}

func TestGenerate_InvalidToolSchemasAreErrors(t *testing.T) {
	tests := []struct {
		name    string
		schema  json.RawMessage
		wantErr string
	}{
		{"not an object", json.RawMessage(`[1,2]`), "tool tickets_read: input schema"},
		{"not an object schema", json.RawMessage(`{"type":"string"}`), "tool tickets_read: input schema: type must be object"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := newFakeAPI(t)

			_, err := newModel(t, api).Generate(context.Background(), model.Request{
				Messages: []model.Message{{Role: model.RoleUser, Text: "x"}},
				Tools:    []toolgateway.Definition{{Name: "tickets_read", InputSchema: tt.schema}},
			})

			require.ErrorContains(t, err, tt.wantErr)
			assert.Empty(t, api.recorded())
		})
	}
}

func TestGenerate_ToolWithoutSchemaTakesAnyObject(t *testing.T) {
	api := newFakeAPI(t, reply(t, "end_turn", textBlock("hi")))

	_, err := newModel(t, api).Generate(context.Background(), model.Request{
		Messages: []model.Message{{Role: model.RoleUser, Text: "x"}},
		Tools:    []toolgateway.Definition{{Name: "tickets_read"}},
	})

	require.NoError(t, err)
	var body struct {
		Tools []json.RawMessage `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(api.recorded()[0].Body, &body))
	require.Len(t, body.Tools, 1)
	assert.JSONEq(t, `{"name":"tickets_read","input_schema":{"type":"object"}}`, string(body.Tools[0]))
}

func TestGenerate_CancelledContext(t *testing.T) {
	api := newFakeAPI(t, reply(t, "end_turn", textBlock("late")))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := newModel(t, api).Generate(ctx, model.Request{Messages: []model.Message{{Role: model.RoleUser, Text: "x"}}})

	require.ErrorIs(t, err, context.Canceled)
}

// TestGenerate_IgnoresTheEnvironment checks that the SDK's environment
// defaults are off: no variable can redirect requests, and with them the API
// key, or add credentials or headers.
func TestGenerate_IgnoresTheEnvironment(t *testing.T) {
	elsewhere := newFakeAPI(t)
	t.Setenv("ANTHROPIC_BASE_URL", elsewhere.srv.URL)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-from-the-environment")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "token-from-the-environment")
	t.Setenv("ANTHROPIC_CUSTOM_HEADERS", "X-Leak: yes")
	api := newFakeAPI(t, reply(t, "end_turn", textBlock("hi")))

	_, err := newModel(t, api).Generate(context.Background(), model.Request{Messages: []model.Message{{Role: model.RoleUser, Text: "x"}}})

	require.NoError(t, err)
	assert.Empty(t, elsewhere.recorded())
	reqs := api.recorded()
	require.Len(t, reqs, 1)
	assert.Equal(t, testKey, reqs[0].Header.Get("X-Api-Key"))
	assert.Empty(t, reqs[0].Header.Get("Authorization"))
	assert.Empty(t, reqs[0].Header.Get("X-Leak"))
}
