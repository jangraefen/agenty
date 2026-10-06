// Package api holds the JSON types of Agenty's HTTP API, shared by the
// server and its clients. schema/openapi.yaml at the module root defines the
// API: its routes, and the types generated from it into api.gen.go. This file adds what the spec
// cannot express in Go: the event names of a run's stream, and conversions
// from and to the platform's own types.
package api

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0 -config oapi-codegen.yaml ../../schema/openapi.yaml

import (
	"time"

	"github.com/jangraefen/agenty/internal/harness"
	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/policy"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

// Event names of the server-sent events of a run. Every event's data is JSON:
// an AuditRecord for EventAudit, an ApprovalRequest for EventApproval, and a
// Run for EventFinished, which is always the last event.
const (
	EventAudit    = "audit"
	EventApproval = "approval"
	EventFinished = "finished"
)

// The page sizes of listing runs, as the spec gives them.
const (
	DefaultRunsLimit = 50
	MaxRunsLimit     = 200
)

// FromHarness returns h in its API form.
func FromHarness(h harness.Harness) Harness {
	out := Harness{
		Name:         h.Name,
		Instructions: h.Instructions,
		Model:        HarnessModel{Provider: h.Model.Provider, Name: h.Model.Name},
		Tools:        h.Tools,
		Limits:       Limits{MaxSteps: h.Limits.MaxSteps, MaxToolCalls: h.Limits.MaxToolCalls},
	}
	for _, m := range h.Policy {
		out.Policy = append(out.Policy, PolicyModule{Name: m.Name, Source: m.Source})
	}
	return out
}

// ToHarness returns h as a harness.Harness.
func (h Harness) ToHarness() harness.Harness {
	out := harness.Harness{
		Name:         h.Name,
		Instructions: h.Instructions,
		Model:        harness.Model{Provider: h.Model.Provider, Name: h.Model.Name},
		Tools:        h.Tools,
		Limits:       harness.Limits{MaxSteps: h.Limits.MaxSteps, MaxToolCalls: h.Limits.MaxToolCalls},
	}
	for _, m := range h.Policy {
		out.Policy = append(out.Policy, policy.Module{Name: m.Name, Source: m.Source})
	}
	return out
}

// FromRecord returns rec, recorded at the given time, in its API form.
func FromRecord(rec toolgateway.Record, recordedAt time.Time) AuditRecord {
	return AuditRecord{
		RunID:      rec.RunID,
		CallID:     rec.CallID,
		Event:      AuditEvent(rec.Event),
		Tool:       rec.Tool,
		Args:       rec.Args,
		Decision:   Decision(rec.Decision),
		Reason:     rec.Reason,
		Approver:   rec.Approver,
		Result:     rec.Result,
		Error:      rec.Err,
		RecordedAt: recordedAt,
	}
}

// FromMessage returns msg, at position in its conversation and stored at the
// given time, in its API form.
func FromMessage(position int, msg model.Message, createdAt time.Time) TranscriptMessage {
	out := TranscriptMessage{Position: position, Role: Role(msg.Role), Text: msg.Text, CreatedAt: createdAt}
	for _, c := range msg.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{ID: c.ID, Name: c.Name, Args: c.Args})
	}
	for _, r := range msg.ToolResults {
		out.ToolResults = append(out.ToolResults, ToolResult{CallID: r.CallID, Content: r.Content, IsError: r.IsError})
	}
	if p := msg.Provider; p != nil {
		out.Provider = ProviderPart{Name: p.Name, Data: p.Data}
	}
	return out
}
