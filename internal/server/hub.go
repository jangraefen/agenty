package server

import (
	"context"
	"crypto/rand"
	"slices"
	"sync"

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

// hub holds the events of one running run, for any number of subscribers,
// and the approvals the run is waiting for. It is also the run's approver.
type hub struct {
	harness string

	mu       sync.Mutex
	events   []event
	finished bool
	// changed is closed, and replaced, whenever an event is published.
	changed chan struct{}
	pending map[string]chan toolgateway.Approval
}

func newHub(harness string) *hub {
	return &hub{harness: harness, changed: make(chan struct{}), pending: map[string]chan toolgateway.Approval{}}
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

// since returns the events from index i on, a channel that is closed when
// more are published, and whether the run has finished.
func (h *hub) since(i int) ([]event, <-chan struct{}, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.events[i:], h.changed, h.finished
}

var _ toolgateway.Approver = (*hub)(nil)

// Approve publishes an approval request and waits until a client answers it
// or ctx ends. The gateway has already redacted req and reasons.
func (h *hub) Approve(ctx context.Context, req toolgateway.Request, reasons []string) (toolgateway.Approval, error) {
	id := rand.Text()
	answer := make(chan toolgateway.Approval, 1)
	h.mu.Lock()
	h.pending[id] = answer
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.pending, id)
		h.mu.Unlock()
	}()

	h.publish(event{api.EventApproval, api.ApprovalRequest{ID: id, Harness: req.Harness, Tool: req.Tool, Args: req.Args, Reasons: reasons}})
	select {
	case <-ctx.Done():
		// An answer that arrives now is accepted but unused: the call is
		// denied as cancelled.
		return toolgateway.Approval{}, ctx.Err()
	case a := <-answer:
		return a, nil
	}
}

// answer answers the pending approval request id. It reports false if the
// run is not waiting for it.
func (h *hub) answer(id string, a toolgateway.Approval) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch, ok := h.pending[id]
	if !ok {
		return false
	}
	delete(h.pending, id)
	ch <- a
	return true
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
	if err := a.store.Record(context.WithoutCancel(ctx), rec); err != nil {
		return err
	}
	a.hub.publish(event{api.EventAudit, rec})
	return nil
}

// runTranscript redacts each message of a run's conversation and stores it.
type runTranscript struct {
	store  *store.Store
	redact *secret.Redactor
}

var _ agent.Transcript = runTranscript{}

// Append stores msg even if the run is being cancelled, as Record does.
func (t runTranscript) Append(ctx context.Context, runID string, index int, msg model.Message) error {
	msg.Text = t.redact.String(msg.Text)
	msg.ToolCalls = slices.Clone(msg.ToolCalls)
	for i := range msg.ToolCalls {
		msg.ToolCalls[i].Args = t.redact.JSON(msg.ToolCalls[i].Args)
	}
	msg.ToolResults = slices.Clone(msg.ToolResults)
	for i := range msg.ToolResults {
		msg.ToolResults[i].Content = t.redact.String(msg.ToolResults[i].Content)
	}
	if msg.Provider != nil {
		p := *msg.Provider
		p.Data = t.redact.JSON(p.Data)
		msg.Provider = &p
	}
	return t.store.AppendMessage(context.WithoutCancel(ctx), runID, index, msg)
}
