package api_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/api"
	"github.com/jangraefen/agenty/internal/harness"
	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/policy"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

func TestHarness_RoundTrips(t *testing.T) {
	tests := []harness.Harness{
		{
			Name:         "notes",
			Instructions: "Tidy the notes.",
			Model:        harness.Model{Provider: "anthropic", Name: "claude-test"},
			Tools:        []string{"files_read", "files_write"},
			Limits:       harness.Limits{MaxSteps: 5, MaxToolCalls: 10},
			Policy:       []policy.Module{{Name: "notes (inline policy)", Source: "package agenty.tool"}},
		},
		{Name: "bare", Model: harness.Model{Provider: "anthropic", Name: "m"}, Limits: harness.Limits{MaxSteps: 1, MaxToolCalls: 1}},
	}
	for _, h := range tests {
		assert.Equal(t, h, api.FromHarness(h).ToHarness())
	}
}

func TestFromRecord(t *testing.T) {
	at := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	rec := toolgateway.Record{
		RunID: "r1", CallID: "c1", Event: toolgateway.EventApproval, Tool: "files_write",
		Args: json.RawMessage(`{"path":"a"}`), Decision: toolgateway.Deny, Reason: "no",
		Approver: "alice", Result: json.RawMessage(`{}`), Err: "boom",
	}

	assert.Equal(t, api.AuditRecord{
		CallID: "c1", Event: api.AuditEventApproval, Tool: "files_write",
		Args: json.RawMessage(`{"path":"a"}`), Decision: api.DecisionDeny, Reason: "no",
		Approver: "alice", Result: json.RawMessage(`{}`), Error: "boom", RecordedAt: at,
	}, api.FromRecord(rec, at))
}

func TestFromMessage(t *testing.T) {
	msg := model.Message{
		Role:        model.RoleAssistant,
		Text:        "done",
		ToolCalls:   []model.ToolCall{{ID: "c1", Name: "files_read", Args: json.RawMessage(`{}`)}},
		ToolResults: []model.ToolResult{{CallID: "c0", Content: "x", IsError: true}},
		Provider:    &model.ProviderPart{Name: "anthropic", Data: json.RawMessage(`{"a":1}`)},
	}

	got := api.FromMessage(3, msg)

	assert.Equal(t, api.TranscriptMessage{
		Position:    3,
		Role:        api.RoleAssistant,
		Text:        "done",
		ToolCalls:   []api.ToolCall{{ID: "c1", Name: "files_read", Args: json.RawMessage(`{}`)}},
		ToolResults: []api.ToolResult{{CallID: "c0", Content: "x", IsError: true}},
	}, got)
	b, err := json.Marshal(got)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "anthropic", "the provider's form of a reply is for the model alone")
}

func TestRun_UnfinishedHasNoFinishedAt(t *testing.T) {
	b, err := json.Marshal(api.Run{ID: "r1", Status: api.RunStatusRunning})
	require.NoError(t, err)
	assert.NotContains(t, string(b), "finished_at")
}
