// Package model defines the provider-neutral model interface the agent loop
// talks to. Package modeltest has a scripted implementation for tests.
//
// # Role in the architecture
//
// Agenty owns its agent loop instead of using an agent framework, and this
// package is the seam between that loop and the model providers. Package
// agent builds a Request for every step and calls Model.Generate; package
// anthropic implements Model on the official Anthropic SDK, and package
// modeltest implements it with a script, so the loop is tested without a
// real model. The server (internal/server) picks the implementation for a
// harness's model provider, and the store (internal/store) and the API
// types (internal/api) read and write Message as the unit of a run's
// transcript. The package itself depends only on toolgateway, for the tool
// definitions a request offers.
//
// # Contents and how they fit
//
// A Request holds the instructions, the conversation as a list of Message
// values and the tools the model may call. A Message is one turn: user turns
// carry an input or the ToolResults of the calls before; assistant turns
// carry text and ToolCalls. Each ToolResult answers the ToolCall with the
// same ID, which is how a provider pairs them.
//
// Two fields serve the server's handling of conversations across runs:
// ProviderPart keeps a reply in the provider's own form, so that it can be
// replayed unchanged on a later request, and Usage keeps the tokens of the
// model call that wrote the reply.
//
// # Trust-model guarantees
//
// The model is not trusted (guarantee 1): a ToolCall is only a request,
// which the agent hands to the tool gateway to decide. The tools a Request
// offers are the gateway's definitions of the granted tools alone (3), and
// the tool results it carries come from the gateway, redacted, so no
// credential reaches the model through them (5).
package model

import (
	"context"
	"encoding/json"

	"github.com/jangraefen/agenty/internal/toolgateway"
)

// Model generates the next assistant message for a conversation. It is the
// one thing the agent loop needs of a provider: one call per step, given the
// whole conversation, since provider APIs are stateless. An implementation
// returns an error for a reply the agent cannot use, such as one cut off by
// a limit, rather than passing it on as an answer.
type Model interface {
	Generate(ctx context.Context, req Request) (Message, error)
}

// Request is everything the model sees for one step. The agent builds a new
// one per step from the harness's instructions, the conversation so far and
// the gateway's tool definitions.
type Request struct {
	// System is the harness's instructions.
	System string
	// Messages is the conversation: the earlier runs' messages first, then
	// those of the current run.
	Messages []Message
	// Tools are the tools the model may call, as offered by the gateway.
	Tools []toolgateway.Definition
	// History is how many of the Messages are those of earlier runs of the
	// conversation, which a provider may cache for longer.
	History int
}

// Role says who wrote a message. There are only two: instructions travel in
// Request.System, and tool results are user turns, as the providers have
// them.
type Role string

// The roles of a conversation's messages.
const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is one turn of the conversation. Assistant messages may carry tool
// calls; the user message that follows carries their results.
//
// It is also the form a run's transcript is stored and served in, which is
// why it has JSON tags: the store keeps each message as the model saw it,
// redacted, and the API serves it without the provider's form.
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
	// Usage is the tokens of the model call that wrote a reply, if the
	// provider reports them. A model call that fails, such as one whose
	// reply was cut off, writes no reply, and its tokens are not counted.
	Usage *Usage `json:"usage,omitempty"`
}

// Usage counts the tokens of a model call. Input the provider read from its
// prompt cache, or wrote to it, is counted apart from the rest of the input.
type Usage struct {
	// InputTokens are the input tokens neither read from nor written to the
	// cache.
	InputTokens int64 `json:"input_tokens"`
	// OutputTokens are the tokens of the reply.
	OutputTokens int64 `json:"output_tokens"`
	// CacheWriteTokens are the input tokens written to the prompt cache, and
	// CacheReadTokens those read from it; providers price them apart.
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
}

// ProviderPart is a message in a provider's own form, as JSON, so it can be
// stored and replayed later. Only the provider called Name reads Data.
//
// It exists because a provider may require its earlier replies back exactly
// as it sent them, such as Anthropic's thinking blocks, which a rebuild from
// Text and ToolCalls would lose. Keeping it opaque keeps this package and
// everything above it provider-neutral; a reply whose part names another
// provider is rebuilt from its text and calls instead.
type ProviderPart struct {
	Name string          `json:"name"`
	Data json.RawMessage `json:"data"`
}

// ToolCall is the model asking for a tool to be called. ID is the provider's
// identifier of the call, Name a tool name as the gateway offers it, and
// Args the arguments as the model wrote them. Nothing about it is trusted:
// the gateway decides whether it runs.
type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

// ToolResult answers the tool call with the same ID. Content is the tool's
// output, or the error text of a denied or failed call, with IsError set, so
// the model learns why a call had no effect.
type ToolResult struct {
	CallID  string `json:"call_id"`
	Content string `json:"content"`
	IsError bool   `json:"is_error,omitempty"`
}
