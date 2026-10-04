// Package roles selects and runs the roles of the agenty process: api, worker,
// and scheduler (ARCHITECTURE §5). Roles coordinate only through the database,
// so any combination can run in one process.
package roles

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

// Role is one independently startable part of the agenty process.
type Role string

// The roles of the agenty process, in canonical order.
const (
	API       Role = "api"
	Worker    Role = "worker"
	Scheduler Role = "scheduler"
)

// All returns every role in canonical order.
func All() []Role {
	return []Role{API, Worker, Scheduler}
}

// Parse reads a comma-separated list of roles. It ignores surrounding spaces
// and duplicates and returns the roles in canonical order.
func Parse(s string) ([]Role, error) {
	seen := map[Role]bool{}
	for part := range strings.SplitSeq(s, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		r := Role(name)
		if !slices.Contains(All(), r) {
			return nil, fmt.Errorf("unknown role %q (valid roles: %s)", name, strings.Join(Strings(All()), ", "))
		}
		seen[r] = true
	}
	var out []Role
	for _, r := range All() {
		if seen[r] {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("at least one role is required")
	}
	return out, nil
}

// Strings converts roles to their names.
func Strings(rs []Role) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = string(r)
	}
	return out
}

// Component is the running part of a role. Run blocks until ctx is canceled
// and the component has stopped, or until the component fails.
type Component interface {
	Run(ctx context.Context) error
}

// Run starts every component and waits until all have stopped. When ctx is
// canceled or any component fails, the others are stopped too. The returned
// error names each failing role.
func Run(ctx context.Context, components map[Role]Component) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	for role, c := range components {
		wg.Go(func() {
			if err := c.Run(ctx); err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("role %s: %w", role, err))
				mu.Unlock()
			}
			cancel()
		})
	}
	wg.Wait()
	return errors.Join(errs...)
}

// Idle returns a component that does nothing until it is stopped. Roles
// without behavior yet run as Idle.
func Idle() Component {
	return idle{}
}

type idle struct{}

func (idle) Run(ctx context.Context) error {
	<-ctx.Done()
	return nil
}
