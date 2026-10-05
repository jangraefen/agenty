package model

import (
	"context"
	"errors"
	"slices"
	"sync"
)

// ErrScriptExhausted is returned when a Scripted model has no steps left.
var ErrScriptExhausted = errors.New("model script exhausted")

// Step is one scripted model response: a message, or an error.
type Step struct {
	Response Message
	Err      error
}

// Reply is a step that answers with text and no tool calls.
func Reply(text string) Step {
	return Step{Response: Message{Role: RoleAssistant, Text: text}}
}

// CallTools is a step that asks for the given tool calls.
func CallTools(calls ...ToolCall) Step {
	return Step{Response: Message{Role: RoleAssistant, ToolCalls: calls}}
}

// Fail is a step that returns err.
func Fail(err error) Step {
	return Step{Err: err}
}

var _ Model = (*Scripted)(nil)

// Scripted is a deterministic Model that replays predefined steps and keeps
// a copy of every request it receives.
type Scripted struct {
	mu       sync.Mutex
	steps    []Step
	requests []Request
}

// NewScripted returns a model that replays steps in order.
func NewScripted(steps ...Step) *Scripted {
	return &Scripted{steps: steps}
}

// Generate returns the next step. A cancelled context returns its error
// without consuming a step.
func (s *Scripted) Generate(ctx context.Context, req Request) (Message, error) {
	if err := ctx.Err(); err != nil {
		return Message{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.requests = append(s.requests, cloneRequest(req))
	if len(s.steps) == 0 {
		return Message{}, ErrScriptExhausted
	}
	step := s.steps[0]
	s.steps = s.steps[1:]
	return step.Response, step.Err
}

// Requests returns copies of the requests received so far.
func (s *Scripted) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Request, len(s.requests))
	for i, r := range s.requests {
		out[i] = cloneRequest(r)
	}
	return out
}

func cloneRequest(r Request) Request {
	r.Messages = slices.Clone(r.Messages)
	r.Tools = slices.Clone(r.Tools)
	return r
}
