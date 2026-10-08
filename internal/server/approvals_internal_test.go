package server

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/store"
)

// TestInvariant_AnApprovalRunsOnlyTheCallItWasAskedFor guards a trust-model
// guarantee: a resumed call runs only if the run's stored reply makes the very
// call the approver saw, by name and arguments.
func TestInvariant_AnApprovalRunsOnlyTheCallItWasAskedFor(t *testing.T) {
	reply := model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{
		{ID: "c1", Name: "files_read", Args: json.RawMessage(`{"path":"a.md"}`)},
		{ID: "c2", Name: "files_write", Args: json.RawMessage(`{"path": "a.md", "content": "x"}`)},
	}}
	approval := func(call int, tool, args string) store.Approval {
		return store.Approval{NewApproval: store.NewApproval{Call: call, Tool: tool, Args: json.RawMessage(args)}}
	}
	tests := []struct {
		name     string
		own      []model.Message
		approval store.Approval
		want     bool
	}{
		{"the call asked for, spaced otherwise", []model.Message{reply}, approval(1, "files_write", `{"content":"x","path":"a.md"}`), true},
		{"other arguments", []model.Message{reply}, approval(1, "files_write", `{"path":"b.md","content":"x"}`), false},
		{"another tool", []model.Message{reply}, approval(1, "files_delete", `{"path":"a.md","content":"x"}`), false},
		{"another call of the reply", []model.Message{reply}, approval(0, "files_write", `{"path":"a.md","content":"x"}`), false},
		{"a call the reply does not make", []model.Message{reply}, approval(2, "files_write", `{}`), false},
		{"no reply", nil, approval(0, "files_read", `{"path":"a.md"}`), false},
		{"arguments that are not JSON", []model.Message{reply}, approval(1, "files_write", `{`), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, asked(tt.own, tt.approval))
		})
	}
}
