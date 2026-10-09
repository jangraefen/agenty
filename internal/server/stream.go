package server

import (
	"errors"
	"net/http"
	"strconv"
	"sync"

	"github.com/gin-contrib/sse"
	"github.com/gin-gonic/gin"

	"github.com/jangraefen/agenty/internal/api"
	"github.com/jangraefen/agenty/internal/store"
)

// notifier wakes the readers of a run's events when the run records more.
// It holds no events: the audit log does.
type notifier struct {
	mu sync.Mutex
	// waiting holds, for each run that readers wait for, the channel that
	// notify closes. A run that records nothing more, such as one left
	// waiting as the server stops, keeps its entry: one per such run.
	waiting map[string]chan struct{}
}

// wait returns a channel that is closed once the run records more. A reader
// takes it before it reads, so nothing recorded after the read goes
// unnoticed.
func (n *notifier) wait(run string) <-chan struct{} {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.waiting == nil {
		n.waiting = map[string]chan struct{}{}
	}
	ch, ok := n.waiting[run]
	if !ok {
		ch = make(chan struct{})
		n.waiting[run] = ch
	}
	return ch
}

// notify wakes the readers of the run's events. Call it once what the run
// recorded is committed.
func (n *notifier) notify(run string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if ch, ok := n.waiting[run]; ok {
		close(ch)
		delete(n.waiting, run)
	}
}

// streamPage is how many events a stream reads at a time.
const streamPage = 100

// StreamRunEvents sends a run's events, read from the audit log, after the
// one Last-Event-ID names, and follows a run that has not finished until it
// does. When the server stops, a stream sends what the runs it stopped
// recorded, then ends; those of queued and waiting runs, which the next
// server takes up, end without the run's end.
func (s handlers) StreamRunEvents(c *gin.Context, workspace, id string, params api.StreamRunEventsParams) {
	if params.LastEventID < 0 {
		s.fail(c, http.StatusBadRequest, errors.New("the Last-Event-ID header must name an event by its id"))
		return
	}
	if _, ok := s.ownRun(c, workspace, id); !ok {
		return
	}
	ctx := c.Request.Context()
	after, stopping := params.LastEventID, false
	for {
		changed := s.events.wait(id)
		page, err := s.cfg.Store.RunEvents(ctx, id, after, streamPage)
		if err != nil {
			s.streamFailed(c, id, err)
			return
		}
		for _, e := range page {
			after = e.ID
			if !s.sendEvent(c, workspace, id, e) {
				return
			}
			if e.Finished {
				c.Writer.Flush()
				return
			}
		}
		if len(page) == streamPage {
			continue
		}
		s.startStream(c)
		c.Writer.Flush()
		if stopping {
			return
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return
		case <-s.stopped:
			// The runs the server stopped have recorded their ends: one
			// more read sends them.
			stopping = true
		}
	}
}

// startStream sends the stream's headers, unless it has.
func (s handlers) startStream(c *gin.Context) {
	if !c.Writer.Written() {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Status(http.StatusOK)
		c.Writer.WriteHeaderNow()
	}
}

// sendEvent sends e, an event of the run id, with its id in the audit log,
// and reports whether the stream goes on.
func (s handlers) sendEvent(c *gin.Context, workspace, id string, e store.RunEvent) bool {
	s.startStream(c)
	var name string
	var data any
	switch {
	case e.Record != nil:
		name, data = api.EventAudit, api.FromRecord(e.Record.Record, e.Record.RecordedAt)
	case e.Approval != nil:
		name, data = api.EventApproval, apiApproval(*e.Approval)
	case e.Finished:
		run, err := s.cfg.Store.Run(c.Request.Context(), workspace, id)
		if err != nil {
			s.streamFailed(c, id, err)
			return false
		}
		name, data = api.EventFinished, apiRun(run)
	}
	c.Render(-1, sse.Event{Id: strconv.FormatInt(e.ID, 10), Event: name, Data: data})
	return true
}

// streamFailed answers with err if the stream has not started, and ends it
// otherwise, without the run's end, which tells the reader it was cut short.
func (s handlers) streamFailed(c *gin.Context, id string, err error) {
	if !c.Writer.Written() {
		s.failStore(c, err)
		return
	}
	if c.Request.Context().Err() == nil {
		s.cfg.Logger.Error("cannot read a run's events", "run_id", id, "error", err)
	}
}
