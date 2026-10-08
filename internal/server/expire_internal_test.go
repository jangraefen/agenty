package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestExpiryWait_ARequestDueAlreadyIsTriedAgain: the expiry loop waits for
// the next request to expire, never forever while one is pending, also when
// it is due already, as one that fell due while the loop read when it would.
func TestExpiryWait_ARequestDueAlreadyIsTriedAgain(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		next    time.Time
		pending bool
		want    time.Duration
		forever bool
	}{
		{"none pending", time.Time{}, false, 0, true},
		{"one later", now.Add(time.Minute), true, time.Minute, false},
		{"one due now", now, true, expiryRetry, false},
		{"one due before", now.Add(-time.Second), true, expiryRetry, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wait, forever := expiryWait(now, tt.next, tt.pending)

			assert.Equal(t, tt.forever, forever)
			if !tt.forever {
				assert.Equal(t, tt.want, wait)
			}
		})
	}
}
