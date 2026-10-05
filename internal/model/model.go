// Package model defines the provider-neutral model interface the agent loop
// talks to, and a scripted implementation for deterministic tests.
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
	Role        Role
	Text        string
	ToolCalls   []ToolCall
	ToolResults []ToolResult
}

// ToolCall is the model asking for a tool to be called.
type ToolCall struct {
	ID   string
	Name string
	Args json.RawMessage
}

// ToolResult answers the tool call with the same ID.
type ToolResult struct {
	CallID  string
	Content string
	IsError bool
}
