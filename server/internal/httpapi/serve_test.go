//go:build module

package httpapi_test

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
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
	ctx, cancelFn := context.WithCancel(context.Background())
	doneCh := make(chan error, 1)
	go func() { doneCh <- httpapi.Serve(ctx, ln, h, slog.New(slog.DiscardHandler), timeout) }()
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

	err = httpapi.Serve(context.Background(), ln, http.NotFoundHandler(), nil, time.Second)

	assert.Error(t, err, "Serve on a closed listener succeeded, want error")
}

func TestServeClosesIdleKeepAliveConnections(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "listen")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- httpapi.ServeWithIdleTimeout(ctx, ln, http.NotFoundHandler(), slog.New(slog.DiscardHandler), time.Second, 100*time.Millisecond)
	}()
	defer func() {
		cancel()
		assert.NoError(t, <-done, "Serve")
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err, "dial")
	defer func() { _ = conn.Close() }()
	_, err = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: test\r\n\r\n")
	require.NoError(t, err, "write request")
	r := bufio.NewReader(conn)
	resp, err := http.ReadResponse(r, nil)
	require.NoError(t, err, "read response")
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	require.False(t, resp.Close, "server closed the connection after the response, want keep-alive")

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)), "set deadline")
	start := time.Now()
	_, err = r.ReadByte()
	assert.ErrorIs(t, err, io.EOF, "idle connection, want closed by the server")
	assert.Less(t, time.Since(start), 3*time.Second, "idle connection stayed open")
}

func TestServeLogsServerErrorsAsWarnings(t *testing.T) {
	logger, records := jsonLogger(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "listen")
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.WriteHeader(http.StatusTeapot) // superfluous: net/http reports it through ErrorLog
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- httpapi.Serve(ctx, ln, h, logger, time.Second) }()

	_, err = get(ln.Addr().String())
	require.NoError(t, err, "request")
	cancel()
	require.NoError(t, <-done, "Serve")

	var found bool
	for _, r := range records() {
		if msg, _ := r["msg"].(string); strings.Contains(msg, "superfluous") {
			found = true
			assert.Equal(t, "warn", r["level"], "level")
		}
	}
	assert.True(t, found, "net/http error not logged through the logger")
}
