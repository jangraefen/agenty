// Package mcptool reaches tools on MCP servers. It turns each tool of a server
// into a toolgateway.Tool named "<server>_<tool>", so the tool gateway can
// grant, police, execute and audit it like any other tool. This is the only
// package that talks to MCP servers.
//
// # Role in the architecture
//
// The API server (internal/server) builds a Server from each MCP server in
// the operator configuration, with the environment config resolved for it,
// and hands it, through a toolgateway.Pool lease, to the run's gateway. From
// then on only the gateway touches it: it calls Server.Start, lists the
// Session's tools, and calls them. mcptool decides nothing; grants, policy,
// audit and redaction all happen in internal/toolgateway around it. It
// depends on the official MCP Go SDK and on the toolgateway interfaces it
// implements.
//
// # What it contains and how it fits together
//
//   - Server implements toolgateway.ToolServer: Start launches the server
//     process with a restricted environment and connects to it over stdio.
//   - Session implements toolgateway.ToolSession: Tools lists the server's
//     tools once and wraps each, Close ends the connection and the process.
//   - tool implements toolgateway.Tool: Call forwards the arguments to the
//     server and turns its reply into the JSON result the gateway expects.
//
// # Trust-model guarantees
//
// Guarantee 2, every side effect goes through the gateway, is upheld from
// both sides: depguard in .golangci.yml lets no other package import the MCP
// SDK, so no code can open a session around the gateway, and forbidigo lets
// only the gateway call Server.Start and Session.Tools. Guarantee 5,
// credentials never reach the model, is helped by the process environment:
// a server inherits only PATH, so Agenty's own credentials reach it only if
// the operator configures them for it, and anything the server echoes back
// is redacted by the gateway.
package mcptool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jangraefen/agenty/internal/toolgateway"
)

// clientVersion is the version Agenty reports to MCP servers, which the
// protocol requires during the handshake. It comes from the build: the module
// version of a released build, "(devel)" for a local one.
func clientVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "(devel)"
}

var _ toolgateway.ToolServer = Server{}

// Server is an MCP server Agenty starts as a subprocess and talks to over
// stdin and stdout. The tool gateway starts it, under the name that prefixes
// its tools, as in "<name>_<tool>".
type Server struct {
	Command string
	Args    []string
	// Env is the environment of the server process. Only PATH is inherited
	// from Agenty's own environment, so Agenty's credentials never reach a
	// server unless configured here. A PATH set here replaces the inherited
	// one.
	Env map[string]string
}

// Session is a connection to one MCP server, under the name the gateway
// started it as. Closing the connection also ends the server process, which
// the SDK's command transport owns.
type Session struct {
	name    string
	session *mcp.ClientSession
}

// Start starts the server as name and connects to it. Only the tool gateway
// starts servers, and it checks the name first.
//
// It returns the session as a toolgateway.ToolSession, not a nil *Session on
// failure, so the gateway's nil checks see a true nil.
func (s Server) Start(ctx context.Context, name string) (toolgateway.ToolSession, error) {
	if s.Command == "" {
		return nil, fmt.Errorf("mcptool: server %s: command is required", name)
	}
	session, err := connect(ctx, name, &mcp.CommandTransport{Command: command(s)})
	if err != nil {
		return nil, err
	}
	return session, nil
}

// command builds the server process. It inherits only PATH, because servers
// are often scripts that start an interpreter by name, such as npx starting
// node, and that lookup happens inside the server process.
//
// TODO: run servers in a sandbox. Today a server runs with Agenty's user, files
// and network; only its environment is restricted.
func command(srv Server) *exec.Cmd {
	cmd := exec.Command(srv.Command, srv.Args...) //nolint:gosec // G204: the operator configures which server to run.
	// Later entries win, so a configured PATH replaces the inherited one.
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	for _, key := range slices.Sorted(maps.Keys(srv.Env)) {
		cmd.Env = append(cmd.Env, key+"="+srv.Env[key])
	}
	return cmd
}

// connect performs the MCP handshake over t and returns the session named
// name. Start uses the command transport; tests use an in-memory one through
// export_test.go, so the same code path runs without a process.
func connect(ctx context.Context, name string, t mcp.Transport) (*Session, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "agenty", Version: clientVersion()}, nil)
	session, err := client.Connect(ctx, t, nil)
	if err != nil {
		return nil, fmt.Errorf("mcptool: connect to %s: %w", name, err)
	}
	return &Session{name: name, session: session}, nil
}

// Tools lists the server's tools once. Tools the server adds later are not
// picked up: the gateway fixes a run's tools when the run starts, so a server
// cannot slip a new tool in mid-run, and the model sees the same tools on
// every step.
//
// Each tool is named "<session>_<remote>", the name grants and policy use;
// the remote name is kept to call it on the server. The gateway checks the
// names, so a remote name that is not a valid tool name fails the run's
// start rather than being altered here.
func (s *Session) Tools(ctx context.Context) ([]toolgateway.Tool, error) {
	var tools []toolgateway.Tool
	for t, err := range s.session.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("mcptool: %s: list tools: %w", s.name, err)
		}
		name := s.name + "_" + t.Name
		schema, err := json.Marshal(t.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("mcptool: %s: tool %s: input schema: %w", s.name, t.Name, err)
		}
		tools = append(tools, &tool{
			session: s.session,
			remote:  t.Name,
			def: toolgateway.Definition{
				Name:        name,
				Description: t.Description,
				InputSchema: schema,
			},
		})
	}
	return tools, nil
}

// Close ends the session and stops the server.
func (s *Session) Close() error {
	return s.session.Close()
}

var _ toolgateway.Tool = (*tool)(nil)

// tool is one tool of an MCP server: the session to call it on, its name on
// the server, and its definition under the gateway's name.
type tool struct {
	session *mcp.ClientSession
	remote  string
	def     toolgateway.Definition
}

// Definition describes the tool under its gateway name, with the server's
// description and input schema.
func (t *tool) Definition() toolgateway.Definition {
	return t.def
}

// Call runs the tool on the server. Structured content is returned as is;
// otherwise the text content is returned as a JSON string, with placeholders
// for content the agent cannot use yet. A tool error becomes a Go error.
//
// Only the gateway calls it, after the grant, policy and audit checks.
func (t *tool) Call(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	// The model's arguments are untrusted text: refuse invalid JSON here
	// rather than send the server something it might misread. No arguments
	// are sent as none, not as JSON null.
	var arguments any
	if len(args) > 0 {
		if !json.Valid(args) {
			return nil, fmt.Errorf("mcptool: %s: arguments are not valid JSON", t.def.Name)
		}
		arguments = args
	}
	res, err := t.session.CallTool(ctx, &mcp.CallToolParams{Name: t.remote, Arguments: arguments})
	if err != nil {
		return nil, fmt.Errorf("mcptool: %s: %w", t.def.Name, err)
	}
	// A tool-level error, as opposed to a protocol one, is a Go error too, so
	// the gateway records it in the result's error field and the model sees
	// it as a failed call.
	text := contentText(res.Content)
	if res.IsError {
		if text == "" {
			text = "tool reported an error without a message"
		}
		return nil, errors.New(text)
	}
	// Prefer structured content: it is JSON already, so the model gets the
	// fields the server meant rather than their rendering as text.
	if res.StructuredContent != nil {
		out, err := json.Marshal(res.StructuredContent)
		if err != nil {
			return nil, fmt.Errorf("mcptool: %s: structured content: %w", t.def.Name, err)
		}
		return out, nil
	}
	out, err := json.Marshal(text)
	if err != nil {
		return nil, fmt.Errorf("mcptool: %s: text content: %w", t.def.Name, err)
	}
	return out, nil
}

// contentText joins a result's content into text, one block per line. Kinds
// the agent loop cannot pass to the model yet, such as images, become
// placeholders, so the model knows something was left out instead of
// receiving a silently shortened result.
func contentText(content []mcp.Content) string {
	parts := make([]string, 0, len(content))
	for _, c := range content {
		switch c := c.(type) {
		case *mcp.TextContent:
			parts = append(parts, c.Text)
		case *mcp.ImageContent:
			parts = append(parts, "[image omitted]")
		case *mcp.AudioContent:
			parts = append(parts, "[audio omitted]")
		case *mcp.ResourceLink:
			parts = append(parts, "[resource link omitted]")
		case *mcp.EmbeddedResource:
			parts = append(parts, "[resource omitted]")
		default:
			parts = append(parts, "[unsupported content omitted]")
		}
	}
	return strings.Join(parts, "\n")
}
