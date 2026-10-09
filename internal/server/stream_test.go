package server_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/api"
	"github.com/jangraefen/agenty/internal/model/modeltest"
)

// TestStream_AReaderGoesOnAfterTheLastEventItSaw: every event of a run's
// stream has an id, and a reader that comes back with the last it saw as
// Last-Event-ID gets the events after it, while the run goes on and once it
// has finished.
func TestStream_AReaderGoesOnAfterTheLastEventItSaw(t *testing.T) {
	f := newFixture(t, options{policy: writesNeedApproval})
	f.putNotes(t)
	run, req := f.waitForApproval(t)
	first := f.events(t, run.ID)
	decision := first.next()
	approval := first.next()
	require.Equal(t, api.EventAudit, decision.name)
	require.Equal(t, api.EventApproval, approval.name)
	require.NotEmpty(t, decision.id)
	require.NotEmpty(t, approval.id)
	assert.NotEqual(t, decision.id, approval.id)

	again := f.eventsAfter(t, aliceToken, run.ID, decision.id)
	assert.Equal(t, approval, again.next(), "a waiting run's stream goes on after the event named")
	f.script(modeltest.Reply("written"))
	f.answer(t, req, api.Answer{Approved: true})
	rest := first.rest()
	require.NotEmpty(t, rest)
	end := rest[len(rest)-1]
	require.Equal(t, api.EventFinished, end.name)
	assert.Equal(t, "written", decodeAs[api.Run](t, end).Output)
	assert.Equal(t, rest, again.rest(), "every reader sees the same events")

	replayed := f.eventsAfter(t, aliceToken, run.ID, rest[0].id).rest()
	assert.Equal(t, rest[1:], replayed, "a finished run's stream goes on after the event named too")
	all := f.events(t, run.ID).rest()
	assert.Equal(t, append([]sse{decision, approval}, rest...), all, "a finished run's stream replays every event, its approval requests too")
}

func TestStream_ALastEventIDMustBeAnEventID(t *testing.T) {
	f := newFixture(t, options{})
	f.putNotes(t)
	f.script(modeltest.Reply("done"))
	run := f.startRun(t, "tidy my notes")
	f.finish(t, run.ID)

	for _, id := range []string{"x", "-1", "1.5"} {
		code, body := f.rawWith(t, http.MethodGet, home+"/runs/"+run.ID+"/events", map[string]string{"Last-Event-ID": id})
		assert.Equal(t, http.StatusBadRequest, code, "%s: %s", id, body)
	}
}
