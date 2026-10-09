// Package gatewaytest provides fakes for testing code that uses the tool gateway.
//
// The gateway depends only on its own interfaces, so tests of the gateway,
// the agent loop and the server swap in these fakes for real MCP servers, OPA
// policy and the database audit log: Tool and Server stand in for tool
// servers, Policy for internal/policy, Audit for the store, and NoSecrets for
// a redactor. The real gateway still runs in between, so a test through these
// fakes exercises the same grant, policy, audit and redaction path as
// production. Each fake records what it was asked, so tests can assert that a
// denied call never reached a tool and that every call left its records.
package gatewaytest

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"

	"github.com/jangraefen/agenty/internal/must"
	"github.com/jangraefen/agenty/internal/secret"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

var _ toolgateway.Tool = (*Tool)(nil)

// Tool is a fake tool that returns a fixed result and counts its calls. Runs
// may call it at once; read Calls and Args once they have ended.
type Tool struct {
	Name string
	// Description and InputSchema, if set, replace the generic ones.
	Description string
	InputSchema json.RawMessage
	Result      json.RawMessage
	Err         error
	// OnCall, if set, runs at the start of every call.
	OnCall func(ctx context.Context)

	mu    sync.Mutex
	Calls int
	// Args holds the arguments of the last call.
	Args json.RawMessage
}

// Definition describes the fake with a generic object schema.
func (t *Tool) Definition() toolgateway.Definition {
	return toolgateway.Definition{
		Name:        t.Name,
		Description: cmp.Or(t.Description, "fake "+t.Name),
		InputSchema: json.RawMessage(cmp.Or(string(t.InputSchema), `{"type":"object"}`)),
	}
}

// Call records the call and returns the configured result and error.
func (t *Tool) Call(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	if t.OnCall != nil {
		t.OnCall(ctx)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Calls++
	t.Args = args
	return t.Result, t.Err
}

// ErrAuditDown is returned by Audit for the event it is set to fail on.
var ErrAuditDown = errors.New("audit store down")

var _ toolgateway.Audit = (*Audit)(nil)

// Audit keeps every record and fails writes of the event in FailOn, so tests
// can check that a call whose decision or approval cannot be recorded never
// runs. It is not safe for concurrent use; one run's calls are sequential.
type Audit struct {
	Records []toolgateway.Record
	FailOn  toolgateway.Event
}

// Record stores rec, or returns ErrAuditDown if rec.Event is FailOn.
func (a *Audit) Record(_ context.Context, rec toolgateway.Record) error {
	if rec.Event == a.FailOn {
		return ErrAuditDown
	}
	a.Records = append(a.Records, rec)
	return nil
}

// WithoutIDs returns records with RunID and CallID cleared, for comparing
// everything else.
func WithoutIDs(records []toolgateway.Record) []toolgateway.Record {
	var out []toolgateway.Record
	for _, r := range records {
		r.RunID, r.CallID = "", ""
		out = append(out, r)
	}
	return out
}

var _ toolgateway.Policy = (*Policy)(nil)

// Policy is a fake policy. It returns the verdict configured for the tool,
// allows tools without one, and records every input. Allowing by default
// lets a test configure only the calls it is about; the gateway's own grant
// check still denies anything not granted.
type Policy struct {
	Verdicts map[string]toolgateway.Verdict
	Err      error

	Inputs []toolgateway.Request
}

// Evaluate records in and returns the configured verdict or error.
func (p *Policy) Evaluate(_ context.Context, in toolgateway.Request) (toolgateway.Verdict, error) {
	p.Inputs = append(p.Inputs, in)
	if p.Err != nil {
		return toolgateway.Verdict{}, p.Err
	}
	if v, ok := p.Verdicts[in.Tool]; ok {
		return v, nil
	}
	return toolgateway.Verdict{Decision: toolgateway.Allow}, nil
}

var _ toolgateway.ToolServer = (*Server)(nil)

// Server is a fake tool server. Its session serves Tools, and it records how
// often it was started and closed.
type Server struct {
	Tools    []toolgateway.Tool
	StartErr error
	ToolsErr error
	CloseErr error
	// OnStart, if set, runs at the start of every start; an error it returns
	// fails the start.
	OnStart func(ctx context.Context) error
	// OnClose, if set, runs at the start of every close.
	OnClose func()

	// StartedAs holds the name of every start.
	StartedAs []string
	Closed    int

	// mu guards StartedAs and Closed while runs start and stop concurrently;
	// tests read them once the runs are over.
	mu sync.Mutex
}

// ClosedCount returns Closed, for reading while sessions may still close.
func (s *Server) ClosedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Closed
}

// Start records the start and returns a session, or StartErr.
func (s *Server) Start(ctx context.Context, name string) (toolgateway.ToolSession, error) {
	s.mu.Lock()
	s.StartedAs = append(s.StartedAs, name)
	s.mu.Unlock()
	if s.OnStart != nil {
		if err := s.OnStart(ctx); err != nil {
			return nil, err
		}
	}
	if s.StartErr != nil {
		return nil, s.StartErr
	}
	return &session{server: s}, nil
}

var _ toolgateway.ToolSession = (*session)(nil)

// session is a started Server; it serves the server's Tools and counts its
// closes on the server.
type session struct {
	server *Server
}

// Tools returns the server's Tools and ToolsErr.
func (s *session) Tools(context.Context) ([]toolgateway.Tool, error) {
	return s.server.Tools, s.server.ToolsErr
}

// Close runs OnClose, counts the close and returns CloseErr.
func (s *session) Close() error {
	if s.server.OnClose != nil {
		s.server.OnClose()
	}
	s.server.mu.Lock()
	s.server.Closed++
	s.server.mu.Unlock()
	return s.server.CloseErr
}

// Servers puts tools on fake servers by the server part of their names, as in
// "<server>_<tool>", so tests can hand any set of fake tools to the gateway.
func Servers(tools ...toolgateway.Tool) map[string]toolgateway.ToolServer {
	byName := map[string]*Server{}
	for _, tool := range tools {
		name, _, _ := strings.Cut(tool.Definition().Name, "_")
		if byName[name] == nil {
			byName[name] = &Server{}
		}
		byName[name].Tools = append(byName[name].Tools, tool)
	}
	servers := make(map[string]toolgateway.ToolServer, len(byName))
	for name, srv := range byName {
		servers[name] = srv
	}
	return servers
}

// NoSecrets is a redactor without secrets, for tests that do not handle
// credentials.
var NoSecrets = must.Value(secret.NewRedactor(nil))
