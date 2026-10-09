// Package server is Agenty's HTTP API: it stores harnesses, runs them, and
// streams each run's events, approval requests included, to its clients.
// Runs execute in the server's own process. schema/openapi.yaml defines the
// routes, which oapi-codegen generates the gin interface of into this package.
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
	// pool keeps the MCP servers of each conversation between its runs.
	pool *toolgateway.Pool

	// wake wakes a worker waiting for a queued run.
	wake chan struct{}
	// expiry wakes the goroutine that expires approval requests when a
	// request is added.
	expiry chan struct{}

	// events wakes the readers of runs' events.
	events notifier
	// stopped is closed once Close has stopped every run it stops.
	stopped chan struct{}

	mu sync.Mutex
	// closed is set by Close; no run is queued after it.
	closed bool
	// runs holds the jobs of the runs of this server that have not finished:
	// queued, running and waiting.
	runs map[string]*job
}

// errClosed is returned when a run is started on a closed server.
var errClosed = errors.New("the server is stopping")

// New returns a Server for cfg and starts its workers. Runs left running by an
// earlier server cannot continue, so New marks them as failed; the runs it
// left queued are taken up.
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
	if _, err := cfg.Store.AppendEvent(ctx, serverStarted(cfg)); err != nil {
		return nil, fmt.Errorf("server: %w", err)
	}
	n, err := cfg.Store.FailRunningRuns(ctx, errStopped)
	if err != nil {
		return nil, fmt.Errorf("server: %w", err)
	}
	if n > 0 {
		cfg.Logger.Warn("marked runs of an earlier server as failed", "runs", n)
	}

	idleTimeout := make(map[string]time.Duration, len(cfg.Operator.MCPServers))
	for name, srv := range cfg.Operator.MCPServers {
		idleTimeout[name] = srv.IdleTimeoutOrDefault()
	}
	idle, err := cfg.Store.IdleRuns(ctx)
	if err != nil {
		return nil, fmt.Errorf("server: %w", err)
	}
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	workers := cfg.Operator.Runs.WorkersOrDefault()
	s := &Server{
		cfg: cfg, ctx: runCtx, cancel: cancel, runs: map[string]*job{}, tokens: hashTokens(cfg.Resolved.UserTokens),
		pool:    toolgateway.NewPool(idleTimeout, cfg.Logger),
		wake:    make(chan struct{}, workers),
		expiry:  make(chan struct{}, 1),
		stopped: make(chan struct{}),
	}
	// The runs an earlier server left queued or waiting become this
	// server's jobs, before any worker may claim them. Those of a user who
	// left the run's workspace are cancelled instead: no agent acts for
	// someone who left.
	queued := 0
	for _, r := range idle {
		j, err := s.register(r.ID, r.Workspace, r.Harness)
		if err != nil {
			panic(err) // The server is not closed yet.
		}
		if !s.isMember(r.Owner, r.Workspace) {
			if err := s.cancelLeft(ctx, j, r); err != nil {
				s.Close()
				return nil, fmt.Errorf("server: %w", err)
			}
			continue
		}
		if r.Status == store.RunQueued {
			queued++
		}
	}
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

// Handler returns the API's HTTP handler.
func (s *Server) Handler() http.Handler {
	return s.engine
}

// Close cancels every running run and waits until each has been recorded as
// finished, then stops the MCP servers kept for conversations. No run is
// queued after it. Event streams end with it; those of queued and waiting
// runs, which the next server takes up, end without the run's end.
func (s *Server) Close() {
	s.mu.Lock()
	closed := s.closed
	s.closed = true
	s.mu.Unlock()
	s.cancel()
	s.wg.Wait()
	if !closed {
		close(s.stopped)
	}
	if err := s.pool.Close(); err != nil {
		s.cfg.Logger.Error("cannot stop the MCP servers of conversations", "error", err)
	}
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
