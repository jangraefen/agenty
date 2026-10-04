package roles_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jangraefen/agenty/server/internal/roles"
)

func TestParseAcceptsAnyCombination(t *testing.T) {
	tests := []struct {
		in   string
		want []roles.Role
	}{
		{"api", []roles.Role{roles.API}},
		{"worker", []roles.Role{roles.Worker}},
		{"scheduler", []roles.Role{roles.Scheduler}},
		{"api,worker", []roles.Role{roles.API, roles.Worker}},
		{"scheduler,api", []roles.Role{roles.API, roles.Scheduler}},
		{"worker, scheduler", []roles.Role{roles.Worker, roles.Scheduler}},
		{"scheduler,worker,api", []roles.Role{roles.API, roles.Worker, roles.Scheduler}},
		{"api,api", []roles.Role{roles.API}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := roles.Parse(tt.in)

			if err != nil {
				t.Fatalf("Parse(%q): %v", tt.in, err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Parse(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		in, wantInErr string
	}{
		{"", "at least one role"},
		{" , ", "at least one role"},
		{"api,cron", `"cron"`},
		{"API", `"API"`},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			_, err := roles.Parse(tt.in)

			if err == nil {
				t.Fatalf("Parse(%q) succeeded, want error", tt.in)
			}
			if !strings.Contains(err.Error(), tt.wantInErr) {
				t.Errorf("error %q does not mention %q", err, tt.wantInErr)
			}
		})
	}
}

func TestAllListsEveryRole(t *testing.T) {
	want := []roles.Role{roles.API, roles.Worker, roles.Scheduler}
	if got := roles.All(); !slices.Equal(got, want) {
		t.Errorf("All() = %v, want %v", got, want)
	}
}

func TestStringsConvertsRoles(t *testing.T) {
	got := roles.Strings([]roles.Role{roles.API, roles.Scheduler})
	if want := []string{"api", "scheduler"}; !slices.Equal(got, want) {
		t.Errorf("Strings = %v, want %v", got, want)
	}
}

type fakeComponent struct {
	started chan struct{}
	err     error
}

func newFake(err error) *fakeComponent {
	return &fakeComponent{started: make(chan struct{}), err: err}
}

func (f *fakeComponent) Run(ctx context.Context) error {
	close(f.started)
	if f.err != nil {
		return f.err
	}
	<-ctx.Done()
	return nil
}

func TestRunStartsEveryComponentAndStopsOnCancel(t *testing.T) {
	a, b := newFake(nil), newFake(nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() { done <- roles.Run(ctx, map[roles.Role]roles.Component{roles.API: a, roles.Worker: b}) }()
	<-a.started
	<-b.started
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestRunStopsAllComponentsWhenOneFails(t *testing.T) {
	boom := errors.New("boom")
	failing, idle := newFake(boom), newFake(nil)
	done := make(chan error, 1)

	go func() {
		done <- roles.Run(context.Background(), map[roles.Role]roles.Component{roles.API: failing, roles.Worker: idle})
	}()

	select {
	case err := <-done:
		if !errors.Is(err, boom) {
			t.Errorf("Run = %v, want %v", err, boom)
		}
		if !strings.Contains(err.Error(), "api") {
			t.Errorf("error %q does not name the failing role", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after a component failed")
	}
}

func TestIdleRunsUntilCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() { done <- roles.Idle().Run(ctx) }()
	select {
	case <-done:
		t.Fatal("Idle returned before cancel")
	case <-time.After(20 * time.Millisecond):
	}
	cancel()

	if err := <-done; err != nil {
		t.Errorf("Idle.Run = %v, want nil", err)
	}
}
