package mcptool_test

import (
	"context"
	"encoding/json"
	"os"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// callLog records the tools the test server was asked to run.
type callLog struct {
	mu    sync.Mutex
	names []string
}

func (l *callLog) add(name string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.names = append(l.names, name)
}

func (l *callLog) calls() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.names...)
}

var objectSchema = json.RawMessage(`{"type":"object"}`)

func text(s string) []mcp.Content { return []mcp.Content{&mcp.TextContent{Text: s}} }

// newTestServer serves a few tools that cover the result shapes the adapter
// handles.
func newTestServer(log *callLog) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	add := func(name, description string, h func(context.Context, json.RawMessage) *mcp.CallToolResult) {
		s.AddTool(&mcp.Tool{Name: name, Description: description, InputSchema: objectSchema},
			func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				log.add(name)
				return h(ctx, req.Params.Arguments), nil
			})
	}
	add("echo", "Echoes its arguments.", func(_ context.Context, args json.RawMessage) *mcp.CallToolResult {
		return &mcp.CallToolResult{Content: text(string(args))}
	})
	add("count", "Returns structured content.", func(context.Context, json.RawMessage) *mcp.CallToolResult {
		return &mcp.CallToolResult{Content: text(`{"count":2}`), StructuredContent: map[string]any{"count": 2}}
	})
	add("fail", "Always fails.", func(context.Context, json.RawMessage) *mcp.CallToolResult {
		return &mcp.CallToolResult{IsError: true, Content: text("ticket system unavailable")}
	})
	add("fail-silently", "Fails without a message.", func(context.Context, json.RawMessage) *mcp.CallToolResult {
		return &mcp.CallToolResult{IsError: true}
	})
	add("mixed", "Returns text and other content.", func(context.Context, json.RawMessage) *mcp.CallToolResult {
		return &mcp.CallToolResult{Content: []mcp.Content{
			&mcp.TextContent{Text: "see attachments"},
			&mcp.ImageContent{Data: []byte("aW1n"), MIMEType: "image/png"},
			&mcp.AudioContent{Data: []byte("YXVk"), MIMEType: "audio/wav"},
			&mcp.ResourceLink{URI: "file:///report.pdf", Name: "report"},
			&mcp.EmbeddedResource{Resource: &mcp.ResourceContents{URI: "file:///notes.txt", Text: "notes"}},
		}}
	})
	add("env", "Returns an environment variable of the server process.", func(_ context.Context, args json.RawMessage) *mcp.CallToolResult {
		var in struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return &mcp.CallToolResult{IsError: true, Content: text(err.Error())}
		}
		return &mcp.CallToolResult{Content: text(os.Getenv(in.Name))}
	})
	add("wait", "Blocks until the call is cancelled.", func(ctx context.Context, _ json.RawMessage) *mcp.CallToolResult {
		<-ctx.Done()
		return &mcp.CallToolResult{Content: text("cancelled")}
	})
	return s
}
