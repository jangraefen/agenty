// Package agent runs harnesses. An Agent holds a harness and its wiring; each
// Run gets a fresh run ID and its own tool gateway, built from the harness
// grants. The agent never calls a tool itself: every tool call the model makes
// goes through that gateway.
package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/jangraefen/agenty/internal/harness"
	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/policy"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

// ErrMaxSteps is returned when the model still asks for tools on the last
// step the harness allows.
var ErrMaxSteps = errors.New("max steps reached")

// Config wires a harness to a model, tools and an audit log.
type Config struct {
	Harness *harness.Harness
	Model   model.Model
	// Tools are the in-process executors available to runs. Only those the
	// harness grants are reachable, and only through the run's gateway.
	Tools []toolgateway.Tool
	// Servers are tool servers, such as MCP servers, by name. The gateway
	// starts those that serve a granted tool; Close stops them.
	Servers map[string]toolgateway.ToolServer
	// Policy is central policy. It applies to every run, and the harness
	// policy, if any, can only tighten it.
	Policy []policy.Module
	// Approver answers calls that policy marks as requiring approval. Without
	// one, such calls are denied.
	Approver toolgateway.Approver
	Audit    toolgateway.Audit
	// Secrets are credential values, such as the model API key and MCP server
	// tokens, that the gateway redacts from everything it hands on.
	Secrets []string
}

// Agent runs one harness. It holds the harness, the model and the tool gateway,
// but no run state, so it can run many times, also at once.
type Agent struct {
	harness harness.Harness
	model   model.Model
	gateway *toolgateway.Gateway
}

// Result is the outcome of a run. On error it holds what happened up to the
// failure.
type Result struct {
	RunID    string
	Output   string
	Steps    int
	Messages []model.Message
}

// New validates cfg, compiles central and harness policy as separate layers,
// builds the tool gateway from the harness grants, which starts the tool
// servers they need, and returns an Agent. Close the Agent to stop them. The
// harness is copied and the gateway copies the grants, so later changes to the
// harness do not affect the agent. A harness that names a policy must come
// with its source.
func New(ctx context.Context, cfg Config) (*Agent, error) {
	switch {
	case cfg.Harness == nil:
		return nil, errors.New("agent: harness is required")
	case cfg.Model == nil:
		return nil, errors.New("agent: model is required")
	case cfg.Audit == nil:
		return nil, errors.New("agent: audit is required")
	}
	if err := cfg.Harness.Validate(); err != nil {
		return nil, fmt.Errorf("agent: invalid harness: %w", err)
	}
	layers := []policy.Layer{{Name: "central", Modules: cfg.Policy}}
	if p := cfg.Harness.Policy; p != nil {
		var modules []policy.Module
		for i, file := range p.Files {
			if i >= len(p.FileSources) {
				return nil, fmt.Errorf("agent: harness policy %q was not loaded", file)
			}
			modules = append(modules, policy.Module{Name: file, Source: p.FileSources[i]})
		}
		if p.Rules != "" {
			modules = append(modules, policy.RulesModule(cfg.Harness.Name+" (inline policy)", p.Rules))
		}
		layers = append(layers, policy.Layer{Name: "harness", Modules: modules})
	}
	engine, err := policy.New(ctx, layers...)
	if err != nil {
		return nil, fmt.Errorf("agent: %w", err)
	}
	gw, err := toolgateway.New(ctx, toolgateway.Config{
		Harness:      cfg.Harness.Name,
		Granted:      cfg.Harness.Tools,
		Tools:        cfg.Tools,
		Servers:      cfg.Servers,
		MaxToolCalls: cfg.Harness.Limits.MaxToolCalls,
		Policy:       engine,
		Approver:     cfg.Approver,
		Audit:        cfg.Audit,
		Secrets:      cfg.Secrets,
	})
	if err != nil {
		return nil, fmt.Errorf("agent: %w", err)
	}
	return &Agent{harness: *cfg.Harness, model: cfg.Model, gateway: gw}, nil
}

// Tools describes the tools runs may call: granted and found, sorted by name.
func (a *Agent) Tools() []toolgateway.Definition {
	return a.gateway.Definitions()
}

// Close stops the tool servers the agent started.
func (a *Agent) Close() error {
	if err := a.gateway.Close(); err != nil {
		return fmt.Errorf("agent: %w", err)
	}
	return nil
}

// Run executes the agent loop for input. Each step is one model call. Tool
// calls go through the gateway, in a gateway run of their own; denials and tool errors are reported
// back to the model, while model errors, audit failures and cancellation end
// the run. If the model still asks for tools on the last allowed step, those
// calls are not executed and Run returns ErrMaxSteps.
func (a *Agent) Run(ctx context.Context, input string) (Result, error) {
	if input == "" {
		return Result{}, errors.New("agent: input is required")
	}
	run := a.gateway.Start()
	res := Result{
		RunID:    run.ID(),
		Messages: []model.Message{{Role: model.RoleUser, Text: input}},
	}
	tools := a.gateway.Definitions()
	maxSteps := a.harness.Limits.MaxSteps

	for step := 1; step <= maxSteps; step++ {
		msg, err := a.model.Generate(ctx, model.Request{
			System:   a.harness.Instructions,
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
		if step == maxSteps {
			break
		}
		results, err := callTools(ctx, run, msg.ToolCalls)
		if err != nil {
			return res, fmt.Errorf("agent: step %d: %w", step, err)
		}
		res.Messages = append(res.Messages, model.Message{Role: model.RoleUser, ToolResults: results})
	}
	return res, fmt.Errorf("agent: %w (%d)", ErrMaxSteps, maxSteps)
}

// callTools runs calls in order through the gateway. Denials and tool errors
// become error results for the model; audit failures and cancellation stop
// the run before any further call.
func callTools(ctx context.Context, run *toolgateway.Run, calls []model.ToolCall) ([]model.ToolResult, error) {
	results := make([]model.ToolResult, 0, len(calls))
	for _, c := range calls {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		out, err := run.Call(ctx, toolgateway.ToolCall{Name: c.Name, Args: c.Args})
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
