// Package mcptool reaches tools on MCP servers. It turns each tool of a server
// into a toolgateway.Tool named "<server>_<tool>", so the tool gateway can
// grant, police, execute and audit it like any other tool. This is the only
// package that talks to MCP servers.
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

// Session is a connection to one MCP server.
type Session struct {
	name    string
	session *mcp.ClientSession
}

// Start starts the server as name and connects to it. Only the tool gateway
// starts servers, and it checks the name first.
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

func connect(ctx context.Context, name string, t mcp.Transport) (*Session, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "agenty", Version: clientVersion()}, nil)
	session, err := client.Connect(ctx, t, nil)
	if err != nil {
		return nil, fmt.Errorf("mcptool: connect to %s: %w", name, err)
	}
	return &Session{name: name, session: session}, nil
}

// Tools lists the server's tools once. Tools the server adds later are not
// picked up.
func (s *Session) Tools(ctx context.Context) ([]toolgateway.Tool, error) {
	var tools []toolgateway.Tool
	for t, err := range s.session.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("mcptool: %s: list tools: %w", s.name, err)
		}
		name := s.name + "_" + t.Name
		if err := toolgateway.ValidateToolName(name); err != nil {
			return nil, fmt.Errorf("mcptool: %s: %w", s.name, err)
		}
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

// tool is one tool of an MCP server.
type tool struct {
	session *mcp.ClientSession
	remote  string
	def     toolgateway.Definition
}

func (t *tool) Definition() toolgateway.Definition {
	return t.def
}

// Call runs the tool on the server. Structured content is returned as is;
// otherwise the text content is returned as a JSON string, with placeholders
// for content the agent cannot use yet. A tool error becomes a Go error.
func (t *tool) Call(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
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
	text := contentText(res.Content)
	if res.IsError {
		if text == "" {
			text = "tool reported an error without a message"
		}
		return nil, errors.New(text)
	}
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
