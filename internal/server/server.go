// Package server is Agenty's HTTP API: it stores harnesses, runs them, and
// streams each run's events, approval requests included, to its clients.
// Runs execute in the server's own process; see package api for the routes.
//
// Every request signs in with a configured user's bearer token, and sees
// only the workspaces that user is a member of.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jangraefen/agenty/internal/agent"
	"github.com/jangraefen/agenty/internal/api"
	"github.com/jangraefen/agenty/internal/config"
	"github.com/jangraefen/agenty/internal/harness"
	"github.com/jangraefen/agenty/internal/mcptool"
	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/model/anthropic"
	"github.com/jangraefen/agenty/internal/policy"
	"github.com/jangraefen/agenty/internal/store"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

// Config configures a Server.
type Config struct {
	Store    *store.Store
	Operator *config.Config
	// Resolved holds the operator config's values, the users' tokens among
	// them, and the redactor every response, event, stored run and log line
	// passes through.
	Resolved *config.Resolved
	Logger   *slog.Logger
	// NewModel returns the model a harness runs on. Nil means the
	// configured provider.
	NewModel func(harness.Model) (model.Model, error)
	// Server returns the tool server for a configured MCP server. Nil means
	// srv itself, started as a subprocess.
	Server func(name string, srv mcptool.Server) toolgateway.ToolServer
}

// Server serves the API. Close it to stop its runs.
type Server struct {
	cfg    Config
	engine *gin.Engine

	// ctx is the context of every run; Close cancels it.
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// tokens maps each user's token, as a SHA-256 hash, to the user.
	tokens []userToken

	mu sync.Mutex
	// closed is set by Close; no run starts after it.
	closed bool
	runs   map[string]*hub
}

// errClosed is returned when a run is started on a closed server.
var errClosed = errors.New("the server is stopping")

// New returns a Server for cfg. Runs left running by an earlier server cannot
// continue, so New marks them as failed.
func New(ctx context.Context, cfg Config) (*Server, error) {
	switch {
	case cfg.Store == nil:
		return nil, errors.New("server: store is required")
	case cfg.Operator == nil || cfg.Resolved == nil:
		return nil, errors.New("server: config is required")
	case cfg.Logger == nil:
		return nil, errors.New("server: logger is required")
	}
	if cfg.NewModel == nil {
		cfg.NewModel = func(m harness.Model) (model.Model, error) {
			if m.Provider != "anthropic" {
				return nil, fmt.Errorf("model provider %q is not supported; use anthropic", m.Provider)
			}
			return anthropic.New(anthropic.Config{
				APIKey:    cfg.Resolved.AnthropicAPIKey,
				Model:     m.Name,
				MaxTokens: cfg.Operator.Provider.Anthropic.MaxTokens,
				BaseURL:   cfg.Operator.Provider.Anthropic.BaseURL,
			})
		}
	}
	if cfg.Server == nil {
		cfg.Server = func(_ string, srv mcptool.Server) toolgateway.ToolServer { return srv }
	}
	// Every run compiles central policy; compiling it now finds a mistake
	// before the first run does.
	if _, err := policy.New(ctx, policy.Layer{Name: "central", Modules: cfg.Operator.Policy}); err != nil {
		return nil, fmt.Errorf("server: %w", err)
	}
	n, err := cfg.Store.FailRunningRuns(ctx, "the server stopped before the run finished")
	if err != nil {
		return nil, fmt.Errorf("server: %w", err)
	}
	if n > 0 {
		cfg.Logger.Warn("marked runs of an earlier server as failed", "runs", n)
	}

	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s := &Server{cfg: cfg, ctx: runCtx, cancel: cancel, runs: map[string]*hub{}, tokens: hashTokens(cfg.Resolved.UserTokens)}
	s.engine = s.routes()
	return s, nil
}

// Handler returns the API's HTTP handler.
func (s *Server) Handler() http.Handler {
	return s.engine
}

// Close cancels every run and waits until each has been recorded as finished.
// Event streams end with it, and no run starts after it.
func (s *Server) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.cancel()
	s.wg.Wait()
}

func (s *Server) routes() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	// Global middleware also runs for unknown routes, so preflight requests,
	// which no route serves, are answered too.
	// Sign-in is global middleware too, after the preflight answer, so that
	// unknown routes ask for a token like every other.
	r.Use(gin.Recovery(), s.logRequests, s.localOnly, s.cors, s.authenticate)
	r.NoRoute(func(c *gin.Context) {
		s.fail(c, http.StatusNotFound, fmt.Errorf("%s %s: no such route", c.Request.Method, c.Request.URL.Path))
	})
	RegisterHandlersWithOptions(r, handlers{s}, GinServerOptions{
		Middlewares: []MiddlewareFunc{s.member},
		ErrorHandler: func(c *gin.Context, err error, status int) {
			s.fail(c, status, err)
		},
	})
	return r
}

// localOnly refuses requests that are not for a local host. Until the API
// has TLS, it listens on a loopback address only, so that tokens never cross
// a network unencrypted; a request for any other host is a DNS-rebound page
// using the API as its own.
func (s *Server) localOnly(c *gin.Context) {
	if !isLocalHost(c.Request.Host) {
		s.fail(c, http.StatusForbidden, fmt.Errorf("host %q is not local", c.Request.Host))
		return
	}
	c.Next()
}

// cors lets pages of the configured origins call the API and refuses
// requests from pages of any other origin. Clients such as the CLI send no
// Origin. A preflight request is answered here, before sign-in: browsers send
// it without credentials.
func (s *Server) cors(c *gin.Context) {
	h := c.Writer.Header()
	h.Add("Vary", "Origin")
	origin := c.GetHeader("Origin")
	if origin == "" {
		c.Next()
		return
	}
	if !slices.Contains(s.cfg.Operator.CORS.Origins, origin) {
		s.fail(c, http.StatusForbidden, fmt.Errorf("requests from origin %q are not accepted", origin))
		return
	}
	h.Set("Access-Control-Allow-Origin", origin)
	if c.Request.Method == http.MethodOptions && c.GetHeader("Access-Control-Request-Method") != "" {
		h.Set("Access-Control-Allow-Methods", "GET, POST, PUT")
		h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		h.Set("Access-Control-Max-Age", "600")
		c.AbortWithStatus(http.StatusNoContent)
		return
	}
	c.Next()
}

// isLocalHost reports whether host, with or without a port, is localhost or
// a loopback address.
func isLocalHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func (s *Server) logRequests(c *gin.Context) {
	start := time.Now()
	c.Next()
	s.cfg.Logger.Info("request", "method", c.Request.Method, "path", c.Request.URL.Path, "user", c.GetString(userKey), "status", c.Writer.Status(), "duration", time.Since(start))
}

// fail responds with an error, redacted. Errors the client did not cause are
// logged.
func (s *Server) fail(c *gin.Context, status int, err error) {
	msg := s.cfg.Resolved.Redactor.String(err.Error())
	if status >= http.StatusInternalServerError {
		s.cfg.Logger.Error("request failed", "path", c.Request.URL.Path, "error", msg)
	}
	c.AbortWithStatusJSON(status, api.Error{Error: msg})
}

// failStore responds to a store error: ErrNotFound is a 404.
func (s *Server) failStore(c *gin.Context, err error) {
	if errors.Is(err, store.ErrNotFound) {
		s.fail(c, http.StatusNotFound, err)
		return
	}
	s.fail(c, http.StatusInternalServerError, err)
}

// errNotJSON is returned for a body that is not declared as JSON. Requiring
// the content type keeps cross-site forms, which cannot send JSON without a
// preflight, out.
var errNotJSON = errors.New("the request body must be application/json")

func (s *Server) failDecode(c *gin.Context, err error) {
	if errors.Is(err, errNotJSON) {
		s.fail(c, http.StatusUnsupportedMediaType, err)
		return
	}
	s.fail(c, http.StatusBadRequest, err)
}

// decode reads the JSON body into v, rejecting unknown fields.
func decode(c *gin.Context, v any) error {
	if mt, _, err := mime.ParseMediaType(c.GetHeader("Content-Type")); err != nil || mt != "application/json" {
		return errNotJSON
	}
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	return nil
}

// newRun is a run to start.
type newRun struct {
	version store.HarnessVersion
	input   string
	// user is who starts the run.
	user string
	// follows, if set, is the ID of the run whose conversation the run
	// continues, and prior are that conversation's runs so far.
	follows string
	prior   []priorRun
}

// start builds an agent for the run's harness version, which starts the MCP
// servers it needs, stores the run, and executes it in the background. On
// error it also returns the status to respond with: 422 for a harness that
// cannot run, 409 for a run that another run already follows, 503 when the
// server is stopping, 500 otherwise.
func (s *Server) start(r newRun) (string, int, error) {
	v := r.version
	h := v.Harness
	m, err := s.cfg.NewModel(h.Model)
	if err != nil {
		return "", http.StatusUnprocessableEntity, err
	}
	servers := make(map[string]toolgateway.ToolServer, len(s.cfg.Operator.MCPServers))
	for name, srv := range s.cfg.Operator.MCPServers {
		servers[name] = s.cfg.Server(name, mcptool.Server{Command: srv.Command, Args: srv.Args, Env: s.cfg.Resolved.MCPServerEnv[name]})
	}
	timeout := s.cfg.Operator.Approvals.Timeout
	if timeout == 0 {
		timeout = config.DefaultApprovalTimeout
	}
	hub := newHub(v.Workspace, h.Name, timeout)
	a, err := agent.New(s.ctx, agent.Config{
		Harness:    &h,
		Model:      m,
		Servers:    servers,
		Policy:     s.cfg.Operator.Policy,
		Approver:   hub,
		Audit:      runAudit{store: s.cfg.Store, hub: hub},
		Redactor:   s.cfg.Resolved.Redactor,
		Transcript: runTranscript{store: s.cfg.Store, redact: s.cfg.Resolved.Redactor},
	})
	if err != nil {
		return "", http.StatusUnprocessableEntity, err
	}
	digest := a.PromptDigest()
	history := conversationHistory(s.cfg.Resolved.Redactor, r.prior, digest)
	run := a.Start()
	hub.runID = run.ID()
	runCtx, cancel := context.WithCancelCause(s.ctx)
	hub.cancel = cancel
	// Register the run's hub before storing it, so a stored running run of
	// this server always has an event stream. Registering under the lock
	// Close takes means no run starts once Close has begun waiting.
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		cancel(nil)
		return "", http.StatusServiceUnavailable, errors.Join(errClosed, a.Close())
	}
	s.runs[run.ID()] = hub
	s.wg.Add(1)
	s.mu.Unlock()
	if err := s.cfg.Store.CreateRun(s.ctx, store.NewRun{ID: run.ID(), HarnessVersionID: v.ID, Input: r.input, StartedBy: r.user, Follows: r.follows, PromptDigest: digest, HistoryDigest: historyDigest(history)}); err != nil {
		s.mu.Lock()
		delete(s.runs, run.ID())
		s.mu.Unlock()
		s.wg.Done()
		cancel(nil)
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrConflict) {
			// Another follow-up of the same run was stored first.
			status = http.StatusConflict
			err = fmt.Errorf("run %s is already followed up; follow up the conversation's latest run", r.follows)
		}
		return "", status, errors.Join(err, a.Close())
	}
	go func() {
		defer s.wg.Done()
		defer cancel(nil)
		s.execute(runCtx, a, run, hub, history, r.input)
	}()
	return run.ID(), 0, nil
}

// cancelledBy is the cause of a run's cancellation by a user: the user.
type cancelledBy string

func (u cancelledBy) Error() string { return "cancelled by " + string(u) }

// execute runs the agent on ctx, after history, stops its MCP servers,
// records how the run ended, and publishes that as the run's last event. A
// run that fails after a user cancelled it ended as cancelled.
func (s *Server) execute(ctx context.Context, a *agent.Agent, run *agent.Run, hub *hub, history []model.Message, input string) {
	defer func() {
		s.mu.Lock()
		delete(s.runs, run.ID())
		s.mu.Unlock()
	}()
	redact := s.cfg.Resolved.Redactor
	res, runErr := run.Continue(ctx, history, input)
	err := errors.Join(runErr, a.Close())
	status, errMsg := store.RunSucceeded, ""
	var by cancelledBy
	switch {
	case runErr != nil && errors.As(context.Cause(ctx), &by):
		// The cause is what the run's record says; any other error, such as
		// a server that did not stop, is logged.
		status, errMsg = store.RunCancelled, by.Error()
		s.cfg.Logger.Info("run cancelled", "run_id", run.ID(), "error", err)
	case err != nil:
		status, errMsg = store.RunFailed, redact.String(err.Error())
	}
	// The run's context may be cancelled; how the run ended is recorded
	// regardless.
	ctx = context.WithoutCancel(ctx)
	if err := s.cfg.Store.FinishRun(ctx, run.ID(), status, redact.String(res.Output), res.Steps, errMsg); err != nil {
		s.cfg.Logger.Error("cannot record the end of a run", "run_id", run.ID(), "error", err)
	}
	s.cfg.Logger.Info("run finished", "harness", hub.harness, "run_id", run.ID(), "status", status, "steps", res.Steps)
	finished, err := s.cfg.Store.Run(ctx, hub.workspace, run.ID())
	if err != nil {
		s.cfg.Logger.Error("cannot read a finished run", "run_id", run.ID(), "error", err)
		finished = store.Run{ID: run.ID(), Status: status, Output: redact.String(res.Output), Steps: res.Steps, Error: errMsg}
	}
	hub.publish(event{api.EventFinished, apiRun(finished)})
}

// hub returns the hub of the running run id in workspace, or nil if there is
// none.
func (s *Server) hub(workspace, id string) *hub {
	s.mu.Lock()
	defer s.mu.Unlock()
	if h := s.runs[id]; h != nil && h.workspace == workspace {
		return h
	}
	return nil
}
