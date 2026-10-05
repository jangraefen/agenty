// Package agent runs the agent loop of a harness. The loop owns no tools: every
// tool call the model makes goes through the run's tool gateway.
package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/jangraefen/agenty/internal/harness"
	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

// ErrMaxSteps is returned when the model still asks for tools on the last
// step the harness allows.
var ErrMaxSteps = errors.New("max steps reached")

// Config is one run of a harness.
type Config struct {
	Harness *harness.Harness
	Model   model.Model
	// Gateway is the only way the run reaches tools. It must be built for
	// this run from the harness grants.
	Gateway *toolgateway.Gateway
	// Input is the first user message.
	Input string
}

// Result is the outcome of a run. On error it holds what happened up to the
// failure.
type Result struct {
	Output   string
	Steps    int
	Messages []model.Message
}

// Run executes the agent loop. Each step is one model call. Tool calls go
// through the gateway; denials and tool errors are reported back to the model,
// while model errors, audit failures and cancellation end the run. If the model
// still asks for tools on the last allowed step, those calls are not executed
// and Run returns ErrMaxSteps.
func Run(ctx context.Context, cfg Config) (Result, error) {
	if err := cfg.validate(); err != nil {
		return Result{}, err
	}
	h := cfg.Harness
	tools := toolDefinitions(cfg.Gateway)
	res := Result{Messages: []model.Message{{Role: model.RoleUser, Text: cfg.Input}}}

	for step := 1; step <= h.Limits.MaxSteps; step++ {
		msg, err := cfg.Model.Generate(ctx, model.Request{
			System:   h.Instructions,
			Messages: res.Messages,
			Tools:    tools,
		})
		if err != nil {
			return res, fmt.Errorf("agent: step %d: model: %w", step, err)
		}
		msg.Role = model.RoleAssistant
		res.Steps = step
		res.Messages = append(res.Messages, msg)

		if len(msg.ToolCalls) == 0 {
			res.Output = msg.Text
			return res, nil
		}
		if step == h.Limits.MaxSteps {
			break
		}
		results, err := callTools(ctx, cfg.Gateway, msg.ToolCalls)
		if err != nil {
			return res, fmt.Errorf("agent: step %d: %w", step, err)
		}
		res.Messages = append(res.Messages, model.Message{Role: model.RoleUser, ToolResults: results})
	}
	return res, fmt.Errorf("agent: %w (%d)", ErrMaxSteps, h.Limits.MaxSteps)
}

// callTools runs calls in order through the gateway. Denials and tool errors
// become error results for the model; audit failures and cancellation stop
// the run before any further call.
func callTools(ctx context.Context, gw *toolgateway.Gateway, calls []model.ToolCall) ([]model.ToolResult, error) {
	results := make([]model.ToolResult, 0, len(calls))
	for _, c := range calls {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		out, err := gw.Call(ctx, toolgateway.ToolCall{Name: c.Name, Args: c.Args})
		if errors.Is(err, toolgateway.ErrAudit) {
			return nil, err
		}
		result := model.ToolResult{CallID: c.ID, Content: string(out)}
		if err != nil {
			result.Content, result.IsError = err.Error(), true
		}
		results = append(results, result)
	}
	return results, nil
}

func toolDefinitions(gw *toolgateway.Gateway) []model.ToolDefinition {
	var defs []model.ToolDefinition
	for _, d := range gw.Definitions() {
		defs = append(defs, model.ToolDefinition{Name: d.Name, Description: d.Description, InputSchema: d.InputSchema})
	}
	return defs
}

func (cfg Config) validate() error {
	switch {
	case cfg.Harness == nil:
		return errors.New("agent: harness is required")
	case cfg.Model == nil:
		return errors.New("agent: model is required")
	case cfg.Gateway == nil:
		return errors.New("agent: gateway is required")
	case cfg.Input == "":
		return errors.New("agent: input is required")
	}
	if err := cfg.Harness.Validate(); err != nil {
		return fmt.Errorf("agent: invalid harness: %w", err)
	}
	return nil
}
