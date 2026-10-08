package server_test

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/api"
	"github.com/jangraefen/agenty/internal/harness"
	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/model/modeltest"
)

// blockReads makes every files_read call wait until release is called or
// its run is cancelled. Each call sends on the returned channel as it starts.
func (f *fixture) blockReads(t *testing.T) (started <-chan struct{}, release func()) {
	t.Helper()
	calls := make(chan struct{}, 10)
	released := make(chan struct{})
	f.read.OnCall = func(ctx context.Context) {
		calls <- struct{}{}
		select {
		case <-released:
		case <-ctx.Done():
		}
	}
	var done bool
	release = func() {
		if !done {
			done = true
			close(released)
		}
	}
	t.Cleanup(release)
	return calls, release
}

// busy starts a run that holds the fixture's only worker in a files_read
// call until release is called.
func (f *fixture) busy(t *testing.T) (run api.Run, release func()) {
	t.Helper()
	started, release := f.blockReads(t)
	f.script(modeltest.CallTools(call("c1", "files_read", `{"path":"notes.md"}`)), modeltest.Reply("read"))
	run = f.startRun(t, "read my notes")
	<-started
	return run, release
}

// status returns the stored status of a run.
func (f *fixture) status(t *testing.T, id string) api.RunStatus {
	t.Helper()
	var run api.Run
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/runs/"+id, nil, &run))
	return run.Status
}

func TestQueue_ARunWaitsForAFreeWorker(t *testing.T) {
	f := newFixture(t, options{workers: 1})
	f.putNotes(t)
	first, release := f.busy(t)
	m := f.script(modeltest.Reply("second"))

	second := f.startRun(t, "tidy my notes")

	assert.Equal(t, api.RunStatusQueued, f.status(t, second.ID), "the only worker is busy")
	assert.Empty(t, m.Requests())
	release()
	assert.Equal(t, api.RunStatusSucceeded, f.finish(t, first.ID).Status)
	finished := f.finish(t, second.ID)
	assert.Equal(t, api.RunStatusSucceeded, finished.Status)
	assert.Equal(t, "second", finished.Output)
}

func TestQueue_QueuedRunsOutliveARestart(t *testing.T) {
	f := newFixture(t, options{workers: 1})
	f.putNotes(t)
	first, _ := f.busy(t)
	f.script(modeltest.Reply("after the restart"))
	second := f.startRun(t, "tidy my notes")

	f.restart(t, options{workers: 1})

	assert.Equal(t, api.RunStatusFailed, f.status(t, first.ID), "a run that was running cannot continue")
	finished := f.finish(t, second.ID)
	assert.Equal(t, api.RunStatusSucceeded, finished.Status, "a queued run is taken up by the next server")
	assert.Equal(t, "after the restart", finished.Output)
}

func TestQueue_CancelAQueuedRun(t *testing.T) {
	f := newFixture(t, options{workers: 1})
	f.putNotes(t)
	first, release := f.busy(t)
	m := f.script(modeltest.Reply("never"))
	second := f.startRun(t, "tidy my notes")

	require.Equal(t, http.StatusAccepted, f.do(t, http.MethodPost, home+"/runs/"+second.ID+"/cancel", nil, nil))

	cancelled := f.finish(t, second.ID)
	assert.Equal(t, api.RunStatusCancelled, cancelled.Status)
	assert.Equal(t, "cancelled by alice", cancelled.Error)
	release()
	assert.Equal(t, api.RunStatusSucceeded, f.finish(t, first.ID).Status)
	assert.Empty(t, m.Requests(), "a cancelled run is never taken up")
	var resp api.Error
	assert.Equal(t, http.StatusConflict, f.do(t, http.MethodPost, home+"/runs/"+second.ID+"/cancel", nil, &resp))
	assert.Contains(t, resp.Error, "already finished")
}

func TestQueue_AQueuedRunCannotBeFollowedUp(t *testing.T) {
	f := newFixture(t, options{workers: 1})
	f.putNotes(t)
	f.busy(t)
	f.script(modeltest.Reply("ok"))
	queued := f.startRun(t, "tidy my notes")
	var resp api.Error

	status := f.do(t, http.MethodPost, home+"/runs/"+queued.ID+"/follow-up", api.FollowUp{Input: "and then"}, &resp)

	assert.Equal(t, http.StatusConflict, status)
	assert.Contains(t, resp.Error, "has not finished")
}

func TestQueue_ManyWorkersRunAtOnce(t *testing.T) {
	f := newFixture(t, options{workers: 2})
	f.putNotes(t)
	started, release := f.blockReads(t)
	var runs []api.Run
	for range 2 {
		f.script(modeltest.CallTools(call("c1", "files_read", `{"path":"notes.md"}`)), modeltest.Reply("read"))
		runs = append(runs, f.startRun(t, "read my notes"))
	}

	<-started
	<-started
	release()

	for _, run := range runs {
		assert.Equal(t, api.RunStatusSucceeded, f.finish(t, run.ID).Status)
	}
}

// TestQueue_ARunCancelledAsItStartsEndsCancelled: a cancel that comes after
// a worker claimed the run, while it builds the run's model, ends the run as
// cancelled, not failed.
func TestQueue_ARunCancelledAsItStartsEndsCancelled(t *testing.T) {
	building, proceed := make(chan struct{}), make(chan struct{})
	m := modeltest.NewScripted(modeltest.Reply("never"))
	f := newFixture(t, options{newModel: func(harness.Model) (model.Model, error) {
		close(building)
		<-proceed
		return m, nil
	}})
	f.putNotes(t)
	run := f.startRun(t, "tidy my notes")
	<-building

	require.Equal(t, http.StatusAccepted, f.do(t, http.MethodPost, home+"/runs/"+run.ID+"/cancel", nil, nil))
	close(proceed)

	finished := f.finish(t, run.ID)
	assert.Equal(t, api.RunStatusCancelled, finished.Status)
	assert.Equal(t, "cancelled by alice", finished.Error)
	assert.Empty(t, m.Requests())
}

// TestQueue_AQueuedRunsStreamEndsWithTheServer: the next server takes up a
// queued run, so its stream ends with the server without the run's end.
func TestQueue_AQueuedRunsStreamEndsWithTheServer(t *testing.T) {
	f := newFixture(t, options{workers: 1})
	f.putNotes(t)
	f.busy(t)
	f.script(modeltest.Reply("later"))
	queued := f.startRun(t, "tidy my notes")
	events := f.events(t, queued.ID)

	f.server.Close()

	assert.Empty(t, events.rest(), "the stream ends without the run's end")
	assert.Equal(t, api.RunStatusQueued, f.status(t, queued.ID))
}

// TestQueue_ARunFinishingAsItIsReadIsNotOnAnotherServer: a run that ends
// while its events are asked for, or its cancel, is this server's: its
// stream replays it, and its cancel finds it finished or cancels it.
func TestQueue_ARunFinishingAsItIsReadIsNotOnAnotherServer(t *testing.T) {
	f := newFixture(t, options{})
	f.putNotes(t)
	for range 30 {
		f.script(modeltest.Reply("done"))
		run := f.startRun(t, "tidy my notes")
		code, body := f.raw(t, http.MethodGet, home+"/runs/"+run.ID+"/events")
		require.Equal(t, http.StatusOK, code, body)
		f.finish(t, run.ID)

		f.script(modeltest.Reply("done"))
		run = f.startRun(t, "tidy my notes")
		code, body = f.raw(t, http.MethodPost, home+"/runs/"+run.ID+"/cancel")
		if code != http.StatusAccepted {
			require.Equal(t, http.StatusConflict, code, body)
			require.Contains(t, body, "has already finished")
		}
		f.finish(t, run.ID)
	}
}

// raw sends alice's request without a body and returns the response's
// status and body, read to the end.
func (f *fixture) raw(t *testing.T, method, path string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, f.http.URL+path, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	return resp.StatusCode, string(body)
}
