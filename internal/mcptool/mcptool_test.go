package mcptool_test

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strings"
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
	return connectServer(t, srv), srv, log
}

// connectServer serves srv over in-memory transports and connects a session
// named "test" to it.
func connectServer(t *testing.T, srv *mcp.Server) *mcptool.Session {
	t.Helper()
	clientT, serverT := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(t.Context(), serverT, nil)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, ss.Close()) })
	s, err := mcptool.ConnectTransport(t.Context(), "test", clientT)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, s.Close()) })
	return s
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
	gw, err := toolgateway.New(context.Background(), toolgateway.Config{
		Granted:      []string{tool.Definition().Name},
		Tools:        []toolgateway.Tool{tool},
		MaxToolCalls: 100,
		Policy:       &gatewaytest.Policy{},
		Audit:        &gatewaytest.Audit{},
	})
	require.NoError(t, err)
	return gw.Start().Call(ctx, toolgateway.ToolCall{Name: tool.Definition().Name, Args: args})
}

// jsonString encodes s as JSON. It runs while test tables are built, before
// there is a *testing.T to fail, so it panics instead.
func jsonString(s string) json.RawMessage {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return b
}

func TestTools_PrefixesNamesAndKeepsDefinitions(t *testing.T) {
	s, _, _ := connectInMemory(t)

	tools := toolsByName(t, s)

	assert.ElementsMatch(t, []string{"test_echo", "test_count", "test_fail", "test_fail-silently", "test_mixed", "test_env", "test_wait"}, slices.Collect(maps.Keys(tools)))
	def := tools["test_echo"].Definition()
	assert.Equal(t, "Echoes its arguments.", def.Description)
	assert.JSONEq(t, `{"type":"object"}`, string(def.InputSchema))
	_, err := toolgateway.New(context.Background(), toolgateway.Config{Tools: slices.Collect(maps.Values(tools)), MaxToolCalls: 1, Policy: &gatewaytest.Policy{}, Audit: &gatewaytest.Audit{}})
	require.NoError(t, err, "the gateway accepts every MCP tool")
}

func TestCall_Results(t *testing.T) {
	tests := []struct {
		name string
		tool string
		args json.RawMessage
		want json.RawMessage
	}{
		{"text content becomes a JSON string", "test_echo", json.RawMessage(`{"id":7}`), jsonString(`{"id":7}`)},
		{"structured content is returned as is", "test_count", nil, json.RawMessage(`{"count":2}`)},
		{"unsupported content becomes placeholders", "test_mixed", nil, jsonString("see attachments\n[image omitted]\n[audio omitted]\n[resource link omitted]\n[resource omitted]")},
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
		{"tool error carries the server's message", "test_fail", nil, nil, "ticket system unavailable"},
		{"tool error without a message", "test_fail-silently", nil, nil, "tool reported an error"},
		{"invalid arguments", "test_echo", json.RawMessage(`{not json`), nil, "arguments"},
		{"tool gone from the server", "test_echo", nil, func(srv *mcp.Server) { srv.RemoveTools("echo") }, "echo"},
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
	tool := toolsByName(t, s)["test_wait"]
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	_, err := callTool(ctx, t, tool, nil)

	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestStart_RejectsInvalidServers(t *testing.T) {
	tests := []struct {
		name    string
		as      string
		server  mcptool.Server
		wantErr string
	}{
		{"no name", "", mcptool.Server{Command: "x"}, "name"},
		{"name with a dot", "tickets.v2", mcptool.Server{Command: "x"}, `"tickets.v2"`},
		{"uppercase name", "Tickets", mcptool.Server{Command: "x"}, `"Tickets"`},
		{"no command", "tickets", mcptool.Server{}, "command"},
		{"command that does not exist", "tickets", mcptool.Server{Command: "/no/such/mcp-server"}, "tickets"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := tt.server.Start(t.Context(), tt.as)
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

func TestTools_RejectsNamesModelsCannotUse(t *testing.T) {
	tests := []struct {
		name    string
		tool    string
		wantErr string
	}{
		{"dot in the tool name", "admin.list", `"test_admin.list"`},
		{"too long with the server prefix", strings.Repeat("a", 60), "must be 1 to 64"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
			srv.AddTool(&mcp.Tool{Name: tt.tool, InputSchema: objectSchema}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{}, nil
			})
			s := connectServer(t, srv)

			tools, err := s.Tools(t.Context())

			require.ErrorContains(t, err, tt.wantErr)
			assert.Nil(t, tools)
		})
	}
}
