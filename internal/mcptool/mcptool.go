// Package mcptool reaches tools on MCP servers. It turns each tool of a server
// into a toolgateway.Tool named "<server>.<tool>", so the tool gateway can
// grant, police, execute and audit it like any other tool. This is the only
// package that talks to MCP servers.
package mcptool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jangraefen/agenty/internal/toolgateway"
)

const clientVersion = "0.0.0"

var serverName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Server is an MCP server Agenty starts as a subprocess and talks to over
// stdin and stdout.
type Server struct {
	// Name prefixes the server's tools, as in "<name>.<tool>".
	Name    string
	Command string
	Args    []string
	// Env is the environment of the server process, as "KEY=value" entries.
	// Only PATH is inherited from Agenty's own environment, so Agenty's
	// credentials never reach a server unless configured here.
	Env []string
}

// Session is a connection to one MCP server.
type Session struct {
	name    string
	session *mcp.ClientSession
}

// Connect starts the server and connects to it.
func Connect(ctx context.Context, srv Server) (*Session, error) {
	if err := validateName(srv.Name); err != nil {
		return nil, err
	}
	if srv.Command == "" {
		return nil, fmt.Errorf("mcptool: server %s: command is required", srv.Name)
	}
	cmd := exec.Command(srv.Command, srv.Args...) //nolint:gosec // G204: the operator configures which server to run.
	// Later entries win, so a configured PATH replaces the inherited one.
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH")}, srv.Env...)
	return connect(ctx, srv.Name, &mcp.CommandTransport{Command: cmd})
}

func connect(ctx context.Context, name string, t mcp.Transport) (*Session, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "agenty", Version: clientVersion}, nil)
	session, err := client.Connect(ctx, t, nil)
	if err != nil {
		return nil, fmt.Errorf("mcptool: connect to %s: %w", name, err)
	}
	return &Session{name: name, session: session}, nil
}

func validateName(name string) error {
	if !serverName.MatchString(name) {
		return fmt.Errorf("mcptool: server name %q must be lowercase letters, digits and single hyphens", name)
	}
	return nil
}

// Tools lists the server's tools once. Tools the server adds later are not
// picked up.
func (s *Session) Tools(ctx context.Context) ([]toolgateway.Tool, error) {
	var tools []toolgateway.Tool
	for t, err := range s.session.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("mcptool: %s: list tools: %w", s.name, err)
		}
		schema, _ := json.Marshal(t.InputSchema) // decoded from JSON, so it always marshals
		tools = append(tools, &tool{
			session: s.session,
			remote:  t.Name,
			def: toolgateway.Definition{
				Name:        s.name + "." + t.Name,
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
		out, _ := json.Marshal(res.StructuredContent) // decoded from JSON, so it always marshals
		return out, nil
	}
	out, _ := json.Marshal(text) // a string always marshals
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
