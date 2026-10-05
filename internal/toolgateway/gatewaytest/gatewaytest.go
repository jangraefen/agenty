// Package gatewaytest provides fakes for testing code that uses the tool gateway.
package gatewaytest

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jangraefen/agenty/internal/toolgateway"
)

var _ toolgateway.Tool = (*Tool)(nil)

// Tool is a fake tool that returns a fixed result and counts its calls.
type Tool struct {
	Name   string
	Result json.RawMessage
	Err    error
	// OnCall, if set, runs at the start of every call.
	OnCall func(ctx context.Context)

	Calls int
	// Args holds the arguments of the last call.
	Args json.RawMessage
}

// Definition describes the fake with a generic object schema.
func (t *Tool) Definition() toolgateway.Definition {
	return toolgateway.Definition{
		Name:        t.Name,
		Description: "fake " + t.Name,
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}
}

// Call records the call and returns the configured result and error.
func (t *Tool) Call(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	if t.OnCall != nil {
		t.OnCall(ctx)
	}
	t.Calls++
	t.Args = args
	return t.Result, t.Err
}

// ErrAuditDown is returned by Audit for the event it is set to fail on.
var ErrAuditDown = errors.New("audit store down")

var _ toolgateway.Audit = (*Audit)(nil)

// Audit keeps every record and fails writes of the event in FailOn.
type Audit struct {
	Records []toolgateway.Record
	FailOn  toolgateway.Event
}

// Record stores rec, or returns ErrAuditDown if rec.Event is FailOn.
func (a *Audit) Record(_ context.Context, rec toolgateway.Record) error {
	if rec.Event == a.FailOn {
		return ErrAuditDown
	}
	a.Records = append(a.Records, rec)
	return nil
}

// WithoutCallIDs returns records with CallID cleared, for comparing everything else.
func WithoutCallIDs(records []toolgateway.Record) []toolgateway.Record {
	var out []toolgateway.Record
	for _, r := range records {
		r.CallID = ""
		out = append(out, r)
	}
	return out
}
