package mcptool

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// This file is compiled only for tests. Being in package mcptool, it can export
// the unexported connect to the external test package mcptool_test, so tests
// can talk to an in-memory MCP server without starting a process, while
// connect stays out of the public API and MCP SDK types stay out of it.

// ConnectTransport connects over any transport, so tests can use the
// in-memory one.
func ConnectTransport(ctx context.Context, name string, t mcp.Transport) (*Session, error) {
	return connect(ctx, name, t)
}
