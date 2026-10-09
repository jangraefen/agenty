package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDuration_IsReadable(t *testing.T) {
	for _, tt := range []struct {
		d    time.Duration
		want string
	}{
		{time.Hour, "1h"},
		{30 * time.Minute, "30m"},
		{90 * time.Minute, "1h30m"},
		{10 * time.Second, "10s"},
		{20 * time.Millisecond, "20ms"},
	} {
		assert.Equal(t, tt.want, duration(tt.d))
	}
}

// TestNotifier_WakesWhoWaitsBeforeTheNotify: a reader that takes its
// channel before it reads is woken by a notify that comes after, whenever
// it looks, and only by its own run's.
func TestNotifier_WakesWhoWaitsBeforeTheNotify(t *testing.T) {
	var n notifier
	n.notify("r1") // nobody waits: nothing to wake, nothing kept
	r1, again, r2 := n.wait("r1"), n.wait("r1"), n.wait("r2")
	assert.Equal(t, r1, again, "readers of one run share its channel")

	n.notify("r1")

	for _, ch := range []<-chan struct{}{r1, again} {
		select {
		case <-ch:
		default:
			t.Fatal("a reader of r1 is not woken")
		}
	}
	select {
	case <-r2:
		t.Fatal("a reader of r2 is woken by r1")
	default:
	}
	select {
	case <-n.wait("r1"):
		t.Fatal("a reader that waits after the notify waits for the next")
	default:
	}
}
