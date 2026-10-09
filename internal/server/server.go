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

// Config configures a Server: the store it keeps its state in, the operator
// config read once at the start, and the two seams tests replace, the model
// and the tool servers. New checks that the required fields are set and fills
// in the seams left nil.
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
//
// It is the hub of the package: the handlers embed it, the workers and the
// expire goroutine run on it, and runs holds the event hub of every run it
// has not finished. Its state is the operator config, fixed for its life, and
// the store; what it holds in memory is only what a restart may lose: the
// hubs, rebuilt from the store by New, and the MCP servers kept for
// conversations, which a conversation's next run starts anew.
type Server struct {
	// cfg is the Config New was given, its seams filled in and its logger
	// redacting.
	cfg Config
	// engine is the gin engine routes built, which Handler returns.
	engine *gin.Engine

	// ctx is the context of every run; Close cancels it.
	ctx    context.Context
	cancel context.CancelFunc
	// wg waits for the workers and the expire goroutine.
	wg sync.WaitGroup

	// tokens maps each user's token, as a SHA-256 hash, to the user.
	tokens []userToken
	// pool keeps the MCP servers of each conversation between its runs.
	pool *toolgateway.Pool

	// wake wakes a worker waiting for a queued run.
	wake chan struct{}
	// expiry wakes the goroutine that expires approval requests when a
	// request is added.
	expiry chan struct{}

	mu sync.Mutex
	// closed is set by Close; no run is queued after it.
	closed bool
	// runs holds the hubs of the runs of this server that have not finished:
	// queued, running and waiting.
	runs map[string]*hub
}

// errClosed is returned when a run is started on a closed server.
var errClosed = errors.New("the server is stopping")

// New returns a Server for cfg and starts its workers. Runs left running by an
// earlier server cannot continue, so New marks them as failed; the runs it
// left queued are taken up.
//
// In order, it checks cfg, wraps the logger in the redactor, compiles central
// policy, records server.started, fails the runs left running, rebuilds the
// hubs of queued and waiting runs (cancelling those of users who left), and
// only then starts the workers and the expire goroutine, so no worker claims
// a run whose stream is not ready yet. ctx bounds only this start: the
// server's runs live on a context of their own, which Close cancels.
func New(ctx context.Context, cfg Config) (*Server, error) {
	switch {
	case cfg.Store == nil:
		return nil, errors.New("server: store is required")
	case cfg.Operator == nil || cfg.Resolved == nil:
		return nil, errors.New("server: config is required")
	case cfg.Resolved.Redactor == nil:
		return nil, errors.New("server: redactor is required")
	case cfg.Logger == nil:
		return nil, errors.New("server: logger is required")
	}
	// Whatever logger the server is given, it logs no credentials.
	cfg.Logger = slog.New(cfg.Resolved.Redactor.Handler(cfg.Logger.Handler()))
	// The seams default to production: the configured Anthropic provider,
	// the only one so far, and MCP servers started as subprocesses.
	if cfg.NewModel == nil {
		cfg.NewModel = func(m harness.Model) (model.Model, error) {
			if m.Provider != "anthropic" {
				return nil, fmt.Errorf("model provider %q is not supported; use anthropic", m.Provider)
			}
			return anthropic.New(anthropic.Config{
				APIKey:          cfg.Resolved.AnthropicAPIKey,
				Model:           m.Name,
				MaxTokens:       cfg.Operator.Provider.Anthropic.MaxTokens,
				BaseURL:         cfg.Operator.Provider.Anthropic.BaseURL,
				HistoryCacheTTL: cfg.Operator.Provider.Anthropic.HistoryCacheTTL,
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
	// The config in force is recorded before anything acts under it, so every
	// later event is read against the latest server.started before it. A
	// server that cannot record it does not start.
	if _, err := cfg.Store.AppendEvent(ctx, serverStarted(cfg)); err != nil {
		return nil, fmt.Errorf("server: %w", err)
	}
	// One server serves a database, so a run still marked running was the
	// earlier server's, and its goroutine is gone. It is failed, never
	// retried: a tool may not be idempotent.
	n, err := cfg.Store.FailRunningRuns(ctx, errStopped)
	if err != nil {
		return nil, fmt.Errorf("server: %w", err)
	}
	if n > 0 {
		cfg.Logger.Warn("marked runs of an earlier server as failed", "runs", n)
	}

	// The pool keeps each MCP server for its own idle timeout.
	idleTimeout := make(map[string]time.Duration, len(cfg.Operator.MCPServers))
	for name, srv := range cfg.Operator.MCPServers {
		idleTimeout[name] = srv.IdleTimeoutOrDefault()
	}
	idle, err := cfg.Store.IdleRuns(ctx)
	if err != nil {
		return nil, fmt.Errorf("server: %w", err)
	}
	// Runs outlive the start's context, which may be a request's or a
	// signal's; only Close ends them.
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	workers := cfg.Operator.Runs.WorkersOrDefault()
	s := &Server{
		cfg: cfg, ctx: runCtx, cancel: cancel, runs: map[string]*hub{}, tokens: hashTokens(cfg.Resolved.UserTokens),
		pool:   toolgateway.NewPool(idleTimeout, cfg.Logger),
		wake:   make(chan struct{}, workers),
		expiry: make(chan struct{}, 1),
	}
	// The runs an earlier server left queued or waiting get their event
	// streams, with what they recorded so far, before any worker may claim
	// them. Those of a user who left the run's workspace are cancelled
	// instead: no agent acts for someone who left.
	queued := 0
	for _, r := range idle {
		h, err := s.register(r.ID, r.Workspace, r.Harness)
		if err != nil {
			panic(err) // The server is not closed yet.
		}
		if !s.isMember(r.Owner, r.Workspace) {
			if err := s.cancelLeft(ctx, h, r); err != nil {
				s.Close()
				return nil, fmt.Errorf("server: %w", err)
			}
			continue
		}
		if err := s.replay(ctx, h, r.Status); err != nil {
			s.Close()
			return nil, fmt.Errorf("server: %w", err)
		}
		if r.Status == store.RunQueued {
			queued++
		}
	}
	// The workers wait for a signal, so one is sent for each run left
	// queued, up to one per worker: a woken worker claims until none is left.
	for range workers {
		s.wg.Go(s.work)
	}
	for range min(queued, workers) {
		s.signal()
	}
	s.wg.Go(s.expire)
	s.engine = s.routes()
	return s, nil
}

// Handler returns the API's HTTP handler: the gin engine routes built, with
// its middleware. `agenty serve` serves it on a loopback address.
func (s *Server) Handler() http.Handler {
	return s.engine
}

// Close cancels every running run and waits until each has been recorded as
// finished, then stops the MCP servers kept for conversations. No run is
// queued after it. Event streams end with it; those of queued and waiting
// runs, which the next server takes up, end without the run's end.
//
// Setting closed under the lock first means register refuses new runs, so
// no worker is left with a run after Wait returns.
func (s *Server) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.cancel()
	s.wg.Wait()
	// What is left are queued and waiting runs, which the next server takes
	// up: their event streams end without an end.
	s.mu.Lock()
	for _, h := range s.runs {
		h.stop()
	}
	s.mu.Unlock()
	if err := s.pool.Close(); err != nil {
		s.cfg.Logger.Error("cannot stop the MCP servers of conversations", "error", err)
	}
}

// routes builds the gin engine: the global middleware every request passes,
// a 404 for unknown routes, and the generated routes, each behind the
// membership check and with parameter errors answered through fail like
// every other error.
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

// logRequests logs every request once it is answered: method, path, user,
// status and duration. It logs the path, not the query or body, and the
// server's logger redacts what it is given, so a credential in a request is
// not logged.
func (s *Server) logRequests(c *gin.Context) {
	start := time.Now()
	c.Next()
	s.cfg.Logger.Info("request", "method", c.Request.Method, "path", c.Request.URL.Path, "user", c.GetString(userKey), "status", c.Writer.Status(), "duration", time.Since(start))
}

// fail responds with an error, redacted. Errors the client did not cause are
// logged. It aborts the gin chain, so middleware that fails stops the request
// there. Every error answer of the package goes through it, so no error text,
// which may quote a tool's output or a configured value, reaches a client
// unredacted.
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

// failDecode responds to an error of decode: 415 for a body not declared as
// JSON, 400 for one that does not decode.
func (s *Server) failDecode(c *gin.Context, err error) {
	if errors.Is(err, errNotJSON) {
		s.fail(c, http.StatusUnsupportedMediaType, err)
		return
	}
	s.fail(c, http.StatusBadRequest, err)
}

// decode reads the JSON body into v, rejecting unknown fields, so a
// misspelt field is an error rather than silently ignored. The body must be
// declared as application/json; see errNotJSON.
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
