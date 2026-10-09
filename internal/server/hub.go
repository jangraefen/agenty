package server

import (
	"context"
	"sync"

	"github.com/jangraefen/agenty/internal/api"
)

// event is one server-sent event of a run: its name, one of api's Event
// constants, and its data, which the stream sends as JSON.
type event struct {
	name string
	data any
}

// hub holds the events of one run that has not finished, for any number of
// subscribers.
//
// It is an append-only list, not a set of subscriber channels: a subscriber
// reads from any index, so one that connects late gets every event from the
// first, and a slow one holds nothing up. Waiting for more is waiting on
// changed, which publish closes, waking every subscriber at once, and
// replaces. A hub lives in Server.runs from enqueue, or New for a run an
// earlier server left, until finish, cancelIdle or cancelLeft unregisters it.
// It also carries the run's context, so a cancel reaches the run through it
// whether a worker runs it or not.
type hub struct {
	runID, workspace, harness string
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

// newHub returns an empty hub for a run; register gives it its context.
func newHub(runID, workspace, harness string) *hub {
	return &hub{runID: runID, workspace: workspace, harness: harness, changed: make(chan struct{})}
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

// stop ends the hub's event streams without an end: the run is left to the
// next server.
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
