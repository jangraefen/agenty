package toolgateway

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"sync"
)

// ToolServer is a source of tools the gateway starts and stops itself, such
// as an MCP server. Only the gateway starts servers and lists their tools, so
// no other code holds a connection to one: the forbidigo rules reject
// ToolServer.Start and ToolSession.Tools outside this package (guarantee 2).
//
// The server's configuration says how to reach it; the name it is started
// under comes from the gateway, which validated it, so a server cannot pick
// a prefix that impersonates another's tools. mcptool.Server is the
// production implementation; a leased server from a Pool wraps one.
type ToolServer interface {
	// Start starts the server under name. Its tools must be named
	// "<name>_<tool>".
	Start(ctx context.Context, name string) (ToolSession, error)
}

// ToolSession is a started ToolServer. The gateway lists its tools once, when
// the run starts, and closes it when the run ends.
type ToolSession interface {
	Tools(ctx context.Context) ([]Tool, error)
	// Close stops the server.
	Close() error
}

// serverName has no underscore, so the first "_" of a tool name always ends
// the server name.
var serverName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// validateServers checks that every server is set and has a valid name. It
// runs before anything starts. Names are checked in sorted order so the same
// configuration always reports the same error.
func validateServers(servers map[string]ToolServer) error {
	for _, name := range slices.Sorted(maps.Keys(servers)) {
		if !serverName.MatchString(name) {
			return fmt.Errorf("toolgateway: server name %q must be lowercase letters, digits and single hyphens", name)
		}
		if servers[name] == nil {
			return fmt.Errorf("toolgateway: server %s is nil", name)
		}
	}
	return nil
}

// startServers starts every server that serves a granted tool, all at once,
// and returns their sessions and tools in server name order. The first
// failure cancels the starts still running; if anything fails, every server
// that did start is stopped, and every failure is reported except
// cancellations caused by an earlier one.
//
// Starting in parallel matters because a server, such as one started through
// npx, may take seconds; a run with several would otherwise wait for their sum.
func startServers(ctx context.Context, servers map[string]ToolServer, granted map[string]bool) ([]namedSession, []Tool, error) {
	// A server is needed if at least one of its tools is granted.
	needed := map[string]bool{}
	for tool := range granted {
		server, _, _ := strings.Cut(tool, "_")
		needed[server] = true
	}
	names := slices.Sorted(maps.Keys(needed))

	// One failure dooms the run, so it cancels the other starts instead of
	// letting them finish only to be stopped. Each goroutine writes only its
	// own slot of results, so no lock is needed.
	startCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make([]started, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Go(func() {
			results[i] = startServer(startCtx, name, servers[name])
			if results[i].err != nil {
				cancel()
			}
		})
	}
	wg.Wait()

	// Collect in name order: sessions to stop or keep, tools to serve, and
	// the failures worth reporting.
	var (
		sessions []namedSession
		tools    []Tool
		errs     []error
		first    error
	)
	for i, r := range results {
		if r.session != nil {
			sessions = append(sessions, namedSession{name: names[i], session: r.session})
		}
		switch {
		case r.err == nil:
			tools = append(tools, r.tools...)
		case ctx.Err() == nil && errors.Is(r.err, context.Canceled):
			// Cancelled because another server failed, which is reported;
			// unless it is the only failure, see below.
			first = cmp.Or(first, r.err)
		default:
			errs = append(errs, r.err)
		}
	}
	// If the only failures are cancellations not caused by the caller, report
	// the first of them rather than nothing, so a failed start is never silent.
	if len(errs) == 0 && first != nil {
		errs = append(errs, first)
	}
	// All or nothing: a run never goes ahead with some of its servers, and
	// never leaves a started one behind.
	if err := errors.Join(errs...); err != nil {
		return nil, nil, errors.Join(err, closeSessions(sessions))
	}
	return sessions, tools, nil
}

// started is the outcome of starting one server. A session that started is
// set even if listing its tools failed, so it can be stopped.
type started struct {
	session ToolSession
	tools   []Tool
	err     error
}

// startServer starts one server under name, lists its tools and checks them.
// A server is untrusted configuration as far as its tool list goes: every
// tool must carry the server's own prefix, a valid name and no duplicate,
// or the start fails. The prefix check is what makes a grant on
// "<server>_<tool>" mean that server's tool and no other's.
func startServer(ctx context.Context, name string, server ToolServer) started {
	session, err := server.Start(ctx, name)
	if err != nil {
		return started{err: fmt.Errorf("toolgateway: server %s: start: %w", name, err)}
	}
	tools, err := session.Tools(ctx)
	if err != nil {
		return started{session: session, err: fmt.Errorf("toolgateway: server %s: tools: %w", name, err)}
	}
	seen := make(map[string]bool, len(tools))
	for _, tool := range tools {
		if tool == nil || !strings.HasPrefix(tool.Definition().Name, name+"_") {
			return started{session: session, err: fmt.Errorf("toolgateway: server %s: tool %q is not named %s_<tool>", name, toolNameOf(tool), name)}
		}
		def := tool.Definition()
		if err := ValidateToolName(def.Name); err != nil {
			return started{session: session, err: fmt.Errorf("toolgateway: server %s: %w", name, err)}
		}
		if seen[def.Name] {
			return started{session: session, err: fmt.Errorf("toolgateway: server %s: duplicate tool %q", name, def.Name)}
		}
		seen[def.Name] = true
	}
	return started{session: session, tools: tools}
}

// toolNameOf names tool in an error message, nil included, as a server may
// list a nil tool.
func toolNameOf(tool Tool) string {
	if tool == nil {
		return "<nil>"
	}
	return tool.Definition().Name
}

// namedSession is a started server and the name it was started under, kept
// together so a failure to stop it names the server.
type namedSession struct {
	name    string
	session ToolSession
}

// closeSessions stops every session, all at once, and reports every failure,
// in start order. One failure does not stop the others: each session holds a
// process that must not be leaked.
func closeSessions(sessions []namedSession) error {
	errs := make([]error, len(sessions))
	var wg sync.WaitGroup
	for i, s := range sessions {
		wg.Go(func() {
			if err := s.session.Close(); err != nil {
				errs[i] = fmt.Errorf("toolgateway: server %s: stop: %w", s.name, err)
			}
		})
	}
	wg.Wait()
	return errors.Join(errs...)
}
