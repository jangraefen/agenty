package toolgateway

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
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

// startServers starts, in name order, every server that serves a granted
// tool, and returns their sessions and tools. If anything fails, the servers
// already started are stopped.
func startServers(ctx context.Context, servers map[string]ToolServer, granted map[string]bool) (sessions []namedSession, tools []Tool, err error) {
	needed := map[string]bool{}
	for tool := range granted {
		if server, _, found := strings.Cut(tool, "_"); found && servers[server] != nil {
			needed[server] = true
		}
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, closeSessions(sessions))
			sessions, tools = nil, nil
		}
	}()
	for _, name := range slices.Sorted(maps.Keys(needed)) {
		session, err := servers[name].Start(ctx, name)
		if err != nil {
			return sessions, nil, fmt.Errorf("toolgateway: server %s: start: %w", name, err)
		}
		sessions = append(sessions, namedSession{name: name, session: session})
		serverTools, err := session.Tools(ctx)
		if err != nil {
			return sessions, nil, fmt.Errorf("toolgateway: server %s: tools: %w", name, err)
		}
		for _, tool := range serverTools {
			if tool == nil || !strings.HasPrefix(tool.Definition().Name, name+"_") {
				return sessions, nil, fmt.Errorf("toolgateway: server %s: tool %q is not named %s_<tool>", name, toolNameOf(tool), name)
			}
		}
		tools = append(tools, serverTools...)
	}
	return sessions, tools, nil
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

// closeSessions stops every session, in reverse start order, and reports
// every failure.
func closeSessions(sessions []namedSession) error {
	var errs []error
	for _, s := range slices.Backward(sessions) {
		if err := s.session.Close(); err != nil {
			errs = append(errs, fmt.Errorf("toolgateway: server %s: stop: %w", s.name, err))
		}
	}
	return errors.Join(errs...)
}
