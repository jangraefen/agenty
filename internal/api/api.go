// Package api holds the JSON types of Agenty's HTTP API, shared by the
// server and its clients. schema/openapi.yaml at the module root defines the
// API: its routes, and the types generated from it into api.gen.go. This file
// adds what the spec cannot express in Go: the event names of a run's
// stream, and conversions from and to the platform's own types.
//
// The package is the boundary between the platform's internal types
// (harness, model, toolgateway) and what crosses the wire. Package server
// answers with these types, and the CLI's client decodes them; the web
// frontend generates its TypeScript types from the same spec, so the three
// agree by construction. Keeping the conversions here, not in the server,
// means the internal types can change without changing the API, and what a
// conversion leaves out, such as a reply's provider form, is left out for
// every client alike.
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

// The page sizes of lists, as the spec's Limit parameter gives them.
const (
	DefaultPageLimit = 50
	MaxPageLimit     = 200
)

// FromHarness returns h in its API form, field by field, so the API's JSON
// does not follow the harness package's own encoding.
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

// ToHarness returns h as a harness.Harness. It does not validate it: the
// server validates the result before it stores it.
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

// FromRecord returns rec, recorded at the given time, in its API form. The
// record's run ID is left out, as every route that returns records names the
// run already. Its arguments and result are as the gateway recorded them,
// redacted.
func FromRecord(rec toolgateway.Record, recordedAt time.Time) AuditRecord {
	return AuditRecord{
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

// FromMessage returns msg, at position in its conversation, in its API form:
// what was said, without the provider's own form of a reply, which is kept
// only to send it to the model again.
func FromMessage(position int, msg model.Message) TranscriptMessage {
	out := TranscriptMessage{Position: position, Role: Role(msg.Role), Text: msg.Text}
	for _, c := range msg.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{ID: c.ID, Name: c.Name, Args: c.Args})
	}
	for _, r := range msg.ToolResults {
		out.ToolResults = append(out.ToolResults, ToolResult{CallID: r.CallID, Content: r.Content, IsError: r.IsError})
	}
	return out
}

// FromUsage maps a model call's usage, or a run's sums of it, with the
// tokens read from and written to the prompt cache apart.
func FromUsage(u model.Usage) Usage {
	return Usage{InputTokens: u.InputTokens, OutputTokens: u.OutputTokens, CacheWriteTokens: u.CacheWriteTokens, CacheReadTokens: u.CacheReadTokens}
}
