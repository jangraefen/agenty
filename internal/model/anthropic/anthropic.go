// Package anthropic is the Anthropic Messages API model provider: the
// production implementation of model.Model, on the official Anthropic Go
// SDK rather than an agent framework.
//
// # Role in the architecture
//
// The server (internal/server) builds a Model per run with New, from the
// harness's model name and the operator config's API key, reply token
// limit, endpoint and history cache TTL. Package agent then calls Generate
// once per step. The package translates in both directions: params maps a
// provider-neutral model.Request to SDK parameters, with toolParam and
// messageParam for its tools and messages, and reply maps the API's answer
// back to a model.Message. Package anthropictest has a fake API that tests
// point BaseURL at.
//
// # Design decisions
//
//   - Replies are replayed as the API sent them. Each reply keeps its raw
//     JSON as a model.ProviderPart, which the store saves with the
//     transcript; when the conversation is sent again, messageParam
//     restores it, so thinking blocks come back unchanged and in place, as
//     the API requires.
//   - Prompt caching: every request carries the automatic cache breakpoint
//     at its end, so the instructions, tools and earlier turns are read
//     from the cache on the next step. A history cache TTL of an hour adds
//     a breakpoint at the end of the earlier runs, so a follow-up after a
//     longer pause still reads them from the cache.
//   - Fail closed: a reply cut off by a limit, a refusal, an unknown stop
//     reason or an unknown content block is an error, never an answer, so
//     the run ends instead of acting on part of a reply.
//
// # Trust-model guarantees
//
// Credentials never reach the model (guarantee 5): the API key travels only
// in the x-api-key header, and the SDK's environment defaults are off, so
// no environment variable can redirect requests, and the key with them, to
// another endpoint, or add credentials or headers. What the model asks for
// is only mapped into model.ToolCall values, which the agent hands to the
// tool gateway (guarantee 1).
package anthropic

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

// defaultBaseURL is Anthropic's API endpoint, set explicitly because the
// SDK's environment defaults, which would supply it, are off.
const defaultBaseURL = "https://api.anthropic.com"

// providerName names this provider's parts of messages; see
// model.ProviderPart.
const providerName = "anthropic"

var (
	// ErrTruncated is returned when a reply was cut off, by the response token
	// limit or the context window, so it cannot be used as an answer.
	ErrTruncated = errors.New("model reply was cut off")
	// ErrRefused is returned when the model refused to answer.
	ErrRefused = errors.New("model refused to answer")
)

// Config configures the Anthropic provider. Nothing is read from the
// environment: the SDK's environment defaults are turned off, so no variable
// can redirect requests or add credentials.
type Config struct {
	// APIKey is sent only in the x-api-key header.
	APIKey string
	// Model is the model to use, such as "claude-sonnet-5-5".
	Model string
	// MaxTokens bounds each reply.
	MaxTokens int
	// BaseURL overrides the API endpoint. Empty means Anthropic's.
	BaseURL string
	// HistoryCacheTTL, if "1h", caches the conversation of earlier runs for
	// an hour; see Model.params. Empty or "5m" leaves it to the automatic
	// breakpoint and its five minutes.
	HistoryCacheTTL string
}

// A compile-time check that *Model implements model.Model.
var _ model.Model = (*Model)(nil)

// Model generates replies with the Anthropic Messages API. It holds no
// conversation state: every Generate sends the whole conversation, so one
// Model could serve any number of runs; the server builds one per run.
type Model struct {
	client    sdk.Client
	model     sdk.Model
	maxTokens int64
	// historyCache is the breakpoint at the end of the earlier runs'
	// conversation, if any.
	historyCache *sdk.CacheControlEphemeralParam
}

// New validates cfg and returns a Model. It fails on a missing key, model or
// reply limit, and on a TTL the API does not offer, so a misconfigured
// provider fails when the run starts instead of on its first model call.
func New(cfg Config) (*Model, error) {
	switch {
	case cfg.APIKey == "":
		return nil, errors.New("anthropic: api key is required")
	case cfg.Model == "":
		return nil, errors.New("anthropic: model is required")
	case cfg.MaxTokens <= 0:
		return nil, errors.New("anthropic: max tokens must be greater than 0")
	}
	var historyCache *sdk.CacheControlEphemeralParam
	switch cfg.HistoryCacheTTL {
	case "", "5m":
	case "1h":
		historyCache = &sdk.CacheControlEphemeralParam{TTL: sdk.CacheControlEphemeralTTLTTL1h}
	default:
		return nil, errors.New(`anthropic: history cache TTL must be "5m" or "1h"`)
	}
	// WithoutEnvironmentDefaults stops the SDK from reading its environment
	// variables, such as ANTHROPIC_BASE_URL, which could send the key to
	// another endpoint, or another key or token, which would add a
	// credential the operator did not configure. Everything the client uses
	// comes from cfg.
	client := sdk.NewClient(
		option.WithoutEnvironmentDefaults(),
		option.WithBaseURL(cmp.Or(cfg.BaseURL, defaultBaseURL)),
		option.WithAPIKey(cfg.APIKey),
	)
	return &Model{client: client, model: cfg.Model, maxTokens: int64(cfg.MaxTokens), historyCache: historyCache}, nil
}

// Generate sends the conversation and returns the model's reply. A reply cut
// off by a limit, a refusal, and anything the agent cannot use are errors.
// It maps the request first, so a conversation the API would refuse fails
// before anything is sent, then calls the API once and maps the reply. Every
// error is prefixed with the provider's name; the SDK's errors are wrapped,
// so callers can inspect API errors with errors.As.
func (m *Model) Generate(ctx context.Context, req model.Request) (model.Message, error) {
	params, err := m.params(req)
	if err != nil {
		return model.Message{}, fmt.Errorf("anthropic: %w", err)
	}
	resp, err := m.client.Messages.New(ctx, params)
	if err != nil {
		return model.Message{}, fmt.Errorf("anthropic: %w", err)
	}
	msg, err := reply(resp)
	if err != nil {
		return model.Message{}, fmt.Errorf("anthropic: %w", err)
	}
	return msg, nil
}

// params maps a request. The top-level cache_control marks the end of the
// request for prompt caching, so instructions, tools and earlier turns are
// reused from the cache on the next step. A history cache adds a breakpoint
// at the end of the earlier runs' conversation, which a follow-up reads
// although the user took longer to reply than the automatic breakpoint's
// five minutes; it comes first, as the API requires of the longer TTL.
func (m *Model) params(req model.Request) (sdk.MessageNewParams, error) {
	params := sdk.MessageNewParams{
		Model:        m.model,
		MaxTokens:    m.maxTokens,
		CacheControl: sdk.NewCacheControlEphemeralParam(),
	}
	// An empty system prompt is left out rather than sent as an empty text
	// block; without tools, the request has no tools field at all.
	if req.System != "" {
		params.System = []sdk.TextBlockParam{{Text: req.System}}
	}
	for _, def := range req.Tools {
		tool, err := toolParam(def)
		if err != nil {
			return params, err
		}
		params.Tools = append(params.Tools, sdk.ToolUnionParam{OfTool: tool})
	}
	for i, msg := range req.Messages {
		param, err := messageParam(i, msg)
		if err != nil {
			return params, err
		}
		// Never the request's last block: the API refuses a TTL there that
		// differs from the automatic breakpoint's.
		if m.historyCache != nil && i == req.History-1 && i < len(req.Messages)-1 {
			markCache(param, *m.historyCache)
		}
		params.Messages = append(params.Messages, param)
	}
	return params, nil
}

// markCache makes the last block of msg that can be a cache breakpoint one.
// Thinking blocks cannot. Searching backwards finds the block nearest the
// end of the message, so the breakpoint covers as much of it as possible.
func markCache(msg sdk.MessageParam, cache sdk.CacheControlEphemeralParam) {
	for _, block := range slices.Backward(msg.Content) {
		if cc := block.GetCacheControl(); cc != nil {
			*cc = cache
			return
		}
	}
}

// toolParam maps a tool the gateway offers to the API's tool form. The API
// takes only object schemas, and the SDK sets their type itself, so the
// schema is checked to be one and passed on without its type key. A tool
// without a schema takes any object; a schema that is not an object, or not
// JSON, is an error.
func toolParam(def toolgateway.Definition) (*sdk.ToolParam, error) {
	raw := def.InputSchema
	if len(raw) == 0 {
		raw = json.RawMessage(`{"type":"object"}`)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, fmt.Errorf("tool %s: input schema: %w", def.Name, err)
	}
	if schema["type"] != "object" {
		return nil, fmt.Errorf("tool %s: input schema: type must be object", def.Name)
	}
	delete(schema, "type")
	tool := &sdk.ToolParam{Name: def.Name, InputSchema: sdk.ToolInputSchemaParam{ExtraFields: schema}}
	if def.Description != "" {
		tool.Description = sdk.String(def.Description)
	}
	return tool, nil
}

// messageParam maps message i of a conversation to the API's form. A reply
// this provider generated is restored from its provider part; any other
// message, an input, tool results, or a reply without a part of this
// provider, is built from its text, calls and results. i only names the
// message in errors.
func messageParam(i int, msg model.Message) (sdk.MessageParam, error) {
	var blocks []sdk.ContentBlockParamUnion
	switch msg.Role {
	case model.RoleUser:
		// Tool results come first, as the API requires.
		for _, r := range msg.ToolResults {
			blocks = append(blocks, sdk.NewToolResultBlock(r.CallID, r.Content, r.IsError))
		}
		if msg.Text != "" {
			blocks = append(blocks, sdk.NewTextBlock(msg.Text))
		}
	case model.RoleAssistant:
		// A reply this provider generated is replayed exactly as the API
		// sent it: thinking blocks must come back unchanged and in place.
		if p := msg.Provider; p != nil && p.Name == providerName {
			var reply sdk.Message
			if err := json.Unmarshal(p.Data, &reply); err != nil {
				return sdk.MessageParam{}, fmt.Errorf("message %d: provider part: %w", i, err)
			}
			return reply.ToParam(), nil
		}
		// Rebuilt from text and calls: a reply of another provider, one
		// without a part, or one whose part the server dropped, as it does
		// when the reply's provider form is no longer valid to replay.
		if msg.Text != "" {
			blocks = append(blocks, sdk.NewTextBlock(msg.Text))
		}
		for _, c := range msg.ToolCalls {
			input := c.Args
			if len(input) == 0 {
				input = json.RawMessage(`{}`)
			}
			if !json.Valid(input) {
				return sdk.MessageParam{}, fmt.Errorf("message %d: tool call %s: input is not valid JSON", i, c.ID)
			}
			blocks = append(blocks, sdk.NewToolUseBlock(c.ID, input, c.Name))
		}
	default:
		return sdk.MessageParam{}, fmt.Errorf("message %d has unknown role %q", i, msg.Role)
	}
	// The API refuses a message without content; failing here names the
	// message instead.
	if len(blocks) == 0 {
		return sdk.MessageParam{}, fmt.Errorf("message %d is empty", i)
	}
	if msg.Role == model.RoleUser {
		return sdk.NewUserMessage(blocks...), nil
	}
	return sdk.NewAssistantMessage(blocks...), nil
}

// reply maps an API response to the assistant message the agent sees: its
// text blocks joined, its tool calls, its usage and its raw JSON as the
// provider part. Only stop reasons that end a complete reply are accepted;
// the others, and unknown ones, are errors, so a new stop reason or block
// type fails closed until it is handled.
func reply(resp *sdk.Message) (model.Message, error) {
	switch resp.StopReason {
	case sdk.StopReasonEndTurn, sdk.StopReasonToolUse, sdk.StopReasonStopSequence:
	case sdk.StopReasonMaxTokens, sdk.StopReasonModelContextWindowExceeded:
		return model.Message{}, fmt.Errorf("%w: stop reason %q", ErrTruncated, resp.StopReason)
	case sdk.StopReasonRefusal:
		return model.Message{}, ErrRefused
	default:
		return model.Message{}, fmt.Errorf("unsupported stop reason %q", resp.StopReason)
	}
	msg := model.Message{Role: model.RoleAssistant}
	var texts []string
	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			texts = append(texts, block.Text)
		case "tool_use":
			msg.ToolCalls = append(msg.ToolCalls, model.ToolCall{ID: block.ID, Name: block.Name, Args: slices.Clone(block.Input)})
		case "thinking", "redacted_thinking":
			// Not part of the answer; kept in msg.Provider for replay.
		default:
			return model.Message{}, fmt.Errorf("unsupported content block %q", block.Type)
		}
	}
	msg.Text = strings.Join(texts, "\n")
	msg.Usage = &model.Usage{
		InputTokens:      resp.Usage.InputTokens,
		OutputTokens:     resp.Usage.OutputTokens,
		CacheWriteTokens: resp.Usage.CacheCreationInputTokens,
		CacheReadTokens:  resp.Usage.CacheReadInputTokens,
	}
	// The reply exactly as the API sent it, to replay later. A reply the SDK
	// did not decode from JSON has none and is rebuilt from text and calls.
	if raw := resp.RawJSON(); raw != "" {
		msg.Provider = &model.ProviderPart{Name: providerName, Data: json.RawMessage(raw)}
	}
	return msg, nil
}
