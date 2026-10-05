package anthropic_test

import (
	"cmp"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/dotenv"
	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/model/anthropic"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

// TestSmoke_RealAPI checks the adapter against the real API: a tool call, its
// result, and a final answer. It is never part of gating runs: it runs only
// with AGENTY_ANTHROPIC_SMOKE=1 set in the real environment. Only then is the
// .env file at the module root loaded, which may provide ANTHROPIC_API_KEY and
// AGENTY_ANTHROPIC_MODEL.
func TestSmoke_RealAPI(t *testing.T) {
	if os.Getenv("AGENTY_ANTHROPIC_SMOKE") != "1" {
		t.Skip("set AGENTY_ANTHROPIC_SMOKE=1 to run against the real API")
	}
	require.NoError(t, dotenv.LoadModuleRoot())
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Skip("set ANTHROPIC_API_KEY, in the environment or in .env, to run against the real API")
	}
	m, err := anthropic.New(anthropic.Config{
		APIKey:    key,
		Model:     cmp.Or(os.Getenv("AGENTY_ANTHROPIC_MODEL"), "claude-sonnet-5-5"),
		MaxTokens: 512,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	req := model.Request{
		System:   "You look up tickets with the tickets_read tool before answering.",
		Messages: []model.Message{{Role: model.RoleUser, Text: "What is the title of ticket 7?"}},
		Tools: []toolgateway.Definition{{
			Name:        "tickets_read",
			Description: "Reads a ticket by id.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"integer"}},"required":["id"]}`),
		}},
	}

	first, err := m.Generate(ctx, req)
	require.NoError(t, err)
	require.NotEmpty(t, first.ToolCalls, "the model calls the tool")
	call := first.ToolCalls[0]
	assert.Equal(t, "tickets_read", call.Name)

	req.Messages = append(req.Messages, first, model.Message{Role: model.RoleUser, ToolResults: []model.ToolResult{
		{CallID: call.ID, Content: `{"id":7,"title":"Printer on fire"}`},
	}})
	second, err := m.Generate(ctx, req)
	require.NoError(t, err)
	assert.Contains(t, second.Text, "Printer on fire")
}
