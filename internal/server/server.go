// Package server is Agenty's HTTP API: it stores harnesses, runs them, and
// streams each run's events, approval requests included, to its clients.
// Runs execute in the server's own process; see package api for the routes.
//
// Every request signs in with a configured user's bearer token, and sees
// only the workspaces that user is a member of.
package server

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"mime"
	"net"
	"net/http"
	"slices"
	"strconv"
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
	v1 := r.Group("/v1")
	v1.GET("/me", s.me)
	ws := v1.Group("/workspaces/:workspace", s.member)
	ws.PUT("/harnesses/:name", s.putHarness)
	ws.GET("/harnesses", s.listHarnesses)
	ws.GET("/harnesses/:name", s.getHarness)
	ws.POST("/runs", s.createRun)
	ws.GET("/runs", s.listRuns)
	ws.GET("/runs/:id", s.getRun)
	ws.POST("/runs/:id/cancel", s.cancelRun)
	ws.GET("/approvals", s.listApprovals)
	ws.GET("/runs/:id/audit", s.getAudit)
	ws.GET("/runs/:id/transcript", s.getTranscript)
	ws.GET("/runs/:id/events", s.streamEvents)
	ws.POST("/runs/:id/approvals/:approval", s.answerApproval)
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

func (s *Server) me(c *gin.Context) {
	user := c.GetString(userKey)
	workspaces := []string{}
	for _, name := range slices.Sorted(maps.Keys(s.cfg.Operator.Workspaces)) {
		if slices.Contains(s.cfg.Operator.Workspaces[name].Members, user) {
			workspaces = append(workspaces, name)
		}
	}
	c.JSON(http.StatusOK, api.Me{User: user, Workspaces: workspaces})
}

func (s *Server) putHarness(c *gin.Context) {
	var h harness.Harness
	if err := decode(c, &h); err != nil {
		s.failDecode(c, err)
		return
	}
	if h.Name != c.Param("name") {
		s.fail(c, http.StatusBadRequest, fmt.Errorf("harness name %q does not match the path", h.Name))
		return
	}
	if err := h.Validate(); err != nil {
		s.fail(c, http.StatusBadRequest, fmt.Errorf("invalid harness: %w", err))
		return
	}
	// Reject policy that does not compile now, rather than at its first run.
	if _, err := policy.New(c.Request.Context(), policy.Layer{Name: "harness", Modules: h.Policy}); err != nil {
		s.fail(c, http.StatusBadRequest, fmt.Errorf("invalid harness: %w", err))
		return
	}
	v, err := s.cfg.Store.PutHarness(c.Request.Context(), c.Param("workspace"), h)
	if err != nil {
		s.failStore(c, err)
		return
	}
	c.JSON(http.StatusOK, harnessVersion(v))
}

func (s *Server) listHarnesses(c *gin.Context) {
	versions, err := s.cfg.Store.Harnesses(c.Request.Context(), c.Param("workspace"))
	if err != nil {
		s.failStore(c, err)
		return
	}
	out := make([]api.HarnessVersion, len(versions))
	for i, v := range versions {
		out[i] = harnessVersion(v)
	}
	c.JSON(http.StatusOK, out)
}

func (s *Server) getHarness(c *gin.Context) {
	v, err := s.cfg.Store.Harness(c.Request.Context(), c.Param("workspace"), c.Param("name"))
	if err != nil {
		s.failStore(c, err)
		return
	}
	c.JSON(http.StatusOK, harnessVersion(v))
}

func harnessVersion(v store.HarnessVersion) api.HarnessVersion {
	return api.HarnessVersion{ID: v.ID, Version: v.Version, Harness: v.Harness, CreatedAt: v.CreatedAt}
}

func (s *Server) createRun(c *gin.Context) {
	var req api.CreateRun
	if err := decode(c, &req); err != nil {
		s.failDecode(c, err)
		return
	}
	if req.Input == "" {
		s.fail(c, http.StatusBadRequest, errors.New("input is required"))
		return
	}
	ws := c.Param("workspace")
	v, err := s.cfg.Store.Harness(c.Request.Context(), ws, req.Harness)
	if err != nil {
		s.failStore(c, err)
		return
	}
	id, status, err := s.start(v, req.Input, c.GetString(userKey))
	if err != nil {
		s.fail(c, status, fmt.Errorf("cannot start run: %w", err))
		return
	}
	run, err := s.cfg.Store.Run(c.Request.Context(), ws, id)
	if err != nil {
		s.failStore(c, err)
		return
	}
	c.JSON(http.StatusCreated, apiRun(run))
}

// start builds an agent for the harness version, which starts the MCP servers
// it needs, stores the run as started by user, and executes it in the
// background. On error it also returns the status to respond with: 422 for a
// harness that cannot run, 503 when the server is stopping, 500 otherwise.
func (s *Server) start(v store.HarnessVersion, input, user string) (string, int, error) {
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
	if err := s.cfg.Store.CreateRun(s.ctx, store.NewRun{ID: run.ID(), HarnessVersionID: v.ID, Input: input, StartedBy: user}); err != nil {
		s.mu.Lock()
		delete(s.runs, run.ID())
		s.mu.Unlock()
		s.wg.Done()
		cancel(nil)
		return "", http.StatusInternalServerError, errors.Join(err, a.Close())
	}
	go func() {
		defer s.wg.Done()
		defer cancel(nil)
		s.execute(runCtx, a, run, hub, input)
	}()
	return run.ID(), 0, nil
}

// cancelledBy is the cause of a run's cancellation by a user: the user.
type cancelledBy string

func (u cancelledBy) Error() string { return "cancelled by " + string(u) }

// execute runs the agent on ctx, stops its MCP servers, records how the run
// ended, and publishes that as the run's last event. A run that fails after
// a user cancelled it ended as cancelled.
func (s *Server) execute(ctx context.Context, a *agent.Agent, run *agent.Run, hub *hub, input string) {
	defer func() {
		s.mu.Lock()
		delete(s.runs, run.ID())
		s.mu.Unlock()
	}()
	redact := s.cfg.Resolved.Redactor
	res, runErr := run.Execute(ctx, input)
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

func (s *Server) listRuns(c *gin.Context) {
	f := store.RunFilter{
		Harness: c.Query("harness"),
		Status:  store.RunStatus(c.Query("status")),
		Before:  c.Query("before"),
		Limit:   api.DefaultRunsLimit,
	}
	switch f.Status {
	case "", store.RunRunning, store.RunSucceeded, store.RunFailed, store.RunCancelled:
	default:
		s.fail(c, http.StatusBadRequest, fmt.Errorf("status %q: must be running, succeeded, failed or cancelled", f.Status))
		return
	}
	if l := c.Query("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 1 || n > api.MaxRunsLimit {
			s.fail(c, http.StatusBadRequest, fmt.Errorf("limit %q: must be a number from 1 to %d", l, api.MaxRunsLimit))
			return
		}
		f.Limit = n
	}
	runs, err := s.cfg.Store.Runs(c.Request.Context(), c.Param("workspace"), f)
	if err != nil {
		s.failStore(c, err)
		return
	}
	out := api.RunList{Runs: make([]api.Run, len(runs))}
	for i, r := range runs {
		out.Runs[i] = apiRun(r)
	}
	// A full page may be the last: the next one is then empty.
	if len(runs) == f.Limit {
		out.Next = runs[len(runs)-1].ID
	}
	c.JSON(http.StatusOK, out)
}

// cancelRun cancels a running run of this server. The run ends as soon as
// what it is doing stops, and is recorded as cancelled by the user; the
// response comes before that, so the run's events tell when it ended.
func (s *Server) cancelRun(c *gin.Context) {
	ws, id := c.Param("workspace"), c.Param("id")
	if h := s.hub(ws, id); h != nil {
		h.cancel(cancelledBy(c.GetString(userKey)))
		c.Status(http.StatusAccepted)
		return
	}
	run, err := s.cfg.Store.Run(c.Request.Context(), ws, id)
	switch {
	case err != nil:
		s.failStore(c, err)
	case run.Status == store.RunRunning:
		s.fail(c, http.StatusConflict, fmt.Errorf("run %s is running on another server", id))
	default:
		s.fail(c, http.StatusConflict, fmt.Errorf("run %s has already finished", id))
	}
}

// listApprovals returns the approval requests the workspace's runs are
// waiting for, oldest first.
func (s *Server) listApprovals(c *gin.Context) {
	ws := c.Param("workspace")
	s.mu.Lock()
	var hubs []*hub
	for _, h := range s.runs {
		if h.workspace == ws {
			hubs = append(hubs, h)
		}
	}
	s.mu.Unlock()
	out := []api.ApprovalRequest{}
	for _, h := range hubs {
		out = append(out, h.waiting()...)
	}
	slices.SortFunc(out, func(a, b api.ApprovalRequest) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), strings.Compare(a.ID, b.ID))
	})
	c.JSON(http.StatusOK, out)
}

func (s *Server) getRun(c *gin.Context) {
	run, err := s.cfg.Store.Run(c.Request.Context(), c.Param("workspace"), c.Param("id"))
	if err != nil {
		s.failStore(c, err)
		return
	}
	c.JSON(http.StatusOK, apiRun(run))
}

func apiRun(r store.Run) api.Run {
	return api.Run{
		ID:               r.ID,
		HarnessVersionID: r.HarnessVersionID,
		Harness:          r.Harness,
		HarnessVersion:   r.HarnessVersion,
		StartedBy:        r.StartedBy,
		Input:            r.Input,
		Status:           string(r.Status),
		Output:           r.Output,
		Steps:            r.Steps,
		Error:            r.Error,
		CreatedAt:        r.CreatedAt,
		FinishedAt:       r.FinishedAt,
	}
}

func (s *Server) getAudit(c *gin.Context) {
	id := c.Param("id")
	if _, err := s.cfg.Store.Run(c.Request.Context(), c.Param("workspace"), id); err != nil {
		s.failStore(c, err)
		return
	}
	records, err := s.cfg.Store.AuditRecords(c.Request.Context(), id)
	if err != nil {
		s.failStore(c, err)
		return
	}
	out := make([]api.AuditRecord, len(records))
	for i, r := range records {
		out[i] = api.AuditRecord{Record: r.Record, RecordedAt: r.RecordedAt}
	}
	c.JSON(http.StatusOK, out)
}

func (s *Server) getTranscript(c *gin.Context) {
	id := c.Param("id")
	if _, err := s.cfg.Store.Run(c.Request.Context(), c.Param("workspace"), id); err != nil {
		s.failStore(c, err)
		return
	}
	messages, err := s.cfg.Store.Transcript(c.Request.Context(), id)
	if err != nil {
		s.failStore(c, err)
		return
	}
	out := make([]api.TranscriptMessage, len(messages))
	for i, m := range messages {
		out[i] = api.TranscriptMessage{Position: m.Position, Message: m.Message, CreatedAt: m.CreatedAt}
	}
	c.JSON(http.StatusOK, out)
}

// streamEvents sends a run's events from the start. A running run's stream
// follows it until it finishes; a finished run's stream replays its audit
// records and its end from the store.
func (s *Server) streamEvents(c *gin.Context) {
	id := c.Param("id")
	h := s.hub(c.Param("workspace"), id)
	if h == nil {
		s.replayEvents(c, id)
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	sent := 0
	for {
		events, changed, finished := h.since(sent)
		for _, e := range events {
			c.SSEvent(e.name, e.data)
		}
		sent += len(events)
		c.Writer.Flush()
		if finished {
			return
		}
		select {
		case <-changed:
		case <-c.Request.Context().Done():
			return
		case <-s.ctx.Done():
			// The run ends too, promptly; wait for its last event rather
			// than the client.
			<-changed
		}
	}
}

func (s *Server) replayEvents(c *gin.Context, id string) {
	ctx := c.Request.Context()
	run, err := s.cfg.Store.Run(ctx, c.Param("workspace"), id)
	if err != nil {
		s.failStore(c, err)
		return
	}
	if run.Status == store.RunRunning {
		// Every running run of this server has a hub, and New fails those of
		// earlier servers, so this is a run of another server sharing the
		// database.
		s.fail(c, http.StatusConflict, fmt.Errorf("run %s is running on another server", id))
		return
	}
	records, err := s.cfg.Store.AuditRecords(ctx, id)
	if err != nil {
		s.failStore(c, err)
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	for _, r := range records {
		c.SSEvent(api.EventAudit, r.Record)
	}
	c.SSEvent(api.EventFinished, apiRun(run))
	c.Writer.Flush()
}

func (s *Server) answerApproval(c *gin.Context) {
	var answer api.Answer
	if err := decode(c, &answer); err != nil {
		s.failDecode(c, err)
		return
	}
	h := s.hub(c.Param("workspace"), c.Param("id"))
	reason := answer.Reason
	if reason == "" {
		reason = "rejected through the API"
		if answer.Approved {
			reason = "approved through the API"
		}
	}
	if h == nil || !h.answer(c.Param("approval"), toolgateway.Approval{Approved: answer.Approved, Approver: c.GetString(userKey), Reason: reason}) {
		s.fail(c, http.StatusNotFound, fmt.Errorf("run %s is not waiting for approval %s", c.Param("id"), c.Param("approval")))
		return
	}
	c.Status(http.StatusNoContent)
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
