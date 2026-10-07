package toolgateway

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"
)

// Pool keeps the started tool servers of each conversation between its
// runs, so a server's state, such as an open page or a working directory,
// outlasts a run. A server is kept for one conversation only, never shared
// with another, and stopped once it has been idle for its idle timeout.
//
// A run leases its servers from the pool, and hands the lease's servers to
// its gateway, which starts and stops them as ever: starting takes the
// conversation's kept server, if any, and stopping hands it back to the
// lease. Only the gateway lists and calls their tools.
type Pool struct {
	// idleTimeout is how long each server is kept while idle.
	idleTimeout map[string]time.Duration
	// logger reports servers that fail to stop on expiry, when nobody waits.
	logger *slog.Logger

	mu     sync.Mutex
	closed bool
	idle   map[poolKey]*idleSession
}

// poolKey names a kept server: the conversation it belongs to and its name.
type poolKey struct{ conversation, server string }

type idleSession struct {
	session ToolSession
	timer   *time.Timer
}

// NewPool returns a pool that keeps each server idle for its timeout in
// idleTimeout; a server without one is stopped when its run ends. Servers
// that fail to stop once idle for too long are logged to logger.
func NewPool(idleTimeout map[string]time.Duration, logger *slog.Logger) *Pool {
	return &Pool{idleTimeout: maps.Clone(idleTimeout), logger: logger, idle: map[poolKey]*idleSession{}}
}

// Lease returns a lease on the servers of conversation, for one run of it.
// A run that starts a conversation has none yet: conversation is empty.
func (p *Pool) Lease(conversation string, servers map[string]ToolServer) *Lease {
	l := &Lease{pool: p, conversation: conversation, servers: map[string]ToolServer{}, held: map[string]ToolSession{}}
	for name, srv := range servers {
		l.servers[name] = leasedServer{lease: l, server: srv}
	}
	return l
}

// take removes the kept server name of conversation from the pool.
func (p *Pool) take(conversation, name string) ToolSession {
	if conversation == "" {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	key := poolKey{conversation, name}
	kept := p.idle[key]
	if kept == nil {
		return nil
	}
	kept.timer.Stop()
	delete(p.idle, key)
	return kept.session
}

// keep keeps session as the idle server name of conversation, stopping
// whatever was kept under that name before, and stops it once idle for its
// timeout. A closed pool, or a server without idle timeout, stops it at once.
func (p *Pool) keep(conversation, name string, session ToolSession) error {
	timeout := p.idleTimeout[name]
	p.mu.Lock()
	if p.closed || timeout <= 0 {
		p.mu.Unlock()
		return closeSession(name, session)
	}
	key := poolKey{conversation, name}
	old := p.idle[key]
	kept := &idleSession{session: session}
	kept.timer = time.AfterFunc(timeout, func() { p.expire(key, kept) })
	p.idle[key] = kept
	p.mu.Unlock()
	if old != nil {
		old.timer.Stop()
		return closeSession(name, old.session)
	}
	return nil
}

// expire stops kept if it is still the idle server under key.
func (p *Pool) expire(key poolKey, kept *idleSession) {
	p.mu.Lock()
	if p.idle[key] != kept {
		p.mu.Unlock()
		return
	}
	delete(p.idle, key)
	p.mu.Unlock()
	if err := closeSession(key.server, kept.session); err != nil {
		p.logger.Error("cannot stop an idle tool server", "conversation", key.conversation, "error", err)
	}
}

// Close stops every kept server. A run's servers kept after Close are
// stopped at once.
func (p *Pool) Close() error {
	p.mu.Lock()
	p.closed = true
	idle := p.idle
	p.idle = map[poolKey]*idleSession{}
	p.mu.Unlock()
	var errs []error
	for _, key := range slices.SortedFunc(maps.Keys(idle), func(a, b poolKey) int {
		return cmp.Or(strings.Compare(a.conversation, b.conversation), strings.Compare(a.server, b.server))
	}) {
		idle[key].timer.Stop()
		errs = append(errs, closeSession(key.server, idle[key].session))
	}
	return errors.Join(errs...)
}

func closeSession(name string, session ToolSession) error {
	if err := session.Close(); err != nil {
		return fmt.Errorf("toolgateway: server %s: stop: %w", name, err)
	}
	return nil
}

// Lease is a run's hold on the servers of its conversation. Hand Servers to
// the run's gateway; once the gateway has stopped them, Keep hands them back
// to the pool, or Close stops them.
type Lease struct {
	pool         *Pool
	conversation string
	servers      map[string]ToolServer

	mu sync.Mutex
	// held are the servers the gateway has stopped, by name.
	held map[string]ToolSession
	// fresh names the servers started anew rather than taken from the pool.
	fresh []string
}

// Servers returns the servers to hand to the run's gateway.
func (l *Lease) Servers() map[string]ToolServer {
	return l.servers
}

// Fresh names the servers the run started anew, sorted: a conversation that
// used one of them before has lost what it held.
func (l *Lease) Fresh() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Sorted(slices.Values(l.fresh))
}

// Keep hands the servers the gateway stopped back to the pool, as the
// servers of conversation, which names the run's conversation now that it
// has one. Errors stopping servers the pool does not keep are returned.
func (l *Lease) Keep(conversation string) error {
	var errs []error
	for name, session := range l.release() {
		errs = append(errs, l.pool.keep(conversation, name, session))
	}
	return errors.Join(errs...)
}

// Close stops the servers the gateway stopped, for a run that is not kept.
func (l *Lease) Close() error {
	var errs []error
	for name, session := range l.release() {
		errs = append(errs, closeSession(name, session))
	}
	return errors.Join(errs...)
}

// release takes the held servers off the lease.
func (l *Lease) release() map[string]ToolSession {
	l.mu.Lock()
	defer l.mu.Unlock()
	held := l.held
	l.held = map[string]ToolSession{}
	return held
}

// leasedServer starts a server of a lease: it takes the conversation's kept
// server if it still answers, and starts the server anew otherwise.
type leasedServer struct {
	lease  *Lease
	server ToolServer
}

func (s leasedServer) Start(ctx context.Context, name string) (ToolSession, error) {
	l := s.lease
	if kept := l.pool.take(l.conversation, name); kept != nil {
		// A server that died while idle answers no more.
		if _, err := kept.Tools(ctx); err == nil {
			return leasedSession{ToolSession: kept, lease: l, name: name}, nil
		}
		if err := closeSession(name, kept); err != nil {
			return nil, fmt.Errorf("toolgateway: server %s: replacing it: %w", name, err)
		}
	}
	session, err := s.server.Start(ctx, name)
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	l.fresh = append(l.fresh, name)
	l.mu.Unlock()
	return leasedSession{ToolSession: session, lease: l, name: name}, nil
}

// leasedSession is a started server of a lease. Stopping it hands it back
// to the lease.
type leasedSession struct {
	ToolSession
	lease *Lease
	name  string
}

func (s leasedSession) Close() error {
	s.lease.mu.Lock()
	defer s.lease.mu.Unlock()
	s.lease.held[s.name] = s.ToolSession
	return nil
}
