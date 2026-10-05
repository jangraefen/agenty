package mcptool_test

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/mcptool"
	"github.com/jangraefen/agenty/internal/toolgateway"
	"github.com/jangraefen/agenty/internal/toolgateway/gatewaytest"
)

// connectInMemory serves the test server over in-memory transports and
// connects a session named "test" to it.
func connectInMemory(t *testing.T) (*mcptool.Session, *mcp.Server, *callLog) {
	t.Helper()
	log := &callLog{}
	srv := newTestServer(log)
	clientT, serverT := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(t.Context(), serverT, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ss.Close() })
	s, err := mcptool.ConnectTransport(t.Context(), "test", clientT)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s, srv, log
}

func toolsByName(t *testing.T, s *mcptool.Session) map[string]toolgateway.Tool {
	t.Helper()
	tools, err := s.Tools(t.Context())
	require.NoError(t, err)
	byName := map[string]toolgateway.Tool{}
	for _, tool := range tools {
		byName[tool.Definition().Name] = tool
	}
	return byName
}

// callTool calls tool through a gateway that grants and allows it: tools run
// only through the gateway, in tests too.
func callTool(ctx context.Context, t *testing.T, tool toolgateway.Tool, args json.RawMessage) (json.RawMessage, error) {
	t.Helper()
	gw, err := toolgateway.New(toolgateway.Config{
		RunID:        "run-1",
		Granted:      []string{tool.Definition().Name},
		Tools:        []toolgateway.Tool{tool},
		MaxToolCalls: 100,
		Policy:       &gatewaytest.Policy{},
		Audit:        &gatewaytest.Audit{},
	})
	require.NoError(t, err)
	return gw.Call(ctx, toolgateway.ToolCall{Name: tool.Definition().Name, Args: args})
}

func jsonString(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

func TestTools_PrefixesNamesAndKeepsDefinitions(t *testing.T) {
	s, _, _ := connectInMemory(t)

	tools := toolsByName(t, s)

	assert.ElementsMatch(t, []string{"test.echo", "test.count", "test.fail", "test.fail-silently", "test.mixed", "test.env", "test.wait"}, slices.Collect(maps.Keys(tools)))
	def := tools["test.echo"].Definition()
	assert.Equal(t, "Echoes its arguments.", def.Description)
	assert.JSONEq(t, `{"type":"object"}`, string(def.InputSchema))
	require.NoError(t, toolgateway.ValidateTools(slices.Collect(maps.Values(tools))), "the gateway accepts every MCP tool")
}

func TestCall_Results(t *testing.T) {
	tests := []struct {
		name string
		tool string
		args json.RawMessage
		want json.RawMessage
	}{
		{"text content becomes a JSON string", "test.echo", json.RawMessage(`{"id":7}`), jsonString(`{"id":7}`)},
		{"structured content is returned as is", "test.count", nil, json.RawMessage(`{"count":2}`)},
		{"unsupported content becomes placeholders", "test.mixed", nil, jsonString("see attachments\n[image omitted]\n[audio omitted]\n[resource link omitted]\n[resource omitted]")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _, _ := connectInMemory(t)
			tool := toolsByName(t, s)[tt.tool]

			got, err := callTool(t.Context(), t, tool, tt.args)

			require.NoError(t, err)
			assert.JSONEq(t, string(tt.want), string(got))
		})
	}
}

func TestCall_Errors(t *testing.T) {
	tests := []struct {
		name    string
		tool    string
		args    json.RawMessage
		before  func(*mcp.Server)
		wantErr string
	}{
		{"tool error carries the server's message", "test.fail", nil, nil, "ticket system unavailable"},
		{"tool error without a message", "test.fail-silently", nil, nil, "tool reported an error"},
		{"invalid arguments", "test.echo", json.RawMessage(`{not json`), nil, "arguments"},
		{"tool gone from the server", "test.echo", nil, func(srv *mcp.Server) { srv.RemoveTools("echo") }, "echo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, srv, _ := connectInMemory(t)
			tool := toolsByName(t, s)[tt.tool]
			if tt.before != nil {
				tt.before(srv)
			}

			got, err := callTool(t.Context(), t, tool, tt.args)

			require.ErrorContains(t, err, tt.wantErr)
			assert.Nil(t, got)
		})
	}
}

func TestCall_CancelledContextStopsTheCall(t *testing.T) {
	s, _, _ := connectInMemory(t)
	tool := toolsByName(t, s)["test.wait"]
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	_, err := callTool(ctx, t, tool, nil)

	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestConnect_RejectsInvalidServers(t *testing.T) {
	tests := []struct {
		name    string
		server  mcptool.Server
		wantErr string
	}{
		{"no name", mcptool.Server{Command: "x"}, "name"},
		{"name with a dot", mcptool.Server{Name: "tickets.v2", Command: "x"}, `"tickets.v2"`},
		{"uppercase name", mcptool.Server{Name: "Tickets", Command: "x"}, `"Tickets"`},
		{"no command", mcptool.Server{Name: "tickets"}, "command"},
		{"command that does not exist", mcptool.Server{Name: "tickets", Command: "/no/such/mcp-server"}, "tickets"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := mcptool.Connect(t.Context(), tt.server)
			require.ErrorContains(t, err, tt.wantErr)
			assert.Nil(t, s)
		})
	}
}

func TestTools_ClosedSessionIsAnError(t *testing.T) {
	s, _, _ := connectInMemory(t)
	require.NoError(t, s.Close())

	tools, err := s.Tools(t.Context())

	require.ErrorContains(t, err, "test: list tools")
	assert.Nil(t, tools)
}

func TestConnectTransport_RejectsInvalidNames(t *testing.T) {
	clientT, _ := mcp.NewInMemoryTransports()

	s, err := mcptool.ConnectTransport(t.Context(), "Not Valid", clientT)

	require.ErrorContains(t, err, `"Not Valid"`)
	assert.Nil(t, s)
}
