package roles_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

			require.NoError(t, err, "Parse(%q)", tt.in)
			assert.Equal(t, tt.want, got, "Parse(%q)", tt.in)
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

			require.Error(t, err, "Parse(%q) succeeded, want error", tt.in)
			assert.ErrorContains(t, err, tt.wantInErr)
		})
	}
}

func TestAllListsEveryRole(t *testing.T) {
	assert.Equal(t, []roles.Role{roles.API, roles.Worker, roles.Scheduler}, roles.All(), "All()")
}

func TestStringsConvertsRoles(t *testing.T) {
	assert.Equal(t, []string{"api", "scheduler"}, roles.Strings([]roles.Role{roles.API, roles.Scheduler}), "Strings")
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
		assert.NoError(t, err, "Run")
	case <-time.After(5 * time.Second):
		require.Fail(t, "Run did not return after cancel")
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
		require.ErrorIs(t, err, boom, "Run")
		assert.ErrorContains(t, err, "api", "error does not name the failing role")
	case <-time.After(5 * time.Second):
		require.Fail(t, "Run did not return after a component failed")
	}
}

func TestIdleRunsUntilCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() { done <- roles.Idle().Run(ctx) }()
	select {
	case <-done:
		require.Fail(t, "Idle returned before cancel")
	case <-time.After(20 * time.Millisecond):
	}
	cancel()

	assert.NoError(t, <-done, "Idle.Run")
}
