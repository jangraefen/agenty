package store_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/store"
	"github.com/jangraefen/agenty/internal/store/storetest"
)

func TestTranscript_StoresMessagesInOrder(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	newRun(t, s, "r1")
	messages := []model.Message{
		{Role: model.RoleUser, Text: "tidy my notes"},
		{
			Role: model.RoleAssistant, Text: "Reading them.",
			ToolCalls: []model.ToolCall{{ID: "c1", Name: "files_read", Args: json.RawMessage(`{"path":"notes.md"}`)}},
			Provider:  &model.ProviderPart{Name: "anthropic", Data: json.RawMessage(`{"content": [{"type":"thinking","signature":"sig-1"}], "a": 1, "a": 2}`)},
		},
		{Role: model.RoleUser, ToolResults: []model.ToolResult{{CallID: "c1", Content: `{"content":"- milk"}`}, {CallID: "c2", Content: "tool call denied", IsError: true}}},
		{Role: model.RoleAssistant, Text: "Done."},
	}
	for i, m := range messages {
		require.NoError(t, s.AppendMessage(ctx, "r1", store.NewMessage{Position: i, Message: m}))
	}

	got, err := s.Transcript(ctx, "r1")

	require.NoError(t, err)
	require.Len(t, got, len(messages))
	for i, m := range got {
		assert.Equal(t, i, m.Position)
		assert.False(t, m.CreatedAt.IsZero())
		assert.Equal(t, messages[i], m.Message)
		assert.False(t, m.Altered)
	}
	empty, err := s.Transcript(ctx, "ghost")
	require.NoError(t, err)
	assert.Empty(t, empty)
}

func TestAppendMessage_Rejects(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	newRun(t, s, "r1")
	require.NoError(t, s.AppendMessage(ctx, "r1", store.NewMessage{Position: 0, Message: model.Message{Role: model.RoleUser, Text: "x"}}))

	require.Error(t, s.AppendMessage(ctx, "r1", store.NewMessage{Position: 0, Message: model.Message{Role: model.RoleUser, Text: "again"}}), "a position is written once")
	require.Error(t, s.AppendMessage(ctx, "ghost", store.NewMessage{Position: 0, Message: model.Message{Role: model.RoleUser, Text: "x"}}), "messages belong to a stored run")
	require.Error(t, s.AppendMessage(ctx, "r1", store.NewMessage{Position: 1, Message: model.Message{Role: "system", Text: "x"}}), "roles are user or assistant")
	require.ErrorContains(t, s.AppendMessage(ctx, "r1", store.NewMessage{Position: 1, Message: model.Message{Role: model.RoleAssistant, Provider: &model.ProviderPart{Data: json.RawMessage(`{}`)}}}), "provider part without a name",
		"a provider part names its provider, or it could not be read back")
}

// TestAppendMessage_NeverFailsOnContent: what a model or tool wrote must not
// stop the transcript, as with the audit log.
func TestAppendMessage_NeverFailsOnContent(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	newRun(t, s, "r1")
	msgs := []model.Message{
		{
			Role: model.RoleAssistant, Text: "nul \x00 and \xff",
			ToolCalls: []model.ToolCall{{ID: "c1", Name: "files_read", Args: json.RawMessage(`{"path":`)}},
			Provider:  &model.ProviderPart{Name: "anthropic", Data: json.RawMessage(`{"broken`)},
		},
		{Role: model.RoleUser, ToolResults: []model.ToolResult{{CallID: "c1", Content: "bin\x00ary \xff"}}},
	}

	for i, m := range msgs {
		require.NoError(t, s.AppendMessage(ctx, "r1", store.NewMessage{Position: i, Message: m}))
	}

	got, err := s.Transcript(ctx, "r1")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "nul � and �", got[0].Text)
	assert.JSONEq(t, `"{\"path\":"`, string(got[0].ToolCalls[0].Args), "arguments that are not JSON are kept as a string")
	require.NotNil(t, got[0].Provider)
	assert.JSONEq(t, `"{\"broken"`, string(got[0].Provider.Data), "a provider part that is not JSON is kept as a string")
	assert.Equal(t, "bin\x00ary �", got[1].ToolResults[0].Content, "inside JSON, a NUL is kept escaped")
}

// TestTranscript_RecordsAlteredMessages: a message stored other than as the
// model saw or wrote it is marked, whether redaction changed it or the store
// had to replace bytes of its text.
func TestTranscript_RecordsAlteredMessages(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	newRun(t, s, "r1")
	msgs := []store.NewMessage{
		{Message: model.Message{Role: model.RoleUser, Text: "as is"}},
		{Message: model.Message{Role: model.RoleAssistant, Text: "the token is [redacted]"}, Altered: true},
		{Message: model.Message{Role: model.RoleUser, Text: "nul \x00"}},
		{Message: model.Message{Role: model.RoleAssistant, Text: "invalid \xff"}},
	}
	for i, m := range msgs {
		m.Position = i
		require.NoError(t, s.AppendMessage(ctx, "r1", m))
	}

	got, err := s.Transcript(ctx, "r1")

	require.NoError(t, err)
	require.Len(t, got, len(msgs))
	altered := make([]bool, len(got))
	for i, m := range got {
		altered[i] = m.Altered
	}
	assert.Equal(t, []bool{false, true, true, true}, altered)
}

func TestTranscript_KeepsUsage(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	newRun(t, s, "r1")
	newRun(t, s, "r2")
	msgs := []model.Message{
		{Role: model.RoleUser, Text: "tidy"},
		{Role: model.RoleAssistant, Text: "looking", Usage: &model.Usage{InputTokens: 10, OutputTokens: 2, CacheWriteTokens: 100}},
		{Role: model.RoleUser, Text: "more"},
		{Role: model.RoleAssistant, Text: "done", Usage: &model.Usage{InputTokens: 3, OutputTokens: 4, CacheReadTokens: 100}},
	}
	for i, m := range msgs {
		require.NoError(t, s.AppendMessage(ctx, "r1", store.NewMessage{Position: i, Message: m}))
	}

	got, err := s.Transcript(ctx, "r1")
	require.NoError(t, err)
	require.Len(t, got, len(msgs))
	for i, m := range got {
		assert.Equal(t, msgs[i].Usage, m.Usage)
	}
	r1, err := s.Run(ctx, ws, "r1")
	require.NoError(t, err)
	assert.Equal(t, model.Usage{InputTokens: 13, OutputTokens: 6, CacheWriteTokens: 100, CacheReadTokens: 100}, r1.Usage, "a run sums the usage of its messages")
	r2, err := s.Run(ctx, ws, "r2")
	require.NoError(t, err)
	assert.Zero(t, r2.Usage)
}

// TestTranscript_StoreDown: with the database gone, appending and reading a
// transcript are errors, never a silent loss or an empty transcript.
func TestTranscript_StoreDown(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	newRun(t, s, "r1")
	s.Close()

	require.Error(t, s.AppendMessage(ctx, "r1", store.NewMessage{Position: 0, Message: model.Message{Role: model.RoleUser, Text: "x"}}))
	_, err := s.Transcript(ctx, "r1")
	require.Error(t, err)
}
