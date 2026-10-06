// Package model defines the provider-neutral model interface the agent loop
// talks to. Package modeltest has a scripted implementation for tests.
package model

import (
	"context"
	"encoding/json"

	"github.com/jangraefen/agenty/internal/toolgateway"
)

// Model generates the next assistant message for a conversation.
type Model interface {
	Generate(ctx context.Context, req Request) (Message, error)
}

// Request is everything the model sees for one step.
type Request struct {
	System   string
	Messages []Message
	// Tools are the tools the model may call, as offered by the gateway.
	Tools []toolgateway.Definition
}

// Role says who wrote a message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is one turn of the conversation. Assistant messages may carry tool
// calls; the user message that follows carries their results.
type Message struct {
	Role        Role         `json:"role"`
	Text        string       `json:"text,omitempty"`
	ToolCalls   []ToolCall   `json:"tool_calls,omitempty"`
	ToolResults []ToolResult `json:"tool_results,omitempty"`
	// Provider is the message in the form of the provider that generated it,
	// if it keeps one. The provider replays it unchanged on later requests,
	// which some APIs require, for example for thinking blocks. Other code
	// treats it as opaque data that is stored with the message.
	Provider *ProviderPart `json:"provider,omitempty"`
}

// ProviderPart is a message in a provider's own form, as JSON, so it can be
// stored and replayed later. Only the provider called Name reads Data.
type ProviderPart struct {
	Name string          `json:"name"`
	Data json.RawMessage `json:"data"`
}

// ToolCall is the model asking for a tool to be called.
type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

// ToolResult answers the tool call with the same ID.
type ToolResult struct {
	CallID  string `json:"call_id"`
	Content string `json:"content"`
	IsError bool   `json:"is_error,omitempty"`
}
