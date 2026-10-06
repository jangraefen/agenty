// Package modeltest provides a scripted model, a deterministic model.Model
// for tests: it replays predefined responses and tool calls, so the agent loop
// is tested without a real model.
package modeltest

import (
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/jangraefen/agenty/internal/model"
)

// ErrScriptExhausted is returned when a Scripted model has no steps left.
var ErrScriptExhausted = errors.New("model script exhausted")

// Step is one scripted model response: a message, or an error.
type Step struct {
	Response model.Message
	Err      error
}

// Reply is a step that answers with text and no tool calls.
func Reply(text string) Step {
	return Step{Response: model.Message{Role: model.RoleAssistant, Text: text}}
}

// CallTools is a step that asks for the given tool calls.
func CallTools(calls ...model.ToolCall) Step {
	return Step{Response: model.Message{Role: model.RoleAssistant, ToolCalls: calls}}
}

// Fail is a step that returns err.
func Fail(err error) Step {
	return Step{Err: err}
}

var _ model.Model = (*Scripted)(nil)

// Scripted is a deterministic model.Model that replays predefined steps and
// keeps a copy of every request it receives.
type Scripted struct {
	mu       sync.Mutex
	steps    []Step
	requests []model.Request
}

// NewScripted returns a model that replays steps in order.
func NewScripted(steps ...Step) *Scripted {
	return &Scripted{steps: steps}
}

// Generate returns the next step. A cancelled context returns its error
// without consuming a step.
func (s *Scripted) Generate(ctx context.Context, req model.Request) (model.Message, error) {
	if err := ctx.Err(); err != nil {
		return model.Message{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.requests = append(s.requests, cloneRequest(req))
	if len(s.steps) == 0 {
		return model.Message{}, ErrScriptExhausted
	}
	step := s.steps[0]
	s.steps = s.steps[1:]
	return step.Response, step.Err
}

// Requests returns copies of the requests received so far.
func (s *Scripted) Requests() []model.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.Request, len(s.requests))
	for i, r := range s.requests {
		out[i] = cloneRequest(r)
	}
	return out
}

func cloneRequest(r model.Request) model.Request {
	r.Messages = slices.Clone(r.Messages)
	r.Tools = slices.Clone(r.Tools)
	return r
}
