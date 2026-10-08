package toolgateway

import (
	"cmp"
	"context"
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
func (p *Pool) Lease(conversation string, servers map[string]ToolServer) *Lease {
	l := &Lease{pool: p, conversation: conversation, servers: map[string]ToolServer{}, held: map[string]ToolSession{}, taken: map[string]bool{}}
	for name, srv := range servers {
		l.servers[name] = leasedServer{lease: l, server: srv}
	}
	return l
}

// take removes the kept server name of conversation from the pool.
func (p *Pool) take(conversation, name string) ToolSession {
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

// keep keeps session as the idle server name of conversation, and stops it
// once idle for its timeout. It returns what to stop instead: session itself
// on a closed pool or for a server without idle timeout, or what was kept
// under that name before. A conversation has one run at a time, so nothing
// else is kept under that name; should something be, it is stopped, not
// leaked.
func (p *Pool) keep(conversation, name string, session ToolSession) (stop ToolSession) {
	timeout := p.idleTimeout[name]
	key := poolKey{conversation, name}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || timeout <= 0 {
		return session
	}
	old := p.idle[key]
	kept := &idleSession{session: session}
	kept.timer = time.AfterFunc(timeout, func() { p.expire(key, kept) })
	p.idle[key] = kept
	if old == nil {
		return nil
	}
	old.timer.Stop()
	return old.session
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
	// All at once, as a server may take seconds to stop.
	sessions := make([]namedSession, 0, len(idle))
	for _, key := range slices.SortedFunc(maps.Keys(idle), func(a, b poolKey) int {
		return cmp.Or(strings.Compare(a.conversation, b.conversation), strings.Compare(a.server, b.server))
	}) {
		idle[key].timer.Stop()
		sessions = append(sessions, namedSession{name: key.server, session: idle[key].session})
	}
	return closeSessions(sessions)
}

func closeSession(name string, session ToolSession) error {
	if err := session.Close(); err != nil {
		return fmt.Errorf("toolgateway: server %s: stop: %w", name, err)
	}
	return nil
}

// probeTimeout bounds how long a kept server may take to answer before it is
// taken as dead and replaced.
const probeTimeout = 10 * time.Second

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
	// taken names the servers taken from the pool, and fresh those started
	// anew.
	taken map[string]bool
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
// servers of the lease's conversation. Errors stopping servers the pool does
// not keep are returned.
func (l *Lease) Keep() error {
	return l.keepIf(func(string) bool { return true })
}

// Return gives the conversation back the servers the lease took from the
// pool and stops those it started, for a run that does not go ahead: its
// next run starts them anew, and learns that it did.
func (l *Lease) Return() error {
	l.mu.Lock()
	taken := maps.Clone(l.taken)
	l.mu.Unlock()
	return l.keepIf(func(name string) bool { return taken[name] })
}

// Close stops the servers the gateway stopped, for a run whose servers
// cannot be kept.
func (l *Lease) Close() error {
	return l.keepIf(func(string) bool { return false })
}

// keepIf hands the servers the gateway stopped that kept names back to the
// pool, and stops the others, and those the pool does not keep, all at once,
// as a server may take seconds to stop.
func (l *Lease) keepIf(kept func(name string) bool) error {
	l.mu.Lock()
	held := l.held
	l.held = map[string]ToolSession{}
	l.mu.Unlock()
	var stop []namedSession
	for _, name := range slices.Sorted(maps.Keys(held)) {
		session := held[name]
		if kept(name) {
			session = l.pool.keep(l.conversation, name, session)
		}
		if session != nil {
			stop = append(stop, namedSession{name: name, session: session})
		}
	}
	return closeSessions(stop)
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
		// A server that died, or hangs, while idle answers no more.
		probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
		_, err := kept.Tools(probeCtx)
		cancel()
		switch {
		case err == nil:
			l.mu.Lock()
			l.taken[name] = true
			l.mu.Unlock()
			return leasedSession{ToolSession: kept, lease: l, name: name}, nil
		case ctx.Err() != nil:
			// The start was cancelled, which says nothing of the server: the
			// lease holds it, to give it back.
			l.mu.Lock()
			l.taken[name], l.held[name] = true, kept
			l.mu.Unlock()
			return nil, fmt.Errorf("toolgateway: server %s: %w", name, ctx.Err())
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
