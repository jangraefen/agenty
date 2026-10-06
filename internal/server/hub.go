package server

import (
	"context"
	"crypto/rand"
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

// hub holds the events of one running run, for any number of subscribers,
// and the approvals the run is waiting for. It is also the run's approver.
type hub struct {
	workspace, harness string
	// runID is set once the run has an ID, before it executes.
	runID string
	// timeout is how long an approval request waits for an answer.
	timeout time.Duration
	// cancel cancels the run, with the cause recorded as its end.
	cancel context.CancelCauseFunc

	mu       sync.Mutex
	events   []event
	finished bool
	// changed is closed, and replaced, whenever an event is published.
	changed chan struct{}
	pending map[string]pendingApproval
}

// pendingApproval is an approval request waiting for its answer.
type pendingApproval struct {
	req    api.ApprovalRequest
	answer chan toolgateway.Approval
}

func newHub(workspace, harness string, timeout time.Duration) *hub {
	return &hub{workspace: workspace, harness: harness, timeout: timeout, changed: make(chan struct{}), pending: map[string]pendingApproval{}}
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

// Approve publishes an approval request and waits until a client answers it,
// the request times out, which rejects the call, or ctx ends, which fails it
// with ctx's cause. The gateway
// has already redacted req and reasons.
func (h *hub) Approve(ctx context.Context, req toolgateway.Request, reasons []string) (toolgateway.Approval, error) {
	now := time.Now()
	p := pendingApproval{
		req: api.ApprovalRequest{
			ID: rand.Text(), RunID: h.runID, Harness: req.Harness, Tool: req.Tool, Args: req.Args, Reasons: reasons,
			CreatedAt: now, ExpiresAt: now.Add(h.timeout),
		},
		answer: make(chan toolgateway.Approval, 1),
	}
	h.mu.Lock()
	h.pending[p.req.ID] = p
	h.mu.Unlock()

	h.publish(event{api.EventApproval, p.req})
	timer := time.NewTimer(h.timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		// Cancellation wins over an answer accepted at the same moment.
		h.withdraw(p)
		return toolgateway.Approval{}, context.Cause(ctx)
	case <-timer.C:
		if a, answered := h.withdraw(p); answered {
			return a, nil
		}
		return toolgateway.Approval{Reason: "no answer within " + duration(h.timeout)}, nil
	case a := <-p.answer:
		if ctx.Err() != nil {
			// Both were ready, and select picked the answer.
			return toolgateway.Approval{}, context.Cause(ctx)
		}
		return a, nil
	}
}

// withdraw takes p off the pending requests, so no answer is accepted from
// now on. If an answer was accepted before, it returns that answer; a
// request that timed out honours it, as the client was told it counts.
func (h *hub) withdraw(p pendingApproval) (toolgateway.Approval, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.pending[p.req.ID]; ok {
		delete(h.pending, p.req.ID)
		return toolgateway.Approval{}, false
	}
	// answer removed it and sent its answer, under the lock.
	return <-p.answer, true
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

// waiting returns the approval requests the run is waiting for.
func (h *hub) waiting() []api.ApprovalRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]api.ApprovalRequest, 0, len(h.pending))
	for _, p := range h.pending {
		out = append(out, p.req)
	}
	return out
}

// answer answers the pending approval request id. It reports false if the
// run is not waiting for it.
func (h *hub) answer(id string, a toolgateway.Approval) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	p, ok := h.pending[id]
	if !ok {
		return false
	}
	delete(h.pending, id)
	p.answer <- a
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
