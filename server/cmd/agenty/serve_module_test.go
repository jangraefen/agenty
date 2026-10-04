//go:build module

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

type started struct {
	addr   string
	cancel context.CancelFunc
	code   <-chan int
	stderr *bytes.Buffer
}

// start runs the server in-process with the given roles; addr is set only
// when the api role listens.
func start(t *testing.T, roleList string) started {
	t.Helper()
	path := writeConfig(t, "server:\n  address: 127.0.0.1:0\n  shutdownTimeout: 5s\n")
	ctx, cancel := context.WithCancel(context.Background())
	listening := make(chan net.Addr, 1)
	code := make(chan int, 1)
	var stdout, stderr bytes.Buffer
	go func() {
		code <- runContext(ctx, []string{"--config", path, "--roles", roleList}, &stdout, &stderr,
			func(a net.Addr) { listening <- a })
	}()
	s := started{cancel: cancel, code: code, stderr: &stderr}
	select {
	case a := <-listening:
		s.addr = a.String()
	case c := <-code:
		t.Fatalf("server exited early with code %d: %s", c, stderr.String())
	case <-time.After(300 * time.Millisecond):
	}
	t.Cleanup(cancel)
	return s
}

func (s started) stop(t *testing.T) {
	t.Helper()
	s.cancel()
	select {
	case c := <-s.code:
		if c != 0 {
			t.Errorf("exit code = %d, want 0 (stderr: %s)", c, s.stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server did not stop")
	}
}

func healthRoles(t *testing.T, addr string) []string {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+addr+"/healthz", http.NoBody)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body struct {
		Roles []string `json:"roles"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /healthz: %v", err)
	}
	return body.Roles
}

func TestServeReportsSelectedRoles(t *testing.T) {
	tests := []struct {
		roles string
		want  []string
	}{
		{"api", []string{"api"}},
		{"api,worker", []string{"api", "worker"}},
		{"scheduler,api", []string{"api", "scheduler"}},
		{"api,worker,scheduler", []string{"api", "worker", "scheduler"}},
	}
	for _, tt := range tests {
		t.Run(tt.roles, func(t *testing.T) {
			s := start(t, tt.roles)
			if s.addr == "" {
				t.Fatal("api role did not listen")
			}

			got := healthRoles(t, s.addr)

			if !slices.Equal(got, tt.want) {
				t.Errorf("roles = %v, want %v", got, tt.want)
			}
			s.stop(t)
		})
	}
}

func TestServeWithoutAPIRoleDoesNotListen(t *testing.T) {
	for _, roleList := range []string{"worker", "scheduler", "worker,scheduler"} {
		t.Run(roleList, func(t *testing.T) {
			s := start(t, roleList)

			if s.addr != "" {
				t.Errorf("listening on %s without the api role", s.addr)
			}
			s.stop(t)
		})
	}
}

func TestBinaryShutsDownOnSIGTERM(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "agenty")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pick port: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	path := writeConfig(t, "server:\n  address: "+addr+"\n")
	cmd := exec.Command(bin, "--config", path)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	logs := bufio.NewScanner(stderr)
	for logs.Scan() && !strings.Contains(logs.Text(), "listening") {
	}

	if got := healthRoles(t, addr); !slices.Equal(got, []string{"api", "worker", "scheduler"}) {
		t.Errorf("default roles = %v, want all", got)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal: %v", err)
	}
	for logs.Scan() {
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("exit after SIGTERM: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("binary did not exit after SIGTERM")
	}
}
