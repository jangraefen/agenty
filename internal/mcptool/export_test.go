package mcptool

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ConnectTransport connects over any transport, so tests can use the
// in-memory one.
func ConnectTransport(ctx context.Context, name string, t mcp.Transport) (*Session, error) {
	return connect(ctx, name, t)
}
