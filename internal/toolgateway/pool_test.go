package toolgateway_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/toolgateway"
	"github.com/jangraefen/agenty/internal/toolgateway/gatewaytest"
)

// use starts every server of lease, as a gateway does, and stops them.
func use(t *testing.T, lease *toolgateway.Lease) {
	t.Helper()
	for name, srv := range lease.Servers() {
		session, err := srv.Start(context.Background(), name)
		require.NoError(t, err)
		_, err = session.Tools(context.Background())
		require.NoError(t, err)
		require.NoError(t, session.Close())
	}
}

func TestPool_KeepsAConversationsServersForItsNextRun(t *testing.T) {
	files := &gatewaytest.Server{}
	pool := toolgateway.NewPool(map[string]time.Duration{"files": time.Hour}, slog.New(slog.DiscardHandler))
	t.Cleanup(func() { assert.NoError(t, pool.Close()) })
	servers := map[string]toolgateway.ToolServer{"files": files}

	first := pool.Lease("conv-1", servers)
	use(t, first)
	assert.Equal(t, []string{"files"}, first.Fresh())
	require.NoError(t, first.Keep())
	assert.Zero(t, files.Closed, "a kept server keeps running")

	second := pool.Lease("conv-1", servers)
	use(t, second)
	require.NoError(t, second.Keep())

	assert.Equal(t, []string{"files"}, files.StartedAs, "the next run of the conversation uses the same server")
	assert.Empty(t, second.Fresh())
	require.NoError(t, pool.Close())
	assert.Equal(t, 1, files.Closed, "closing the pool stops kept servers")
}

// TestInvariant_PoolNeverSharesServersAcrossConversations guards a
// trust-model guarantee: a server's state, such as an open page, belongs to
// one conversation, so no run of another conversation reaches it.
func TestInvariant_PoolNeverSharesServersAcrossConversations(t *testing.T) {
	files := &gatewaytest.Server{}
	pool := toolgateway.NewPool(map[string]time.Duration{"files": time.Hour}, slog.New(slog.DiscardHandler))
	t.Cleanup(func() { assert.NoError(t, pool.Close()) })
	servers := map[string]toolgateway.ToolServer{"files": files}
	mine := pool.Lease("conv-1", servers)
	use(t, mine)
	require.NoError(t, mine.Keep())

	theirs := pool.Lease("conv-2", servers)
	use(t, theirs)
	require.NoError(t, theirs.Keep())
	fresh := pool.Lease("conv-3", servers)
	use(t, fresh)
	require.NoError(t, fresh.Keep())

	assert.Len(t, files.StartedAs, 3, "each conversation has a server of its own")
	assert.Equal(t, []string{"files"}, theirs.Fresh())
	assert.Equal(t, []string{"files"}, fresh.Fresh())
}

func TestPool_StopsServersIdleForTheirTimeout(t *testing.T) {
	files := &gatewaytest.Server{}
	once := &gatewaytest.Server{}
	pool := toolgateway.NewPool(map[string]time.Duration{"files": 10 * time.Millisecond, "once": 0}, slog.New(slog.DiscardHandler))
	t.Cleanup(func() { assert.NoError(t, pool.Close()) })
	servers := map[string]toolgateway.ToolServer{"files": files, "once": once}
	lease := pool.Lease("conv-1", servers)
	use(t, lease)

	require.NoError(t, lease.Keep())

	assert.Equal(t, 1, once.Closed, "a server without idle time stops when its run ends")
	assert.Eventually(t, func() bool { return files.ClosedCount() == 1 }, time.Second, time.Millisecond)
	next := pool.Lease("conv-1", servers)
	use(t, next)
	require.NoError(t, next.Keep())
	assert.ElementsMatch(t, []string{"files", "once"}, next.Fresh(), "a stopped server starts anew")
}

func TestPool_ReplacesAServerThatDied(t *testing.T) {
	files := &gatewaytest.Server{}
	pool := toolgateway.NewPool(map[string]time.Duration{"files": time.Hour}, slog.New(slog.DiscardHandler))
	t.Cleanup(func() { assert.NoError(t, pool.Close()) })
	servers := map[string]toolgateway.ToolServer{"files": files}
	lease := pool.Lease("conv-1", servers)
	use(t, lease)
	require.NoError(t, lease.Keep())
	// The kept server answers no more; a new one does.
	files.ToolsErr = errors.New("broken pipe")
	files.OnStart = func(context.Context) error {
		files.ToolsErr = nil
		return nil
	}

	next := pool.Lease("conv-1", servers)
	session, err := next.Servers()["files"].Start(context.Background(), "files")

	require.NoError(t, err)
	require.NoError(t, session.Close())
	assert.Len(t, files.StartedAs, 2)
	assert.Equal(t, 1, files.Closed, "the dead server is stopped")
	assert.Equal(t, []string{"files"}, next.Fresh())
}

func TestPool_ClosedLeasesAndPoolsStopTheirServers(t *testing.T) {
	files := &gatewaytest.Server{}
	pool := toolgateway.NewPool(map[string]time.Duration{"files": time.Hour}, slog.New(slog.DiscardHandler))
	servers := map[string]toolgateway.ToolServer{"files": files}
	lease := pool.Lease("conv-1", servers)
	use(t, lease)

	require.NoError(t, lease.Close())
	assert.Equal(t, 1, files.Closed, "a lease that is not kept stops its servers")

	require.NoError(t, pool.Close())
	late := pool.Lease("conv-1", servers)
	use(t, late)
	require.NoError(t, late.Keep())
	assert.Equal(t, 2, files.Closed, "a closed pool keeps nothing")
}

func TestPool_LogsAnIdleServerThatDoesNotStop(t *testing.T) {
	files := &gatewaytest.Server{CloseErr: errors.New("still running")}
	logs := &syncBuffer{}
	pool := toolgateway.NewPool(map[string]time.Duration{"files": time.Millisecond}, slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(func() { assert.NoError(t, pool.Close()) })
	lease := pool.Lease("conv-1", map[string]toolgateway.ToolServer{"files": files})
	use(t, lease)

	require.NoError(t, lease.Keep())

	assert.Eventually(t, func() bool { return strings.Contains(logs.String(), "still running") }, time.Second, time.Millisecond)
	assert.Contains(t, logs.String(), "conv-1")
}

// syncBuffer is a bytes.Buffer safe for a logger writing from a timer.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestPool_ReturnKeepsOnlyWhatWasTaken: a run that does not go ahead, as it
// failed to start, gives the conversation back the servers it took and stops
// those it started: the conversation's next run must start those anew, and
// learns that it did.
func TestPool_ReturnKeepsOnlyWhatWasTaken(t *testing.T) {
	files, mail := &gatewaytest.Server{}, &gatewaytest.Server{}
	pool := toolgateway.NewPool(map[string]time.Duration{"files": time.Hour, "mail": time.Hour}, slog.New(slog.DiscardHandler))
	t.Cleanup(func() { assert.NoError(t, pool.Close()) })
	first := pool.Lease("conv-1", map[string]toolgateway.ToolServer{"files": files})
	use(t, first)
	require.NoError(t, first.Keep())
	lost := pool.Lease("conv-1", map[string]toolgateway.ToolServer{"files": files, "mail": mail})
	use(t, lost)

	require.NoError(t, lost.Return())

	assert.Equal(t, 1, mail.Closed, "the server it started is stopped")
	assert.Zero(t, files.Closed, "the server it took is the conversation's again")
	next := pool.Lease("conv-1", map[string]toolgateway.ToolServer{"files": files})
	use(t, next)
	assert.Empty(t, next.Fresh())
	require.NoError(t, next.Keep())
}

// TestPool_KeepsAServerWhoseProbeWasCancelled: a start cancelled, as when
// another server of the run failed, is no sign the kept server died.
func TestPool_KeepsAServerWhoseProbeWasCancelled(t *testing.T) {
	files := &gatewaytest.Server{}
	pool := toolgateway.NewPool(map[string]time.Duration{"files": time.Hour}, slog.New(slog.DiscardHandler))
	t.Cleanup(func() { assert.NoError(t, pool.Close()) })
	servers := map[string]toolgateway.ToolServer{"files": files}
	first := pool.Lease("conv-1", servers)
	use(t, first)
	require.NoError(t, first.Keep())
	files.ToolsErr = context.Canceled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cancelled := pool.Lease("conv-1", servers)
	_, err := cancelled.Servers()["files"].Start(ctx, "files")
	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, cancelled.Return())

	assert.Zero(t, files.Closed)
	files.ToolsErr = nil
	next := pool.Lease("conv-1", servers)
	use(t, next)
	assert.Empty(t, next.Fresh())
	require.NoError(t, next.Keep())
}

// TestPool_AKeptServerReplacesTheOneBefore: should a conversation's server
// be kept twice, the one kept before is stopped, not leaked.
func TestPool_AKeptServerReplacesTheOneBefore(t *testing.T) {
	files := &gatewaytest.Server{}
	pool := toolgateway.NewPool(map[string]time.Duration{"files": time.Hour}, slog.New(slog.DiscardHandler))
	t.Cleanup(func() { assert.NoError(t, pool.Close()) })
	servers := map[string]toolgateway.ToolServer{"files": files}
	first, second := pool.Lease("conv-1", servers), pool.Lease("conv-1", servers)
	use(t, first)
	use(t, second)

	require.NoError(t, first.Keep())
	require.NoError(t, second.Keep())

	assert.Equal(t, 1, files.Closed)
}

func TestPool_ServersThatFailToStartOrStop(t *testing.T) {
	files := &gatewaytest.Server{}
	pool := toolgateway.NewPool(map[string]time.Duration{"files": time.Hour}, slog.New(slog.DiscardHandler))
	t.Cleanup(func() { assert.NoError(t, pool.Close()) })
	servers := map[string]toolgateway.ToolServer{"files": files}
	lease := pool.Lease("conv-1", servers)
	use(t, lease)
	require.NoError(t, lease.Keep())
	files.ToolsErr, files.CloseErr = errors.New("broken pipe"), errors.New("still running")

	_, err := pool.Lease("conv-1", servers).Servers()["files"].Start(context.Background(), "files")
	require.ErrorContains(t, err, "still running", "a dead server that does not stop is not replaced")

	files.CloseErr, files.StartErr = nil, errors.New("no such command")
	_, err = pool.Lease("conv-2", servers).Servers()["files"].Start(context.Background(), "files")
	require.ErrorContains(t, err, "no such command")
}
