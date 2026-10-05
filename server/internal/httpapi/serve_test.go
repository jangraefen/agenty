//go:build module

package httpapi_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/server/internal/httpapi"
)

// slowServer serves a handler that signals when a request arrives and then
// waits for release before answering.
func slowServer(t *testing.T, timeout time.Duration) (addr string, arrived <-chan struct{}, release chan<- struct{}, cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "listen")
	arrivedCh, releaseCh := make(chan struct{}, 1), make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		arrivedCh <- struct{}{}
		<-releaseCh
		_, _ = io.WriteString(w, "finished")
	})
	ctx, cancelFn := context.WithCancel(t.Context())
	doneCh := make(chan error, 1)
	go func() { doneCh <- httpapi.Serve(ctx, ln, h, timeout) }()
	return ln.Addr().String(), arrivedCh, releaseCh, cancelFn, doneCh
}

func get(addr string) (string, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+addr+"/", http.NoBody)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	return string(body), err
}

func TestServeCompletesInFlightRequestsOnShutdown(t *testing.T) {
	addr, arrived, release, cancel, done := slowServer(t, 5*time.Second)
	type result struct {
		body string
		err  error
	}
	resCh := make(chan result, 1)
	go func() {
		body, err := get(addr)
		resCh <- result{body, err}
	}()
	<-arrived

	cancel()
	select {
	case err := <-done:
		require.Fail(t, "Serve returned while a request was in flight", "Serve = %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)

	res := <-resCh
	assert.NoError(t, res.err, "in-flight request")
	assert.Equal(t, "finished", res.body, "in-flight request body")
	assert.NoError(t, <-done, "Serve")
	_, err := get(addr)
	assert.Error(t, err, "server still accepts requests after shutdown")
}

func TestServeGivesUpAfterShutdownTimeout(t *testing.T) {
	addr, arrived, release, cancel, done := slowServer(t, 100*time.Millisecond)
	defer close(release)
	go func() { _, _ = get(addr) }()
	<-arrived

	start := time.Now()
	cancel()

	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.DeadlineExceeded, "Serve, want shutdown deadline error")
		assert.ErrorContains(t, err, "shutdown", "Serve, want shutdown deadline error")
		elapsed := time.Since(start)
		assert.LessOrEqual(t, elapsed, 3*time.Second, "Serve took %v to give up, want about the timeout", elapsed)
	case <-time.After(5 * time.Second):
		require.Fail(t, "Serve did not return after the shutdown timeout")
	}
}

func TestServeReportsListenerFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "listen")
	_ = ln.Close()

	err = httpapi.Serve(t.Context(), ln, http.NotFoundHandler(), time.Second)

	assert.Error(t, err, "Serve on a closed listener succeeded, want error")
}
