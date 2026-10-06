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
// no other code holds a connection to one.
type ToolServer interface {
	// Start starts the server under name. Its tools must be named
	// "<name>_<tool>".
	Start(ctx context.Context, name string) (ToolSession, error)
}

// ToolSession is a started ToolServer.
type ToolSession interface {
	Tools(ctx context.Context) ([]Tool, error)
	// Close stops the server.
	Close() error
}

// serverName has no underscore, so the first "_" of a tool name always ends
// the server name.
var serverName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// validateServers checks server names, and that no in-process tool uses a
// server's name, so a tool's name always says where it runs.
func validateServers(servers map[string]ToolServer, tools []Tool) error {
	for _, name := range slices.Sorted(maps.Keys(servers)) {
		if !serverName.MatchString(name) {
			return fmt.Errorf("toolgateway: server name %q must be lowercase letters, digits and single hyphens", name)
		}
		if servers[name] == nil {
			return fmt.Errorf("toolgateway: server %s is nil", name)
		}
	}
	for _, tool := range tools {
		tool := tool.Definition().Name
		if server, _, found := strings.Cut(tool, "_"); found && servers[server] != nil {
			return fmt.Errorf("toolgateway: tool %q is named like a tool of server %s", tool, server)
		}
	}
	return nil
}

// startServers starts every server that serves a granted tool, all at once,
// and returns their sessions and tools in server name order. The first
// failure cancels the starts still running; if anything fails, every server
// that did start is stopped, and every failure is reported except
// cancellations caused by an earlier one.
func startServers(ctx context.Context, servers map[string]ToolServer, granted map[string]bool) ([]namedSession, []Tool, error) {
	needed := map[string]bool{}
	for tool := range granted {
		if server, _, found := strings.Cut(tool, "_"); found && servers[server] != nil {
			needed[server] = true
		}
	}
	names := slices.Sorted(maps.Keys(needed))

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
	if len(errs) == 0 && first != nil {
		errs = append(errs, first)
	}
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

func startServer(ctx context.Context, name string, server ToolServer) started {
	session, err := server.Start(ctx, name)
	if err != nil {
		return started{err: fmt.Errorf("toolgateway: server %s: start: %w", name, err)}
	}
	tools, err := session.Tools(ctx)
	if err != nil {
		return started{session: session, err: fmt.Errorf("toolgateway: server %s: tools: %w", name, err)}
	}
	for _, tool := range tools {
		if tool == nil || !strings.HasPrefix(tool.Definition().Name, name+"_") {
			return started{session: session, err: fmt.Errorf("toolgateway: server %s: tool %q is not named %s_<tool>", name, toolNameOf(tool), name)}
		}
	}
	return started{session: session, tools: tools}
}

func toolNameOf(tool Tool) string {
	if tool == nil {
		return "<nil>"
	}
	return tool.Definition().Name
}

// namedSession is a started server and the name it was started under.
type namedSession struct {
	name    string
	session ToolSession
}

// closeSessions stops every session, all at once, and reports every failure,
// in start order.
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
