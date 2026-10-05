package toolgateway_test

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jangraefen/agenty/internal/toolgateway"
)

// fakeTool is a Tool that returns a fixed result and counts its calls.
type fakeTool struct {
	name    string
	result  json.RawMessage
	err     error
	calls   int
	gotArgs json.RawMessage
}

func (f *fakeTool) Name() string { return f.name }

func (f *fakeTool) Call(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	f.calls++
	f.gotArgs = args
	return f.result, f.err
}

var errAuditDown = errors.New("audit store down")

// recordingAudit keeps every record and fails writes of the event in failOn.
type recordingAudit struct {
	records []toolgateway.Record
	failOn  toolgateway.Event
}

func (a *recordingAudit) Record(_ context.Context, r toolgateway.Record) error {
	if r.Event == a.failOn {
		return errAuditDown
	}
	a.records = append(a.records, r)
	return nil
}

// withoutCallIDs returns records with CallID cleared, for comparing everything else.
func withoutCallIDs(records []toolgateway.Record) []toolgateway.Record {
	var out []toolgateway.Record
	for _, r := range records {
		r.CallID = ""
		out = append(out, r)
	}
	return out
}
