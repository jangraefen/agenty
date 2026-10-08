package server

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jangraefen/agenty/internal/agent"
	"github.com/jangraefen/agenty/internal/api"
	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/secret"
	"github.com/jangraefen/agenty/internal/store"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

// event is one server-sent event of a run.
type event struct {
	name string
	data any
}

// hub holds the events of one run that has not finished, for any number of
// subscribers.
type hub struct {
	workspace, harness string
	// runID is set once the run has an ID, before it executes.
	runID string
	// ctx is the run's context, and cancel cancels it, with the cause
	// recorded as the run's end.
	ctx    context.Context
	cancel context.CancelCauseFunc

	mu       sync.Mutex
	events   []event
	finished bool
	// changed is closed, and replaced, whenever an event is published.
	changed chan struct{}
}

func newHub(workspace, harness string) *hub {
	return &hub{workspace: workspace, harness: harness, changed: make(chan struct{})}
}

// publish appends e. An EventFinished event is the last: the hub publishes
// nothing after it.
func (h *hub) publish(e event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.finished {
		return
	}
	h.events = append(h.events, e)
	h.finished = e.name == api.EventFinished
	close(h.changed)
	h.changed = make(chan struct{})
}

// stop ends the hub's event streams without an end: the run is left to
// another server.
func (h *hub) stop() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.finished {
		return
	}
	h.finished = true
	close(h.changed)
	h.changed = make(chan struct{})
}

// since returns the events from index i on, a channel that is closed when
// more are published, and whether the run has finished.
func (h *hub) since(i int) ([]event, <-chan struct{}, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.events[i:], h.changed, h.finished
}

// duration formats d without trailing zero units: 1h, not 1h0m0s.
func duration(d time.Duration) string {
	s := d.String()
	s = strings.TrimSuffix(s, "m0s")
	if s != d.String() {
		s += "m"
	}
	if t := strings.TrimSuffix(s, "h0m"); t != s {
		s = t + "h"
	}
	return s
}

// runAudit records to the store and, once a record is stored, publishes it
// to the run's subscribers.
type runAudit struct {
	store *store.Store
	hub   *hub
}

var _ toolgateway.Audit = runAudit{}

// Record stores rec even if the run is being cancelled: a result is recorded
// after its call ran, and must not be lost to a shutdown.
func (a runAudit) Record(ctx context.Context, rec toolgateway.Record) error {
	at, err := a.store.RecordAt(context.WithoutCancel(ctx), rec)
	if err != nil {
		return err
	}
	a.hub.publish(event{api.EventAudit, api.FromRecord(rec, at)})
	return nil
}

// runTranscript redacts each message of a run's conversation and stores it.
type runTranscript struct {
	store  *store.Store
	redact *secret.Redactor
}

var _ agent.Transcript = runTranscript{}

// Append stores msg even if the run is being cancelled, as Record does. A
// message redaction changed is stored as altered.
func (t runTranscript) Append(ctx context.Context, runID string, index int, msg model.Message) error {
	redacted, altered := redactMessage(t.redact, msg)
	return t.store.AppendMessage(context.WithoutCancel(ctx), runID, store.NewMessage{Position: index, Message: redacted, Altered: altered})
}

// redactMessage returns a copy of msg with every secret redact knows of
// redacted, wherever in the message it is, and whether that changed it.
func redactMessage(redact *secret.Redactor, msg model.Message) (model.Message, bool) {
	changed := false
	text := func(s string) string {
		r := redact.String(s)
		changed = changed || r != s
		return r
	}
	raw := func(j json.RawMessage) json.RawMessage {
		r := redact.JSON(j)
		changed = changed || !bytes.Equal(r, j)
		return r
	}
	msg.Text = text(msg.Text)
	msg.ToolCalls = slices.Clone(msg.ToolCalls)
	for i := range msg.ToolCalls {
		msg.ToolCalls[i].Args = raw(msg.ToolCalls[i].Args)
	}
	msg.ToolResults = slices.Clone(msg.ToolResults)
	for i := range msg.ToolResults {
		msg.ToolResults[i].Content = text(msg.ToolResults[i].Content)
	}
	if msg.Provider != nil {
		p := *msg.Provider
		p.Data = raw(p.Data)
		msg.Provider = &p
	}
	return msg, changed
}
