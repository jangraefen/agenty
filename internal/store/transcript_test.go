package store_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/model"
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
		require.NoError(t, s.AppendMessage(ctx, "r1", i, m))
	}

	got, err := s.Transcript(ctx, "r1")

	require.NoError(t, err)
	require.Len(t, got, len(messages))
	for i, m := range got {
		assert.Equal(t, i, m.Position)
		assert.False(t, m.CreatedAt.IsZero())
		assert.Equal(t, messages[i], m.Message)
	}
	empty, err := s.Transcript(ctx, "ghost")
	require.NoError(t, err)
	assert.Empty(t, empty)
}

func TestAppendMessage_Rejects(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	newRun(t, s, "r1")
	require.NoError(t, s.AppendMessage(ctx, "r1", 0, model.Message{Role: model.RoleUser, Text: "x"}))

	require.Error(t, s.AppendMessage(ctx, "r1", 0, model.Message{Role: model.RoleUser, Text: "again"}), "a position is written once")
	require.Error(t, s.AppendMessage(ctx, "ghost", 0, model.Message{Role: model.RoleUser, Text: "x"}), "messages belong to a stored run")
	require.Error(t, s.AppendMessage(ctx, "r1", 1, model.Message{Role: "system", Text: "x"}), "roles are user or assistant")
	require.ErrorContains(t, s.AppendMessage(ctx, "r1", 1, model.Message{Role: model.RoleAssistant, Provider: &model.ProviderPart{Data: json.RawMessage(`{}`)}}), "provider part without a name",
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
		require.NoError(t, s.AppendMessage(ctx, "r1", i, m))
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

func TestTranscript_StoreDown(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	newRun(t, s, "r1")
	s.Close()

	require.Error(t, s.AppendMessage(ctx, "r1", 0, model.Message{Role: model.RoleUser, Text: "x"}))
	_, err := s.Transcript(ctx, "r1")
	require.Error(t, err)
}
