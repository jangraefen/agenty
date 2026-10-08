// Package agent runs harnesses. An Agent is one run of a harness, under the
// run ID its caller minted, with a tool gateway of its own built from the
// harness grants. The agent never calls a tool itself: every tool call the
// model makes goes through that gateway.
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

// Config wires one run of a harness to a model, tools and an audit log.
type Config struct {
	// RunID identifies the run; every audit record of the run carries it. It
	// is required.
	RunID string
	// Records is the audit log of a run that resumes after it stopped at a
	// call waiting for approval: the run's call counts are restored from it,
	// so its limits and policy see the calls it made before.
	Records []toolgateway.Record
	Harness *harness.Harness
	Model   model.Model
	// Servers are tool servers, such as MCP servers, by name. Every granted
	// tool must be served by one. The gateway starts those that serve a
	// granted tool; Close stops them.
	Servers map[string]toolgateway.ToolServer
	// Policy is central policy. It applies to every run, and the harness
	// policy, if any, can only tighten it.
	Policy []policy.Module
	Audit  toolgateway.Audit
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

// Agent is one run of a harness: it holds what the run needs of the harness,
// the model and the run's tool gateway. Run it once, with Continue, or
// with Resume if the run stopped at a call waiting for approval, then Close
// it.
type Agent struct {
	// modelName, instructions and maxSteps are the harness's.
	modelName    harness.Model
	instructions string
	maxSteps     int
	model        model.Model
	gateway      *toolgateway.Gateway
	transcript   Transcript
}

// Result is the outcome of a run. On error it holds what happened up to the
// failure.
type Result struct {
	Output   string
	Steps    int
	Messages []model.Message
}

// New validates cfg, compiles central and harness policy as separate layers,
// builds the run's tool gateway from the harness grants, which starts the
// tool servers they need, and returns an Agent. Close the Agent to stop them.
// The agent keeps the harness's model, instructions and step limit, the
// gateway its grants and tool call limit, and policy is compiled here, so
// later changes to the harness do not affect the agent.
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
		RunID:        cfg.RunID,
		Records:      cfg.Records,
		Harness:      cfg.Harness.Name,
		Granted:      cfg.Harness.Tools,
		Servers:      cfg.Servers,
		MaxToolCalls: cfg.Harness.Limits.MaxToolCalls,
		Policy:       engine,
		Audit:        cfg.Audit,
		Redactor:     cfg.Redactor,
	})
	if err != nil {
		return nil, fmt.Errorf("agent: %w", err)
	}
	return &Agent{
		modelName:    cfg.Harness.Model,
		instructions: cfg.Harness.Instructions,
		maxSteps:     cfg.Harness.Limits.MaxSteps,
		model:        cfg.Model,
		gateway:      gw,
		transcript:   cfg.Transcript,
	}, nil
}

// ID identifies the run; every audit record of the run carries it.
func (a *Agent) ID() string {
	return a.gateway.ID()
}

// PromptDigest identifies what the run sends the model before
// the conversation: the model, the instructions and the tools. Two agents
// with the same digest send the same. The model is identified by its name:
// a name whose model the provider changes is taken as the same model, which
// providers that bind reasoning to a model are expected to handle as a
// switch of models, leaving out what the new one cannot read.
func (a *Agent) PromptDigest() string {
	type tool struct{ Name, Description, InputSchema string }
	prompt := struct {
		Model        harness.Model
		Instructions string
		Tools        []tool
	}{Model: a.modelName, Instructions: a.instructions}
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

// Suspended is the error a run returns when it stops at a call that waits
// for approval. The run's messages end with the reply that makes the call;
// Call is its index in the reply's calls, and Results are the results of the
// calls before it, which Resume needs.
type Suspended struct {
	*toolgateway.Suspended
	Call    int
	Results []model.ToolResult
}

func (s *Suspended) Unwrap() error { return s.Suspended }

// Resumption is how Resume goes on with a suspended run: the call that waits,
// as Suspended named it, and its answer.
type Resumption struct {
	// CallID identifies the call in the audit log.
	CallID string
	// Call is the call's index in the run's last reply, and Results are the
	// results of the calls before it.
	Call    int
	Results []model.ToolResult
	Answer  toolgateway.Approval
	// Note, if set, is appended to the call's result, such as to say that
	// the server the call ran on had to start anew.
	Note string
}

// Continue executes the agent loop for input, as the next turn of a
// conversation: the model sees history, the messages of the earlier runs, if
// any, before input. The history must start with an input and end with the
// model's answer, a reply without tool calls. Only this run's messages are
// recorded and returned, and the harness's limits count this run's steps and
// tool calls alone.
//
// Each step is one model call. Tool calls go through the run's gateway;
// denials and tool errors are reported back to the model, while model
// errors, audit failures and cancellation end the run, and a call that waits
// for approval stops it with a *Suspended error. If the model still asks for
// tools on the last allowed step, those calls are not executed, which the
// transcript records as their results, and Continue returns ErrMaxSteps.
func (a *Agent) Continue(ctx context.Context, history []model.Message, input string) (Result, error) {
	if input == "" {
		return Result{}, errors.New("agent: input is required")
	}
	if err := checkHistory(history); err != nil {
		return Result{}, err
	}
	var res Result
	add := a.adder(ctx, &res)
	if err := add(model.Message{Role: model.RoleUser, Text: input}); err != nil {
		return res, err
	}
	return a.loop(ctx, history, &res, add, 1)
}

// Resume executes a run that stopped at a call waiting for approval, as
// Continue returned it with a *Suspended error, once the call is answered.
// own holds the run's messages so far, which end with the reply whose call
// waits; the call runs, or not, as s says, then the calls after it, and the
// run goes on from there. The harness's limits count the run's steps before
// the suspension too, and its tool calls if the agent was built with the
// run's audit log as Config.Records.
func (a *Agent) Resume(ctx context.Context, history, own []model.Message, s Resumption) (Result, error) {
	if err := checkHistory(history); err != nil {
		return Result{}, err
	}
	var last model.Message
	if len(own) > 0 {
		last = own[len(own)-1]
	}
	switch {
	case last.Role != model.RoleAssistant || len(last.ToolCalls) == 0:
		return Result{}, errors.New("agent: the run to resume does not end with tool calls")
	case s.Call < 0 || s.Call >= len(last.ToolCalls):
		return Result{}, fmt.Errorf("agent: the run's last reply has no call %d", s.Call)
	}
	res := Result{Messages: slices.Clone(own)}
	for _, msg := range own {
		if msg.Role == model.RoleAssistant {
			res.Steps++
		}
	}
	add := a.adder(ctx, &res)
	c := last.ToolCalls[s.Call]
	out, err := a.gateway.Resume(ctx, s.CallID, toolgateway.ToolCall{Name: c.Name, Args: c.Args}, s.Answer)
	if errors.Is(err, toolgateway.ErrAudit) {
		return res, fmt.Errorf("agent: step %d: %w", res.Steps, err)
	}
	result := toolResult(c, out, err)
	if s.Note != "" {
		result.Content += "\n\n" + s.Note
	}
	results, err := callTools(ctx, a.gateway, last.ToolCalls, s.Call+1, append(slices.Clone(s.Results), result))
	if err != nil {
		return res, fmt.Errorf("agent: step %d: %w", res.Steps, err)
	}
	if err := add(model.Message{Role: model.RoleUser, ToolResults: results}); err != nil {
		return res, err
	}
	return a.loop(ctx, history, &res, add, res.Steps+1)
}

// adder returns a function that appends a message to the run's conversation
// in res and records it.
func (a *Agent) adder(ctx context.Context, res *Result) func(model.Message) error {
	return func(msg model.Message) error {
		res.Messages = append(res.Messages, msg)
		if a.transcript == nil {
			return nil
		}
		if err := a.transcript.Append(ctx, a.gateway.ID(), len(res.Messages)-1, msg); err != nil {
			return fmt.Errorf("agent: transcript: %w", err)
		}
		return nil
	}
}

// loop runs the agent loop from step first on: it asks the model, after
// history and the run's messages in res, and runs the tool calls it makes,
// until it answers or the harness's step limit is reached.
func (a *Agent) loop(ctx context.Context, history []model.Message, res *Result, add func(model.Message) error, first int) (Result, error) {
	tools := a.gateway.Definitions()
	if len(tools) == 0 {
		history = callsAsText(history)
	}
	maxSteps := a.maxSteps

	for step := first; step <= maxSteps; step++ {
		msg, err := a.model.Generate(ctx, model.Request{
			System:   a.instructions,
			Messages: append(slices.Clip(history), res.Messages...),
			Tools:    tools,
			History:  len(history),
		})
		if err != nil {
			return *res, fmt.Errorf("agent: step %d: model: %w", step, err)
		}
		msg.Role = model.RoleAssistant
		res.Steps = step
		if err := add(msg); err != nil {
			return *res, err
		}

		if len(msg.ToolCalls) == 0 {
			res.Output = msg.Text
			return *res, nil
		}
		if step == maxSteps {
			// Recorded, so the transcript ends as a conversation can
			// continue from.
			if err := add(notRun(msg.ToolCalls, maxSteps)); err != nil {
				return *res, err
			}
			break
		}
		results, err := callTools(ctx, a.gateway, msg.ToolCalls, 0, nil)
		if err != nil {
			return *res, fmt.Errorf("agent: step %d: %w", step, err)
		}
		if err := add(model.Message{Role: model.RoleUser, ToolResults: results}); err != nil {
			return *res, err
		}
	}
	return *res, fmt.Errorf("agent: %w (%d)", ErrMaxSteps, maxSteps)
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

// notRun answers calls the run does not make, as it reached its step limit.
func notRun(calls []model.ToolCall, maxSteps int) model.Message {
	steps := "steps"
	if maxSteps == 1 {
		steps = "step"
	}
	results := make([]model.ToolResult, len(calls))
	for i, c := range calls {
		results[i] = model.ToolResult{CallID: c.ID, Content: fmt.Sprintf("Not run: the run reached its limit of %d %s.", maxSteps, steps), IsError: true}
	}
	return model.Message{Role: model.RoleUser, ToolResults: results}
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

// callTools runs calls from index from on, in order, through the gateway,
// after results, those of the calls before. Denials and tool errors become
// error results for the model; audit failures and cancellation stop the run
// before any further call, and a call that waits for approval suspends it
// with a *Suspended error.
func callTools(ctx context.Context, gw *toolgateway.Gateway, calls []model.ToolCall, from int, results []model.ToolResult) ([]model.ToolResult, error) {
	for i := from; i < len(calls); i++ {
		c := calls[i]
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		out, err := gw.Call(ctx, toolgateway.ToolCall{Name: c.Name, Args: c.Args})
		var suspended *toolgateway.Suspended
		switch {
		case errors.Is(err, toolgateway.ErrAudit):
			return nil, err
		case errors.As(err, &suspended):
			return nil, &Suspended{Suspended: suspended, Call: i, Results: results}
		}
		results = append(results, toolResult(c, out, err))
	}
	return results, nil
}

// toolResult is the result the model sees of call, which returned out and
// err: a denial or tool error is an error result.
func toolResult(c model.ToolCall, out json.RawMessage, err error) model.ToolResult {
	result := model.ToolResult{CallID: c.ID, Content: string(out)}
	if err != nil {
		result.Content, result.IsError = err.Error(), true
	}
	return result
}
