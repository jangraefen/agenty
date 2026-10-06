// Package anthropic is the Anthropic Messages API model provider.
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

const defaultBaseURL = "https://api.anthropic.com"

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
}

var _ model.Model = (*Model)(nil)

// Model generates replies with the Anthropic Messages API.
type Model struct {
	client    sdk.Client
	model     sdk.Model
	maxTokens int64
}

// New validates cfg and returns a Model.
func New(cfg Config) (*Model, error) {
	switch {
	case cfg.APIKey == "":
		return nil, errors.New("anthropic: api key is required")
	case cfg.Model == "":
		return nil, errors.New("anthropic: model is required")
	case cfg.MaxTokens <= 0:
		return nil, errors.New("anthropic: max tokens must be greater than 0")
	}
	client := sdk.NewClient(
		option.WithoutEnvironmentDefaults(),
		option.WithBaseURL(cmp.Or(cfg.BaseURL, defaultBaseURL)),
		option.WithAPIKey(cfg.APIKey),
	)
	return &Model{client: client, model: cfg.Model, maxTokens: int64(cfg.MaxTokens)}, nil
}

// Generate sends the conversation and returns the model's reply. A reply cut
// off by a limit, a refusal, and anything the agent cannot use are errors.
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
// reused from the cache on the next step.
func (m *Model) params(req model.Request) (sdk.MessageNewParams, error) {
	params := sdk.MessageNewParams{
		Model:        m.model,
		MaxTokens:    m.maxTokens,
		CacheControl: sdk.NewCacheControlEphemeralParam(),
	}
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
		params.Messages = append(params.Messages, param)
	}
	return params, nil
}

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
		if p, ok := msg.Provider.(sdk.MessageParam); ok {
			return p, nil
		}
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
	if len(blocks) == 0 {
		return sdk.MessageParam{}, fmt.Errorf("message %d is empty", i)
	}
	if msg.Role == model.RoleUser {
		return sdk.NewUserMessage(blocks...), nil
	}
	return sdk.NewAssistantMessage(blocks...), nil
}

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
	msg.Provider = resp.ToParam()
	return msg, nil
}
