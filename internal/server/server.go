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
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"reflect"
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
	// runs holds the hubs of the queued and running runs of this server.
	runs map[string]*hub
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

// newRun is a run to queue.
type newRun struct {
	version store.HarnessVersion
	input   string
	// user is who starts the run.
	user string
	// follows, if set, is the ID of the run whose conversation the run
	// continues.
	follows string
}

// enqueue stores r as a queued run and wakes a worker for it. The run gets
// its hub first, so a stored queued run of this server always has an event
// stream. On error it also returns the status to respond with: 409 for a
// run that another run already follows, 503 when the server is stopping,
// 500 otherwise.
func (s *Server) enqueue(r newRun) (store.Run, int, error) {
	v := r.version
	id := rand.Text()
	h, err := s.register(id, v.Workspace, v.Harness.Name)
	if err != nil {
		return store.Run{}, http.StatusServiceUnavailable, err
	}
	run, err := s.cfg.Store.CreateRun(s.ctx, store.NewRun{ID: id, HarnessVersionID: v.ID, Input: r.input, StartedBy: r.user, Follows: r.follows})
	if err != nil {
		s.unregister(id)
		h.cancel(nil)
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrConflict) {
			// Another follow-up of the same run was stored first.
			status = http.StatusConflict
			err = fmt.Errorf("run %s is already followed up; follow up the conversation's latest run", r.follows)
		}
		return store.Run{}, status, err
	}
	s.signal()
	return run, 0, nil
}

// register gives the run id a hub, unless the server is stopping. Registering
// under the lock Close takes means no run is queued once Close has begun.
func (s *Server) register(id, workspace, harness string) (*hub, error) {
	h := newHub(workspace, harness)
	h.runID = id
	h.ctx, h.cancel = context.WithCancelCause(s.ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		h.cancel(nil)
		return nil, errClosed
	}
	s.runs[id] = h
	return h, nil
}

// unregister removes the hub of the run id.
func (s *Server) unregister(id string) {
	s.mu.Lock()
	delete(s.runs, id)
	s.mu.Unlock()
}

// signal wakes a worker to claim queued runs. A worker claims only when
// woken, so a signal is sent for every run queued; signals beyond one for
// each worker are dropped, as each woken worker claims until none is left.
func (s *Server) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// work waits for a signal, then claims queued runs and executes them, one
// at a time, until none is left, and waits again, until the server stops.
func (s *Server) work() {
	for {
		select {
		case <-s.wake:
		case <-s.ctx.Done():
			return
		}
		for s.ctx.Err() == nil {
			claimed, ok, err := s.cfg.Store.ClaimRun(s.ctx)
			if err != nil {
				if s.ctx.Err() == nil {
					s.cfg.Logger.Error("cannot claim a run", "error", err)
				}
				select {
				case <-time.After(claimRetry):
					continue
				case <-s.ctx.Done():
					return
				}
			}
			if !ok {
				break
			}
			s.take(claimed)
		}
	}
}

// claimRetry is how long a worker waits before it claims again after a
// claim failed.
const claimRetry = 5 * time.Second

// take executes a run a worker claimed, unless it was cancelled while it was
// queued.
func (s *Server) take(claimed store.ClaimedRun) {
	h := s.hub(claimed.Workspace, claimed.ID)
	if h == nil {
		// Every queued run of this server has a hub, and New gives one to
		// those an earlier server left queued.
		s.cfg.Logger.Error("a claimed run has no event stream", "run_id", claimed.ID)
		s.finish(context.Background(), nil, claimed.ID, store.RunFailed, agent.Result{}, "the server lost the run")
		return
	}
	p, err := s.prepare(h, claimed)
	if err != nil {
		// A run cancelled while it was queued, or while it was being
		// prepared, fails to prepare, as its context is cancelled.
		var by cancelledBy
		switch {
		case errors.As(context.Cause(h.ctx), &by):
			if err := s.closeOut(h.ctx, h, claimed.ID, by); err != nil {
				s.cfg.Logger.Error("cannot record the calls a cancelled run did not run", "run_id", claimed.ID, "error", err)
			}
			s.finish(h.ctx, h, claimed.ID, store.RunCancelled, agent.Result{}, by.Error())
		case s.ctx.Err() != nil:
			s.finish(h.ctx, h, claimed.ID, store.RunFailed, agent.Result{}, errStopped)
		default:
			s.finish(h.ctx, h, claimed.ID, store.RunFailed, agent.Result{}, s.cfg.Resolved.Redactor.String(err.Error()))
		}
		return
	}
	s.execute(h.ctx, h, p)
}

// errStopped is how a run ends that the server stopped before it finished.
const errStopped = "the server stopped before the run finished"

// prepared is a run ready to execute.
type prepared struct {
	agent *agent.Agent
	lease *toolgateway.Lease
	run   *agent.Run
	// conversation names the conversation the run belongs to, and history
	// is the conversation so far.
	conversation string
	history      []model.Message
	input        string
	// resume, if set, resumes the run at a call that waited for approval,
	// after own, the run's messages so far, instead of starting it on input.
	resume *agent.Resumption
	own    []model.Message
}

// prepare builds an agent for a claimed run's harness version, which starts
// the MCP servers it needs or takes those its conversation keeps, and the
// conversation so far. A run that does not go ahead gives the conversation
// back the servers it took.
func (s *Server) prepare(h *hub, claimed store.ClaimedRun) (prepared, error) {
	ctx := h.ctx
	if err := context.Cause(ctx); err != nil {
		return prepared{}, err
	}
	stored, err := s.cfg.Store.Run(ctx, claimed.Workspace, claimed.ID)
	if err != nil {
		return prepared{}, err
	}
	v, err := s.cfg.Store.HarnessVersionByID(ctx, stored.HarnessVersionID)
	if err != nil {
		return prepared{}, err
	}
	prior, err := s.prior(ctx, claimed.Workspace, stored)
	if err != nil {
		return prepared{}, err
	}
	hv := v.Harness
	m, err := s.cfg.NewModel(hv.Model)
	if err != nil {
		return prepared{}, fmt.Errorf("cannot start run: %w", err)
	}
	servers := make(map[string]toolgateway.ToolServer, len(s.cfg.Operator.MCPServers))
	for name, srv := range s.cfg.Operator.MCPServers {
		servers[name] = s.cfg.Server(name, mcptool.Server{Command: srv.Command, Args: srv.Args, Env: s.cfg.Resolved.MCPServerEnv[name]})
	}
	lease := s.pool.Lease(stored.ConversationID, servers)
	a, err := agent.New(s.ctx, agent.Config{
		Harness:    &hv,
		Model:      m,
		Servers:    lease.Servers(),
		Policy:     s.cfg.Operator.Policy,
		Approver:   suspend{},
		Audit:      runAudit{store: s.cfg.Store, hub: h},
		Redactor:   s.cfg.Resolved.Redactor,
		Transcript: runTranscript{store: s.cfg.Store, redact: s.cfg.Resolved.Redactor},
	})
	if err != nil {
		return prepared{}, errors.Join(fmt.Errorf("cannot start run: %w", err), lease.Return())
	}
	digest := a.PromptDigest()
	history := conversationHistory(s.cfg.Resolved.Redactor, prior, digest)
	if err := s.cfg.Store.SetRunDigests(ctx, stored.ID, digest, historyDigest(history)); err != nil {
		return prepared{}, errors.Join(err, a.Close(), lease.Return())
	}
	p := prepared{
		agent:        a,
		lease:        lease,
		conversation: stored.ConversationID,
		history:      history,
	}
	approval, waited, err := s.cfg.Store.LatestApproval(ctx, stored.ID)
	if err == nil && waited {
		err = s.resumption(ctx, &p, approval)
	}
	if err != nil {
		return prepared{}, errors.Join(err, a.Close(), lease.Return())
	}
	if !waited {
		p.run = a.StartAs(stored.ID)
		p.input = withLostState(stored.Input, lease.Fresh(), history)
	}
	return p, nil
}

// resumption prepares p to resume its run at the call that waited for the
// answered approval. The call runs only as it was asked for: if the
// transcript does not hold the call as the approver saw it, as redaction
// changed it, the answer is a rejection.
func (s *Server) resumption(ctx context.Context, p *prepared, approval store.Approval) error {
	if approval.Status == store.ApprovalPending || approval.Status == store.ApprovalWithdrawn {
		return fmt.Errorf("run %s was taken up while its approval request is %s", approval.RunID, approval.Status)
	}
	transcript, err := s.cfg.Store.Transcript(ctx, approval.RunID)
	if err != nil {
		return err
	}
	records, err := s.cfg.Store.AuditRecords(ctx, approval.RunID)
	if err != nil {
		return err
	}
	if used(records, approval.CallID) {
		return fmt.Errorf("run %s was taken up for an approval it used", approval.RunID)
	}
	altered := false
	for _, m := range transcript {
		p.own = append(p.own, m.Message)
		altered = altered || m.Altered
	}
	answer := toolgateway.Approval{Approved: approval.Status == store.ApprovalApproved, Approver: approval.Approver, Reason: approval.Reason}
	if !asked(p.own, approval) {
		answer = toolgateway.Approval{Approver: approval.Approver, Reason: "the call cannot run as it was asked for: the model's call is not stored as it was"}
	}
	if altered {
		// What the model saw differs from what is stored, so its replies
		// are sent without their provider forms, nor are those before them.
		p.own = callsAsReplies(p.own)
		p.history = callsAsReplies(p.history)
	}
	audit := make([]toolgateway.Record, len(records))
	for i, r := range records {
		audit[i] = r.Record
	}
	p.run = p.agent.Restore(approval.RunID, audit)
	p.resume = &agent.Resumption{
		CallID:  approval.CallID,
		Call:    approval.Call,
		Results: approval.Results,
		Answer:  answer,
		Note:    lostState(p.lease.Fresh(), append(slices.Clip(p.history), p.own...)),
	}
	return nil
}

// asked reports whether the last of own, a run's messages, makes the call
// approval was asked for, with the arguments the approver saw. A call waits
// only if redaction left its arguments as they were, so they are stored as
// the model made them.
func asked(own []model.Message, approval store.Approval) bool {
	if len(own) == 0 {
		return false
	}
	calls := own[len(own)-1].ToolCalls
	if approval.Call >= len(calls) {
		return false
	}
	c := calls[approval.Call]
	return c.Name == approval.Tool && jsonEqual(c.Args, approval.Args)
}

// jsonEqual reports whether a and b are the same JSON value.
func jsonEqual(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

// callsAsReplies drops the provider form of every message.
func callsAsReplies(messages []model.Message) []model.Message {
	out := slices.Clone(messages)
	for i := range out {
		out[i].Provider = nil
	}
	return out
}

// prior returns the earlier runs of the conversation run continues, as it
// sends them to the model; a run that failed or was cancelled is continued
// from where it stopped, see ended.
func (s *Server) prior(ctx context.Context, workspace string, run store.Run) ([]priorRun, error) {
	if run.Follows == "" {
		return nil, nil
	}
	runs, err := s.cfg.Store.Conversation(ctx, workspace, run.ID)
	if err != nil {
		return nil, err
	}
	runs = runs[:len(runs)-1]
	prior := make([]priorRun, len(runs))
	for i, r := range runs {
		messages, err := s.cfg.Store.Transcript(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		prior[i] = priorRun{digest: r.PromptDigest, historyDigest: r.HistoryDigest, messages: ended(r, messages)}
	}
	return prior, nil
}

// keep keeps the run's servers for its conversation, unless the run was
// cancelled: a call may still be running in a server then, which stopping
// the server ends.
func keep(ctx context.Context, lease *toolgateway.Lease, conversation string) error {
	if ctx.Err() != nil {
		return lease.Close()
	}
	return lease.Keep(conversation)
}

// cancelledBy is the cause of a run's cancellation by a user: the user.
type cancelledBy string

func (u cancelledBy) Error() string { return "cancelled by " + string(u) }

// execute runs the agent on ctx, after history, keeps its MCP servers for
// the conversation, and records how the run ended. The servers are kept
// before, so a follow-up the end allows finds them. A run that fails after a
// user cancelled it ended as cancelled.
func (s *Server) execute(ctx context.Context, h *hub, p prepared) {
	run := p.run
	var (
		res    agent.Result
		runErr error
	)
	if p.resume != nil {
		res, runErr = run.Resume(ctx, p.history, p.own, *p.resume)
	} else {
		res, runErr = run.Continue(ctx, p.history, p.input)
	}
	var suspended *agent.Suspended
	if errors.As(runErr, &suspended) && ctx.Err() == nil {
		// The run waits holding nothing: its servers are kept for the
		// conversation, as after a run that finished.
		if err := errors.Join(p.agent.Close(), keep(ctx, p.lease, p.conversation)); err != nil {
			s.cfg.Logger.Error("cannot keep a suspended run's tool servers", "run_id", run.ID(), "error", err)
		}
		s.suspend(ctx, h, res, suspended)
		return
	}
	err := errors.Join(runErr, p.agent.Close(), keep(ctx, p.lease, p.conversation))
	status, errMsg := store.RunSucceeded, ""
	var by cancelledBy
	switch {
	case runErr != nil && errors.As(context.Cause(ctx), &by):
		// The cause is what the run's record says; any other error, such as
		// a server that did not stop, is logged.
		status, errMsg = store.RunCancelled, by.Error()
		s.cfg.Logger.Info("run cancelled", "run_id", run.ID(), "error", err)
		if p.resume != nil {
			if err := s.closeOut(ctx, h, run.ID(), by); err != nil {
				s.cfg.Logger.Error("cannot record the calls a cancelled run did not run", "run_id", run.ID(), "error", err)
			}
		}
	case err != nil:
		status, errMsg = store.RunFailed, s.cfg.Resolved.Redactor.String(err.Error())
	}
	s.finish(ctx, h, run.ID(), status, res, errMsg)
}

// suspend records that the run waits for approval of the call suspended
// names, and publishes the request. The run then waits, holding no worker,
// until the request is answered or expires, either of which queues it. A run
// that cannot be suspended fails.
func (s *Server) suspend(ctx context.Context, h *hub, res agent.Result, suspended *agent.Suspended) {
	// As precise as PostgreSQL keeps it, so the event and the stored request
	// agree.
	now := time.Now().Truncate(time.Microsecond)
	// The gateway redacted them already; the store gets nothing it did not.
	results := slices.Clone(suspended.Results)
	for i := range results {
		results[i].Content = s.cfg.Resolved.Redactor.String(results[i].Content)
	}
	a := store.NewApproval{
		ID:        rand.Text(),
		RunID:     res.RunID,
		CallID:    suspended.CallID,
		Call:      suspended.Call,
		Results:   results,
		Tool:      suspended.Request.Tool,
		Args:      suspended.Request.Args,
		Reasons:   suspended.Reasons,
		CreatedAt: now,
		ExpiresAt: now.Add(s.approvalTimeout()),
	}
	if err := s.cfg.Store.SuspendRun(context.WithoutCancel(ctx), a); err != nil {
		s.finish(ctx, h, res.RunID, store.RunFailed, res, s.cfg.Resolved.Redactor.String(err.Error()))
		return
	}
	// A cancel that came as the run suspended found it still running.
	var by cancelledBy
	if errors.As(context.Cause(ctx), &by) {
		s.cancelIdle(ctx, h, res.RunID, by)
		return
	}
	select {
	case s.expiry <- struct{}{}:
	default:
	}
	s.cfg.Logger.Info("run waits for approval", "harness", h.harness, "run_id", res.RunID, "tool", a.Tool)
	h.publish(event{api.EventApproval, apiApproval(store.Approval{NewApproval: a, Harness: h.harness})})
}

// approvalTimeout is how long an approval request waits for an answer.
func (s *Server) approvalTimeout() time.Duration {
	return cmp.Or(s.cfg.Operator.Approvals.Timeout, config.DefaultApprovalTimeout)
}

// expire rejects approval requests nobody answered in time, which queues
// their runs, as each request expires, until the server stops.
func (s *Server) expire() {
	reason := "no answer within " + duration(s.approvalTimeout())
	for {
		n, err := s.cfg.Store.ExpireApprovals(s.ctx, reason)
		if err != nil && s.ctx.Err() == nil {
			s.cfg.Logger.Error("cannot expire approval requests", "error", err)
		}
		for range min(n, cap(s.wake)) {
			s.signal()
		}
		wait := claimRetry
		if err == nil {
			next, ok, err := s.cfg.Store.NextApprovalExpiry(s.ctx)
			switch {
			case err != nil && s.ctx.Err() == nil:
				s.cfg.Logger.Error("cannot read when approval requests expire", "error", err)
			case ok:
				wait = time.Until(next)
			default:
				wait = -1
			}
		}
		var timer <-chan time.Time
		if wait >= 0 {
			timer = time.After(wait)
		}
		select {
		case <-timer:
		case <-s.expiry:
		case <-s.ctx.Done():
			return
		}
	}
}

// replay publishes to a run's new hub what the run recorded so far: its
// audit records and, if it waits, its approval request.
func (s *Server) replay(ctx context.Context, h *hub, status store.RunStatus) error {
	records, err := s.cfg.Store.AuditRecords(ctx, h.runID)
	if err != nil {
		return err
	}
	for _, r := range records {
		h.publish(event{api.EventAudit, api.FromRecord(r.Record, r.RecordedAt)})
	}
	if status != store.RunWaiting {
		return nil
	}
	a, ok, err := s.cfg.Store.LatestApproval(ctx, h.runID)
	if err == nil && ok && a.Status == store.ApprovalPending {
		h.publish(event{api.EventApproval, apiApproval(a)})
	}
	return err
}

// closeOut completes the record of a run that was cancelled while it waited
// for approval, or to resume after one: the waiting call did not run, nor
// did the calls after it in the model's reply. The audit log records the
// call's approval as failed, and the transcript the results of the reply's
// calls, so the conversation can continue from it. A run that has no such
// call, as its waiting call was answered in the gateway, is left as it is.
func (s *Server) closeOut(ctx context.Context, h *hub, id string, by cancelledBy) error {
	ctx = context.WithoutCancel(ctx)
	a, ok, err := s.cfg.Store.LatestApproval(ctx, id)
	if err != nil || !ok {
		return err
	}
	transcript, err := s.cfg.Store.Transcript(ctx, id)
	if err != nil {
		return err
	}
	records, err := s.cfg.Store.AuditRecords(ctx, id)
	if err != nil {
		return err
	}
	own := make([]model.Message, len(transcript))
	for i, m := range transcript {
		own[i] = m.Message
	}
	if used(records, a.CallID) || !asked(own, a) {
		return nil
	}
	calls := own[len(own)-1].ToolCalls
	failed := "approval failed: " + by.Error()
	rec := toolgateway.Record{RunID: id, CallID: a.CallID, Event: toolgateway.EventApproval, Tool: a.Tool, Args: a.Args, Decision: toolgateway.Deny, Reason: failed}
	at, err := s.cfg.Store.RecordAt(ctx, rec)
	if err != nil {
		return err
	}
	h.publish(event{api.EventAudit, api.FromRecord(rec, at)})
	results := slices.Clone(a.Results)
	for i, c := range calls[a.Call:] {
		content := "Not run: the run was " + by.Error() + "."
		if i == 0 {
			content = fmt.Sprintf("%v: %s: %s", toolgateway.ErrDenied, c.Name, failed)
		}
		results = append(results, model.ToolResult{CallID: c.ID, Content: content, IsError: true})
	}
	return s.cfg.Store.AppendMessage(ctx, id, store.NewMessage{Position: len(transcript), Message: model.Message{Role: model.RoleUser, ToolResults: results}})
}

// used reports whether the call callID was answered in the gateway already,
// as its run resumed at it, so its approval is used.
func used(records []store.AuditRecord, callID string) bool {
	return slices.ContainsFunc(records, func(r store.AuditRecord) bool {
		return r.CallID == callID && r.Event == toolgateway.EventApproval
	})
}

// cancelIdle cancels the queued or waiting run id in the store, and if it was
// one, completes its record, publishes its end and unregisters its hub h.
func (s *Server) cancelIdle(ctx context.Context, h *hub, id string, by cancelledBy) {
	ctx = context.WithoutCancel(ctx)
	switch cancelled, err := s.cfg.Store.CancelIdleRun(ctx, id, by.Error()); {
	case err != nil:
		s.cfg.Logger.Error("cannot cancel a queued or waiting run in the store", "run_id", id, "error", err)
	case cancelled:
		if err := s.closeOut(ctx, h, id, by); err != nil {
			s.cfg.Logger.Error("cannot record the calls a cancelled run did not run", "run_id", id, "error", err)
		}
		s.publishEnd(ctx, h, store.Run{ID: id, Status: store.RunCancelled, Error: by.Error()})
		s.unregister(id)
	}
}

// cancelLeft cancels the queued or waiting run r, at a start, as its owner
// is no longer a member of its workspace, and records the calls it did not
// run, so the conversation can go on if they come back. It fails unless the
// run was cancelled: the server must not start with it still to be taken
// up. A failure to record the calls, once the run is cancelled, is not
// retried at the next start.
func (s *Server) cancelLeft(ctx context.Context, h *hub, r store.IdleRun) error {
	defer h.cancel(nil)
	defer s.unregister(r.ID)
	by := cancelledBy("the server, as " + r.Owner + " is no longer a member of " + r.Workspace)
	switch cancelled, err := s.cfg.Store.CancelIdleRun(ctx, r.ID, by.Error()); {
	case err != nil:
		return err
	case !cancelled:
		return fmt.Errorf("run %s: not queued or waiting, so not cancelled", r.ID)
	}
	return s.closeOut(ctx, h, r.ID, by)
}

// suspend is the approver of every run: it never waits for an answer, but
// suspends the run until one is given.
type suspend struct{}

var _ toolgateway.Approver = suspend{}

func (suspend) Approve(context.Context, toolgateway.Request, []string) (toolgateway.Approval, error) {
	return toolgateway.Approval{}, toolgateway.ErrSuspend
}

// finish records how the run id ended and publishes that as the last event
// of its hub, if it has one, which it then unregisters.
func (s *Server) finish(ctx context.Context, h *hub, id string, status store.RunStatus, res agent.Result, errMsg string) {
	redact := s.cfg.Resolved.Redactor
	// The run's context may be cancelled; how the run ended is recorded
	// regardless.
	ctx = context.WithoutCancel(ctx)
	if err := s.cfg.Store.FinishRun(ctx, id, status, redact.String(res.Output), res.Steps, errMsg); err != nil {
		s.cfg.Logger.Error("cannot record the end of a run", "run_id", id, "error", err)
	}
	if h == nil {
		return
	}
	defer s.unregister(id)
	s.cfg.Logger.Info("run finished", "harness", h.harness, "run_id", id, "status", status, "steps", res.Steps)
	s.publishEnd(ctx, h, store.Run{ID: id, Status: status, Output: redact.String(res.Output), Steps: res.Steps, Error: errMsg})
}

// publishEnd publishes the run as stored as the last event of its hub, or
// fallback if it cannot be read.
func (s *Server) publishEnd(ctx context.Context, h *hub, fallback store.Run) {
	finished, err := s.cfg.Store.Run(ctx, h.workspace, fallback.ID)
	if err != nil {
		s.cfg.Logger.Error("cannot read a finished run", "run_id", fallback.ID, "error", err)
		finished = fallback
	}
	h.publish(event{api.EventFinished, apiRun(finished)})
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
