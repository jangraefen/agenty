package toolgateway

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestPool_ExpiryOfAServerTakenSince: a timer that fires as its server is
// taken stops nothing.
func TestPool_ExpiryOfAServerTakenSince(t *testing.T) {
	pool := NewPool(map[string]time.Duration{"files": time.Hour}, slog.New(slog.DiscardHandler))

	pool.expire(poolKey{"conv-1", "files"}, &idleSession{})

	assert.Empty(t, pool.idle)
}
