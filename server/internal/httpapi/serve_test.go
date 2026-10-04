//go:build module

package httpapi_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jangraefen/agenty/server/internal/httpapi"
)

// slowServer serves a handler that signals when a request arrives and then
// waits for release before answering.
func slowServer(t *testing.T, timeout time.Duration) (addr string, arrived <-chan struct{}, release chan<- struct{}, cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	arrivedCh, releaseCh := make(chan struct{}, 1), make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		arrivedCh <- struct{}{}
		<-releaseCh
		_, _ = io.WriteString(w, "finished")
	})
	ctx, cancelFn := context.WithCancel(context.Background())
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
		t.Fatalf("Serve returned %v while a request was in flight", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)

	res := <-resCh
	if res.err != nil || res.body != "finished" {
		t.Errorf("in-flight request = (%q, %v), want (finished, nil)", res.body, res.err)
	}
	if err := <-done; err != nil {
		t.Errorf("Serve = %v, want nil", err)
	}
	if _, err := get(addr); err == nil {
		t.Error("server still accepts requests after shutdown")
	}
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
		if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "shutdown") {
			t.Errorf("Serve = %v, want shutdown deadline error", err)
		}
		if elapsed := time.Since(start); elapsed > 3*time.Second {
			t.Errorf("Serve took %v to give up, want about the timeout", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after the shutdown timeout")
	}
}

func TestServeReportsListenerFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_ = ln.Close()

	err = httpapi.Serve(context.Background(), ln, http.NotFoundHandler(), time.Second)

	if err == nil {
		t.Error("Serve on a closed listener succeeded, want error")
	}
}
