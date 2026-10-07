package anthropic_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/model/anthropic"
	"github.com/jangraefen/agenty/internal/model/anthropic/anthropictest"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

const testKey = "sk-ant-test-key-0123456789"

func newModel(t *testing.T, api *anthropictest.API) *anthropic.Model {
	t.Helper()
	m, err := anthropic.New(anthropic.Config{APIKey: testKey, Model: "claude-test", MaxTokens: 1024, BaseURL: api.URL})
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
		{"history cache TTL", anthropic.Config{APIKey: testKey, Model: "m", MaxTokens: 1, HistoryCacheTTL: "10m"}, `history cache TTL must be "5m" or "1h"`},
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
	api := anthropictest.New(t, anthropictest.Reply(t, "end_turn", anthropictest.TextBlock("done")))
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
	reqs := api.Requests()
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
	api := anthropictest.New(t, anthropictest.Reply(t, "end_turn", anthropictest.TextBlock("hi")))

	_, err := newModel(t, api).Generate(context.Background(), model.Request{Messages: []model.Message{{Role: model.RoleUser, Text: "hello"}}})

	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(api.Requests()[0].Body, &body))
	assert.NotContains(t, body, "system")
	assert.NotContains(t, body, "tools")
}

func TestGenerate_Replies(t *testing.T) {
	tests := []struct {
		name string
		resp func(*testing.T) anthropictest.Response
		want model.Message
	}{
		{
			name: "final answer",
			resp: func(t *testing.T) anthropictest.Response {
				return anthropictest.Reply(t, "end_turn", anthropictest.TextBlock("Labelled as urgent."))
			},
			want: model.Message{Role: model.RoleAssistant, Text: "Labelled as urgent."},
		},
		{
			name: "text blocks are joined",
			resp: func(t *testing.T) anthropictest.Response {
				return anthropictest.Reply(t, "end_turn", anthropictest.TextBlock("First."), anthropictest.TextBlock("Second."))
			},
			want: model.Message{Role: model.RoleAssistant, Text: "First.\nSecond."},
		},
		{
			name: "tool calls keep their ids, names and input",
			resp: func(t *testing.T) anthropictest.Response {
				return anthropictest.Reply(t, "tool_use", anthropictest.TextBlock("Let me look."), anthropictest.ToolUseBlock("toolu_1", "tickets_read", map[string]any{"id": 7}), anthropictest.ToolUseBlock("toolu_2", "tickets_label", map[string]any{}))
			},
			want: model.Message{Role: model.RoleAssistant, Text: "Let me look.", ToolCalls: []model.ToolCall{
				{ID: "toolu_1", Name: "tickets_read", Args: json.RawMessage(`{"id":7}`)},
				{ID: "toolu_2", Name: "tickets_label", Args: json.RawMessage(`{}`)},
			}},
		},
		{
			name: "stop sequence is a normal end",
			resp: func(t *testing.T) anthropictest.Response {
				return anthropictest.Reply(t, "stop_sequence", anthropictest.TextBlock("Done"))
			},
			want: model.Message{Role: model.RoleAssistant, Text: "Done"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := anthropictest.New(t, tt.resp(t))

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

func TestGenerate_ReportsUsage(t *testing.T) {
	resp := anthropictest.Reply(t, "end_turn", anthropictest.TextBlock("ok"))
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &body))
	body["usage"] = map[string]any{"input_tokens": 12, "output_tokens": 34, "cache_creation_input_tokens": 56, "cache_read_input_tokens": 78}
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	resp.Body = string(raw)
	api := anthropictest.New(t, resp)

	got, err := newModel(t, api).Generate(context.Background(), model.Request{Messages: []model.Message{{Role: model.RoleUser, Text: "hi"}}})

	require.NoError(t, err)
	assert.Equal(t, &model.Usage{InputTokens: 12, OutputTokens: 34, CacheWriteTokens: 56, CacheReadTokens: 78}, got.Usage)
}

// TestGenerate_UnusableRepliesAreErrors checks that the adapter fails closed:
// a reply the agent cannot use ends the run instead of passing as an answer.
func TestGenerate_UnusableRepliesAreErrors(t *testing.T) {
	tests := []struct {
		name    string
		resp    func(*testing.T) anthropictest.Response
		wantIs  error
		wantErr string
	}{
		{"truncated by max tokens", func(t *testing.T) anthropictest.Response {
			return anthropictest.Reply(t, "max_tokens", anthropictest.TextBlock("Half an ans"))
		}, anthropic.ErrTruncated, ""},
		{"refusal", func(t *testing.T) anthropictest.Response { return anthropictest.Reply(t, "refusal") }, anthropic.ErrRefused, ""},
		{"context window exceeded", func(t *testing.T) anthropictest.Response {
			return anthropictest.Reply(t, "model_context_window_exceeded")
		}, anthropic.ErrTruncated, ""},
		{"paused turn", func(t *testing.T) anthropictest.Response { return anthropictest.Reply(t, "pause_turn") }, nil, `stop reason "pause_turn"`},
		{"unsupported content block", func(t *testing.T) anthropictest.Response {
			return anthropictest.Reply(t, "end_turn", map[string]any{"type": "mystery_block"})
		}, nil, `content block "mystery_block"`},
		{"API error", func(*testing.T) anthropictest.Response {
			return anthropictest.Response{Status: http.StatusBadRequest, Body: `{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long"}}`}
		}, nil, "prompt is too long"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := anthropictest.New(t, tt.resp(t))

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
			api := anthropictest.New(t)

			_, err := newModel(t, api).Generate(context.Background(), model.Request{Messages: tt.msgs})

			require.ErrorContains(t, err, tt.wantErr)
			assert.Empty(t, api.Requests(), "an invalid conversation is never sent")
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
			api := anthropictest.New(t)

			_, err := newModel(t, api).Generate(context.Background(), model.Request{
				Messages: []model.Message{{Role: model.RoleUser, Text: "x"}},
				Tools:    []toolgateway.Definition{{Name: "tickets_read", InputSchema: tt.schema}},
			})

			require.ErrorContains(t, err, tt.wantErr)
			assert.Empty(t, api.Requests())
		})
	}
}

func TestGenerate_ToolWithoutSchemaTakesAnyObject(t *testing.T) {
	api := anthropictest.New(t, anthropictest.Reply(t, "end_turn", anthropictest.TextBlock("hi")))

	_, err := newModel(t, api).Generate(context.Background(), model.Request{
		Messages: []model.Message{{Role: model.RoleUser, Text: "x"}},
		Tools:    []toolgateway.Definition{{Name: "tickets_read"}},
	})

	require.NoError(t, err)
	var body struct {
		Tools []json.RawMessage `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(api.Requests()[0].Body, &body))
	require.Len(t, body.Tools, 1)
	assert.JSONEq(t, `{"name":"tickets_read","input_schema":{"type":"object"}}`, string(body.Tools[0]))
}

func TestGenerate_CancelledContext(t *testing.T) {
	api := anthropictest.New(t, anthropictest.Reply(t, "end_turn", anthropictest.TextBlock("late")))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := newModel(t, api).Generate(ctx, model.Request{Messages: []model.Message{{Role: model.RoleUser, Text: "x"}}})

	require.ErrorIs(t, err, context.Canceled)
}

// TestGenerate_IgnoresTheEnvironment checks that the SDK's environment
// defaults are off: no variable can redirect requests, and with them the API
// key, or add credentials or headers.
func TestGenerate_IgnoresTheEnvironment(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"base URL and API key", map[string]string{"ANTHROPIC_BASE_URL": "elsewhere", "ANTHROPIC_API_KEY": "sk-ant-from-the-environment"}},
		{"auth token", map[string]string{"ANTHROPIC_AUTH_TOKEN": "token-from-the-environment"}},
		{"custom headers", map[string]string{"ANTHROPIC_CUSTOM_HEADERS": "X-Leak: yes"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// No real profile or credential may interfere with the case.
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			for _, key := range []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_PROFILE", "ANTHROPIC_CUSTOM_HEADERS"} {
				t.Setenv(key, "")
			}
			elsewhere := anthropictest.New(t)
			for key, value := range tt.env {
				if value == "elsewhere" {
					value = elsewhere.URL
				}
				t.Setenv(key, value)
			}
			api := anthropictest.New(t, anthropictest.Reply(t, "end_turn", anthropictest.TextBlock("hi")))

			_, err := newModel(t, api).Generate(context.Background(), model.Request{Messages: []model.Message{{Role: model.RoleUser, Text: "x"}}})

			require.NoError(t, err)
			assert.Empty(t, elsewhere.Requests())
			reqs := api.Requests()
			require.Len(t, reqs, 1)
			assert.Equal(t, testKey, reqs[0].Header.Get("X-Api-Key"))
			assert.Empty(t, reqs[0].Header.Get("Authorization"))
			assert.Empty(t, reqs[0].Header.Get("X-Leak"))
		})
	}
}

// TestGenerate_ReplaysTheReplyUnchanged: models that think by default return
// thinking blocks, which must come back unchanged and in their place on the
// next request. The assistant turn is replayed exactly as the API sent it,
// also after the reply went through JSON, as it does when a transcript is
// stored and read back.
func TestGenerate_ReplaysTheReplyUnchanged(t *testing.T) {
	for _, stored := range []bool{false, true} {
		t.Run(fmt.Sprintf("stored=%t", stored), func(t *testing.T) {
			api := anthropictest.New(t,
				anthropictest.Reply(t, "tool_use",
					map[string]any{"type": "thinking", "thinking": "", "signature": "sig-1"},
					anthropictest.TextBlock("Let me look."),
					map[string]any{"type": "redacted_thinking", "data": "opaque-2"},
					anthropictest.ToolUseBlock("toolu_1", "tickets_read", map[string]any{"id": 7}),
				),
				anthropictest.Reply(t, "end_turn", anthropictest.TextBlock("Printer on fire.")),
			)
			m := newModel(t, api)
			req := model.Request{Messages: []model.Message{{Role: model.RoleUser, Text: "ticket 7"}}}

			first, err := m.Generate(context.Background(), req)
			require.NoError(t, err)
			assert.Equal(t, "Let me look.", first.Text, "thinking is not part of the answer")
			require.Len(t, first.ToolCalls, 1)
			require.NotNil(t, first.Provider)
			assert.Equal(t, "anthropic", first.Provider.Name)
			assert.Contains(t, string(first.Provider.Data), "sig-1", "the provider part keeps the thinking blocks")
			if stored {
				b, err := json.Marshal(first)
				require.NoError(t, err)
				first = model.Message{}
				require.NoError(t, json.Unmarshal(b, &first))
			}

			req.Messages = append(req.Messages, first, model.Message{Role: model.RoleUser, ToolResults: []model.ToolResult{{CallID: "toolu_1", Content: `{"title":"Printer on fire"}`}}})
			_, err = m.Generate(context.Background(), req)
			require.NoError(t, err)

			var body struct {
				Messages []struct {
					Role    string          `json:"role"`
					Content json.RawMessage `json:"content"`
				} `json:"messages"`
			}
			require.NoError(t, json.Unmarshal(api.Requests()[1].Body, &body))
			require.Len(t, body.Messages, 3)
			assert.Equal(t, "assistant", body.Messages[1].Role)
			assert.JSONEq(t, `[
				{"type":"thinking","thinking":"","signature":"sig-1"},
				{"type":"text","text":"Let me look."},
				{"type":"redacted_thinking","data":"opaque-2"},
				{"type":"tool_use","id":"toolu_1","name":"tickets_read","input":{"id":7}}
			]`, string(body.Messages[1].Content))
		})
	}
}

func TestGenerate_ProviderParts(t *testing.T) {
	tests := []struct {
		name     string
		provider *model.ProviderPart
		wantErr  string
		// wantContent is the replayed assistant turn when there is no error.
		wantContent string
	}{
		{
			name:        "another provider's part is ignored",
			provider:    &model.ProviderPart{Name: "other", Data: json.RawMessage(`{"whatever":1}`)},
			wantContent: `[{"type":"text","text":"Rebuilt."}]`,
		},
		{
			name:     "a broken part of this provider is an error",
			provider: &model.ProviderPart{Name: "anthropic", Data: json.RawMessage(`{`)},
			wantErr:  "provider part",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := anthropictest.New(t, anthropictest.Reply(t, "end_turn", anthropictest.TextBlock("ok")))
			m := newModel(t, api)
			req := model.Request{Messages: []model.Message{
				{Role: model.RoleUser, Text: "hi"},
				{Role: model.RoleAssistant, Text: "Rebuilt.", Provider: tt.provider},
				{Role: model.RoleUser, Text: "again"},
			}}

			_, err := m.Generate(context.Background(), req)

			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				assert.Empty(t, api.Requests())
				return
			}
			require.NoError(t, err)
			var body struct {
				Messages []struct {
					Content json.RawMessage `json:"content"`
				} `json:"messages"`
			}
			require.NoError(t, json.Unmarshal(api.Requests()[0].Body, &body))
			assert.JSONEq(t, tt.wantContent, string(body.Messages[1].Content))
		})
	}
}

// TestGenerate_CachesTheHistory: with a history cache TTL, the end of the
// earlier runs' conversation is a cache breakpoint of its own, written for
// that long, before the automatic one at the end of the request. Thinking
// blocks cannot be breakpoints, so a reply ending in one is marked at the
// block before it.
func TestGenerate_CachesTheHistory(t *testing.T) {
	answer := anthropictest.Reply(t, "end_turn",
		map[string]any{"type": "thinking", "thinking": "", "signature": "sig-1"},
		anthropictest.TextBlock("It is on fire."),
		map[string]any{"type": "thinking", "thinking": "", "signature": "sig-2"},
	)
	thoughtOnly := anthropictest.Reply(t, "end_turn", map[string]any{"type": "thinking", "thinking": "", "signature": "sig-3"})
	replies := map[string]model.Message{
		"replayed": {Role: model.RoleAssistant, Text: "It is on fire.", Provider: &model.ProviderPart{Name: "anthropic", Data: json.RawMessage(answer.Body)}},
		"rebuilt":  {Role: model.RoleAssistant, Text: "It is on fire."},
		// Nothing in it can be a breakpoint, so there is none.
		"thinking only": {Role: model.RoleAssistant, Provider: &model.ProviderPart{Name: "anthropic", Data: json.RawMessage(thoughtOnly.Body)}},
	}
	marked := []string{"", `{"type":"ephemeral","ttl":"1h"}`, ""}
	none := []string{"", "", ""}
	tests := []struct {
		name, ttl, reply string
		history          int
		want             []string
	}{
		{"an hour", "1h", "replayed", 2, marked},
		{"a rebuilt reply", "1h", "rebuilt", 2, marked},
		{"a reply of thinking only", "1h", "thinking only", 2, none},
		{"no history", "1h", "replayed", 0, none},
		{"nothing after the history", "1h", "replayed", 3, none},
		{"no history cache", "", "replayed", 2, none},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := anthropictest.New(t, anthropictest.Reply(t, "end_turn", anthropictest.TextBlock("done")))
			m, err := anthropic.New(anthropic.Config{APIKey: testKey, Model: "claude-test", MaxTokens: 1024, BaseURL: api.URL, HistoryCacheTTL: tt.ttl})
			require.NoError(t, err)
			history := []model.Message{{Role: model.RoleUser, Text: "ticket 7"}, replies[tt.reply]}
			messages := append(slices.Clone(history), model.Message{Role: model.RoleUser, Text: "and now?"})

			_, err = m.Generate(context.Background(), model.Request{Messages: messages, History: tt.history})

			require.NoError(t, err)
			require.Len(t, api.Requests(), 1)
			var body struct {
				CacheControl json.RawMessage `json:"cache_control"`
				Messages     []struct {
					Content []struct {
						Type         string          `json:"type"`
						CacheControl json.RawMessage `json:"cache_control"`
					} `json:"content"`
				} `json:"messages"`
			}
			require.NoError(t, json.Unmarshal(api.Requests()[0].Body, &body))
			assert.JSONEq(t, `{"type":"ephemeral"}`, string(body.CacheControl), "the rest is cached automatically, for the default time")
			require.Len(t, body.Messages, 3)
			var marks []string
			for _, msg := range body.Messages {
				mark := ""
				for _, block := range msg.Content {
					if len(block.CacheControl) > 0 {
						require.Empty(t, mark, "one breakpoint per message at most")
						require.Equal(t, "text", block.Type)
						mark = string(block.CacheControl)
					}
				}
				marks = append(marks, mark)
			}
			for i := range tt.want {
				if tt.want[i] == "" {
					assert.Empty(t, marks[i], "message %d", i)
				} else {
					assert.JSONEq(t, tt.want[i], marks[i], "message %d", i)
				}
			}
		})
	}
}
