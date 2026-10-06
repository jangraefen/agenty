package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/toolgateway"
)

// TestHub_WithdrawHonoursAnAcceptedAnswer: an answer the API accepted (204)
// is the one the run uses, even if the request timed out or the run was
// cancelled at the same moment; once withdrawn, no answer is accepted.
func TestHub_WithdrawHonoursAnAcceptedAnswer(t *testing.T) {
	h := newHub("home", "notes", time.Hour)
	p := pendingApproval{answer: make(chan toolgateway.Approval, 1)}
	p.req.ID = "a1"
	h.pending["a1"] = p
	approved := toolgateway.Approval{Approved: true, Approver: "alice", Reason: "ok"}

	require.True(t, h.answer("a1", approved), "the API accepts the answer")
	got, answered := h.withdraw(p)

	assert.True(t, answered)
	assert.Equal(t, approved, got)

	p2 := pendingApproval{answer: make(chan toolgateway.Approval, 1)}
	p2.req.ID = "a2"
	h.pending["a2"] = p2
	_, answered = h.withdraw(p2)
	assert.False(t, answered)
	assert.False(t, h.answer("a2", approved), "a withdrawn request accepts no answer")
}

func TestHub_ApproveTimesOutWithAReadableDuration(t *testing.T) {
	for _, tt := range []struct {
		timeout time.Duration
		want    string
	}{
		{time.Hour, "no answer within 1h"},
		{30 * time.Minute, "no answer within 30m"},
		{90 * time.Minute, "no answer within 1h30m"},
		{10 * time.Second, "no answer within 10s"},
		{20 * time.Millisecond, "no answer within 20ms"},
	} {
		assert.Equal(t, tt.want, "no answer within "+duration(tt.timeout))
	}
	h := newHub("home", "notes", 20*time.Millisecond)
	a, err := h.Approve(context.Background(), toolgateway.Request{}, nil)
	require.NoError(t, err)
	assert.Equal(t, toolgateway.Approval{Reason: "no answer within 20ms"}, a)
	assert.Empty(t, h.waiting())
}

func TestHub_ApproveReportsTheCancelCause(t *testing.T) {
	h := newHub("home", "notes", time.Hour)
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(cancelledBy("bob"))

	_, err := h.Approve(ctx, toolgateway.Request{}, nil)

	var by cancelledBy
	require.ErrorAs(t, err, &by)
	assert.Equal(t, cancelledBy("bob"), by)
}

// TestHub_CancellationWinsOverAnAnswer: an answer accepted at the moment the
// run is cancelled is not used; the call fails with the cancel cause.
func TestHub_CancellationWinsOverAnAnswer(t *testing.T) {
	h := newHub("home", "notes", time.Hour)
	ctx, cancel := context.WithCancelCause(context.Background())
	type result struct {
		a   toolgateway.Approval
		err error
	}
	done := make(chan result, 1)
	go func() {
		a, err := h.Approve(ctx, toolgateway.Request{}, nil)
		done <- result{a, err}
	}()
	require.Eventually(t, func() bool { return len(h.waiting()) == 1 }, 5*time.Second, time.Millisecond)

	// Answer and cancel at once: under the hub's lock, so Approve sees both.
	h.mu.Lock()
	for id, p := range h.pending {
		delete(h.pending, id)
		p.answer <- toolgateway.Approval{Approved: true, Approver: "alice"}
	}
	cancel(cancelledBy("bob"))
	h.mu.Unlock()

	r := <-done
	assert.Equal(t, toolgateway.Approval{}, r.a)
	var by cancelledBy
	require.ErrorAs(t, r.err, &by)
	assert.Equal(t, cancelledBy("bob"), by)
}
