// Package agent runs harnesses. An Agent holds a harness and its wiring; each
// Run gets a fresh run ID and its own tool gateway, built from the harness
// grants. The agent never calls a tool itself: every tool call the model makes
// goes through that gateway.
package agent

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/jangraefen/agenty/internal/harness"
	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/must"
	"github.com/jangraefen/agenty/internal/policy"
	"github.com/jangraefen/agenty/internal/secret"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

// ErrMaxSteps is returned when the model still asks for tools on the last
// step the harness allows.
var ErrMaxSteps = errors.New("max steps reached")

// Config wires a harness to a model, tools and an audit log.
type Config struct {
	Harness *harness.Harness
	Model   model.Model
	// Servers are tool servers, such as MCP servers, by name. Every granted
	// tool must be served by one. The gateway starts those that serve a
	// granted tool; Close stops them.
	Servers map[string]toolgateway.ToolServer
	// Policy is central policy. It applies to every run, and the harness
	// policy, if any, can only tighten it.
	Policy []policy.Module
	// Approver answers calls that policy marks as requiring approval. Without
	// one, such calls are denied.
	Approver toolgateway.Approver
	Audit    toolgateway.Audit
	// Redactor holds credentials, such as the model API key and MCP server
	// tokens, that the gateway redacts from everything it hands on. It is
	// required.
	Redactor *secret.Redactor
	// Transcript, if set, records every message of a run's conversation as
	// it joins it. Without one, the transcript is only in Result.
	Transcript Transcript
}

// Transcript records the conversation of runs: the input, every model reply
// and every set of tool results, with its index in the conversation. An
// error ends the run before anything further happens, so a transcript never
// misses a message the run went on from. Messages are as the model saw and
// wrote them, not redacted: an implementation that stores or shows them must
// redact them itself.
type Transcript interface {
	Append(ctx context.Context, runID string, index int, msg model.Message) error
}

// Agent runs one harness. It holds the harness, the model and the tool gateway,
// but no run state, so it can run many times, also at once.
type Agent struct {
	harness    harness.Harness
	model      model.Model
	gateway    *toolgateway.Gateway
	transcript Transcript
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
// harness do not affect the agent.
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
	if len(cfg.Harness.Policy) > 0 {
		layers = append(layers, policy.Layer{Name: "harness", Modules: cfg.Harness.Policy})
	}
	engine, err := policy.New(ctx, layers...)
	if err != nil {
		return nil, fmt.Errorf("agent: %w", err)
	}
	gw, err := toolgateway.New(ctx, toolgateway.Config{
		Harness:      cfg.Harness.Name,
		Granted:      cfg.Harness.Tools,
		Servers:      cfg.Servers,
		MaxToolCalls: cfg.Harness.Limits.MaxToolCalls,
		Policy:       engine,
		Approver:     cfg.Approver,
		Audit:        cfg.Audit,
		Redactor:     cfg.Redactor,
	})
	if err != nil {
		return nil, fmt.Errorf("agent: %w", err)
	}
	return &Agent{harness: *cfg.Harness, model: cfg.Model, gateway: gw, transcript: cfg.Transcript}, nil
}

// Tools describes the tools runs may call: the granted ones, sorted by name.
func (a *Agent) Tools() []toolgateway.Definition {
	return a.gateway.Definitions()
}

// PromptDigest identifies what every run of the agent sends the model before
// the conversation: the model, the instructions and the tools. Two agents
// with the same digest send the same.
func (a *Agent) PromptDigest() string {
	type tool struct{ Name, Description, InputSchema string }
	prompt := struct {
		Model        harness.Model
		Instructions string
		Tools        []tool
	}{Model: a.harness.Model, Instructions: a.harness.Instructions}
	for _, def := range a.gateway.Definitions() {
		prompt.Tools = append(prompt.Tools, tool{def.Name, def.Description, string(def.InputSchema)})
	}
	// Strings and structs of them always marshal.
	sum := sha256.Sum256(must.Value(json.Marshal(prompt)))
	return hex.EncodeToString(sum[:])
}

// Close stops the tool servers the agent started.
func (a *Agent) Close() error {
	if err := a.gateway.Close(); err != nil {
		return fmt.Errorf("agent: %w", err)
	}
	return nil
}

// Run starts a run and executes it on input; see Run.Execute.
func (a *Agent) Run(ctx context.Context, input string) (Result, error) {
	return a.Start().Execute(ctx, input)
}

// Start begins a run without executing it. Its ID is known before it
// executes, so a caller can store the run before any audit record refers to
// it.
func (a *Agent) Start() *Run {
	return &Run{agent: a, gateway: a.gateway.Start()}
}

// Run is one run of an agent, in a gateway run of its own.
type Run struct {
	agent    *Agent
	gateway  *toolgateway.Run
	executed atomic.Bool
}

// ID identifies the run; every audit record of the run carries it.
func (r *Run) ID() string {
	return r.gateway.ID()
}

// Execute executes the agent loop for input, once. Each step is one model
// call. Tool calls go through the run's gateway; denials and tool errors are
// reported back to the model, while model errors, audit failures and
// cancellation end the run. If the model still asks for tools on the last
// allowed step, those calls are not executed and Execute returns ErrMaxSteps.
func (r *Run) Execute(ctx context.Context, input string) (Result, error) {
	return r.Continue(ctx, nil, input)
}

// Continue executes the run as the next turn of a conversation: the model
// sees history, the messages of the earlier runs, before input. The history
// must start with an input and end with the model's answer, a reply without
// tool calls. Only this run's messages are recorded and returned, and the
// harness's limits count this run's steps and tool calls alone. Otherwise it
// is Execute.
func (r *Run) Continue(ctx context.Context, history []model.Message, input string) (Result, error) {
	if input == "" {
		return Result{}, errors.New("agent: input is required")
	}
	if err := checkHistory(history); err != nil {
		return Result{}, err
	}
	if r.executed.Swap(true) {
		return Result{}, errors.New("agent: run already executed")
	}
	a, run := r.agent, r.gateway
	res := Result{RunID: run.ID()}
	// add appends msg to the conversation and records it.
	add := func(msg model.Message) error {
		res.Messages = append(res.Messages, msg)
		if a.transcript == nil {
			return nil
		}
		if err := a.transcript.Append(ctx, run.ID(), len(res.Messages)-1, msg); err != nil {
			return fmt.Errorf("agent: transcript: %w", err)
		}
		return nil
	}
	if err := add(model.Message{Role: model.RoleUser, Text: input}); err != nil {
		return res, err
	}
	tools := a.gateway.Definitions()
	if len(tools) == 0 {
		history = callsAsText(history)
	}
	maxSteps := a.harness.Limits.MaxSteps

	for step := 1; step <= maxSteps; step++ {
		msg, err := a.model.Generate(ctx, model.Request{
			System:   a.harness.Instructions,
			Messages: append(slices.Clip(history), res.Messages...),
			Tools:    tools,
		})
		if err != nil {
			return res, fmt.Errorf("agent: step %d: model: %w", step, err)
		}
		msg.Role = model.RoleAssistant
		res.Steps = step
		if err := add(msg); err != nil {
			return res, err
		}

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
		if err := add(model.Message{Role: model.RoleUser, ToolResults: results}); err != nil {
			return res, err
		}
	}
	return res, fmt.Errorf("agent: %w (%d)", ErrMaxSteps, maxSteps)
}

// checkHistory checks that history, if any, is a conversation a run can
// continue: one that starts with an input and ends with the model's answer.
func checkHistory(history []model.Message) error {
	if len(history) == 0 {
		return nil
	}
	first, last := history[0], history[len(history)-1]
	switch {
	case first.Role != model.RoleUser || len(first.ToolResults) > 0:
		return errors.New("agent: the conversation to continue does not start with an input")
	case last.Role != model.RoleAssistant || len(last.ToolCalls) > 0:
		return errors.New("agent: the conversation to continue does not end with an answer")
	}
	return nil
}

// callsAsText returns history with its tool calls and results written as
// text, for a model that is offered no tools: providers refuse tool calls in
// a conversation without tools. A reply with calls loses its provider form,
// which holds them too.
func callsAsText(history []model.Message) []model.Message {
	out := make([]model.Message, len(history))
	names := map[string]string{}
	for i, msg := range history {
		var parts []string
		if msg.Text != "" {
			parts = append(parts, msg.Text)
		}
		for _, c := range msg.ToolCalls {
			names[c.ID] = c.Name
			parts = append(parts, fmt.Sprintf("[called %s with %s]", c.Name, cmp.Or(string(c.Args), "{}")))
		}
		for _, r := range msg.ToolResults {
			outcome := "returned"
			if r.IsError {
				outcome = "failed"
			}
			parts = append(parts, fmt.Sprintf("[%s %s: %s]", cmp.Or(names[r.CallID], "a tool"), outcome, r.Content))
		}
		if len(msg.ToolCalls) > 0 || len(msg.ToolResults) > 0 {
			msg = model.Message{Role: msg.Role, Text: strings.Join(parts, "\n\n")}
		}
		out[i] = msg
	}
	return out
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
