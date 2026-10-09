package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jangraefen/agenty/internal/api"
	"github.com/jangraefen/agenty/internal/auditlog"
	"github.com/jangraefen/agenty/internal/policy"
	"github.com/jangraefen/agenty/internal/store"
)

// handlers implements the API's routes, as generated from its spec. Sign-in
// and workspace membership are checked before any of them runs.
//
// It embeds the Server rather than being one, so the route methods, whose
// names the spec's operation IDs fix, stay apart from the Server's own API.
// Each handler decodes its request, checks it, calls the store or the run
// lifecycle in run.go, and maps the result to package api's types.
type handlers struct {
	*Server
}

// handlers must implement every route of the spec; a route added to it
// fails to compile here until it is.
var _ ServerInterface = handlers{}

// GetMe names the signed-in user, the workspaces they are a member of, and
// whether they are an auditor; the web frontend checks a token with it.
func (s handlers) GetMe(c *gin.Context) {
	user := c.GetString(userKey)
	c.JSON(http.StatusOK, api.Me{User: user, Workspaces: s.memberships(user), Auditor: s.cfg.Operator.Users[user].Auditor})
}

// GetWorkspace returns the workspace with its members, sorted; only its
// members reach it.
func (s handlers) GetWorkspace(c *gin.Context, workspace string) {
	members := slices.Sorted(slices.Values(s.cfg.Operator.Workspaces[workspace].Members))
	c.JSON(http.StatusOK, api.WorkspaceDetail{Name: workspace, Members: members})
}

// memberships are the workspaces user is a member of, sorted by name.
func (s handlers) memberships(user string) []string {
	workspaces := []string{}
	for _, name := range slices.Sorted(maps.Keys(s.cfg.Operator.Workspaces)) {
		if slices.Contains(s.cfg.Operator.Workspaces[name].Members, user) {
			workspaces = append(workspaces, name)
		}
	}
	return workspaces
}

// ListConversations lists the conversations the user started, in every
// workspace: those of a workspace they left are read-only, not hidden.
func (s handlers) ListConversations(c *gin.Context, params api.ListConversationsParams) {
	limit, ok := s.pageLimit(c, params.Limit)
	if !ok {
		return
	}
	user := c.GetString(userKey)
	filter := store.ConversationFilter{User: user, Limit: limit}
	if params.Before != "" {
		var err error
		if filter.BeforeAt, filter.BeforeID, err = parseConversationCursor(params.Before); err != nil {
			s.fail(c, http.StatusBadRequest, err)
			return
		}
	}
	list, err := s.cfg.Store.Conversations(c.Request.Context(), filter)
	if err != nil {
		s.failStore(c, err)
		return
	}
	out := api.ConversationList{Conversations: make([]api.ConversationSummary, len(list))}
	for i, cv := range list {
		out.Conversations[i] = apiConversation(cv)
	}
	// A full page may be the last: the next one is then empty.
	if len(list) == limit {
		out.Next = conversationCursor(list[len(list)-1])
	}
	c.JSON(http.StatusOK, out)
}

// GetConversation finds a conversation the user started, in any workspace,
// those they left included, with its runs, oldest first. Anyone else's is
// not found.
func (s handlers) GetConversation(c *gin.Context, id string) {
	cv, runs, err := s.cfg.Store.OwnConversation(c.Request.Context(), c.GetString(userKey), id)
	if err != nil {
		s.failStore(c, err)
		return
	}
	out := api.Conversation{ID: cv.ID, Workspace: cv.Workspace, Harness: cv.Harness, Title: cv.Title, Status: api.RunStatus(cv.Status), Runs: make([]api.Run, len(runs))}
	for i, r := range runs {
		out.Runs[i] = apiRun(r)
	}
	c.JSON(http.StatusOK, out)
}

// apiConversation is a stored conversation summary as the API shows it.
func apiConversation(cv store.ConversationSummary) api.ConversationSummary {
	return api.ConversationSummary{ID: cv.ID, Workspace: cv.Workspace, Harness: cv.Harness, Title: cv.Title, Status: api.RunStatus(cv.Status)}
}

// conversationCursor names the conversation a page of them ends with, for
// the next page: its latest activity, in microseconds as stored, and its ID.
// The ID breaks ties between conversations active in the same microsecond,
// so a page never skips or repeats one.
func conversationCursor(cv store.ConversationSummary) string {
	return strconv.FormatInt(cv.UpdatedAt.UnixMicro(), 10) + "." + cv.ID
}

// parseConversationCursor reads a cursor conversationCursor made, and fails
// for anything else.
func parseConversationCursor(cursor string) (time.Time, string, error) {
	micros, id, ok := strings.Cut(cursor, ".")
	at, err := strconv.ParseInt(micros, 10, 64)
	if !ok || err != nil || id == "" {
		return time.Time{}, "", fmt.Errorf("before %q: not a cursor of this API", cursor)
	}
	return time.UnixMicro(at), id, nil
}

// pageLimit is the page size a list's limit parameter asks for, the default
// when it is not given. It answers a size out of range itself, and is then
// not ok.
func (s handlers) pageLimit(c *gin.Context, limit int) (int, bool) {
	if _, set := c.GetQuery("limit"); !set {
		return api.DefaultPageLimit, true
	}
	if limit < 1 || limit > api.MaxPageLimit {
		s.fail(c, http.StatusBadRequest, fmt.Errorf("limit %d: must be from 1 to %d", limit, api.MaxPageLimit))
		return 0, false
	}
	return limit, true
}

// PutHarness stores a harness in the workspace, as a new immutable version
// if it changed; the store records harness.changed with it. The harness is
// validated, and its policy compiled, before it is stored, so a broken
// harness is refused here rather than failing its first run.
func (s handlers) PutHarness(c *gin.Context, workspace, name string) {
	var body api.Harness
	if err := decode(c, &body); err != nil {
		s.failDecode(c, err)
		return
	}
	h := body.ToHarness()
	if h.Name != name {
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
	v, err := s.cfg.Store.PutHarness(c.Request.Context(), workspace, c.GetString(userKey), h)
	if err != nil {
		s.failStore(c, err)
		return
	}
	c.JSON(http.StatusOK, harnessVersion(v))
}

// ListHarnesses lists the latest version of every harness in the workspace.
func (s handlers) ListHarnesses(c *gin.Context, workspace string) {
	versions, err := s.cfg.Store.Harnesses(c.Request.Context(), workspace)
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

// GetHarness returns the latest version of a harness in the workspace.
func (s handlers) GetHarness(c *gin.Context, workspace, name string) {
	v, err := s.cfg.Store.Harness(c.Request.Context(), workspace, name)
	if err != nil {
		s.failStore(c, err)
		return
	}
	c.JSON(http.StatusOK, harnessVersion(v))
}

// harnessVersion is a stored harness version as the API shows it.
func harnessVersion(v store.HarnessVersion) api.HarnessVersion {
	return api.HarnessVersion{Version: v.Version, Harness: api.FromHarness(v.Harness), CreatedAt: v.CreatedAt}
}

// CreateRun starts a new conversation: it queues a run of the latest
// version of the named harness, which the conversation then keeps, and
// answers with the queued run before any worker takes it up.
func (s handlers) CreateRun(c *gin.Context, workspace string) {
	var req api.CreateRun
	if err := decode(c, &req); err != nil {
		s.failDecode(c, err)
		return
	}
	if req.Input == "" {
		s.fail(c, http.StatusBadRequest, errors.New("input is required"))
		return
	}
	v, err := s.cfg.Store.Harness(c.Request.Context(), workspace, req.Harness)
	if err != nil {
		s.failStore(c, err)
		return
	}
	s.startRun(c, newRun{version: v, input: req.Input, user: c.GetString(userKey)})
}

// startRun queues r and responds with the queued run.
func (s handlers) startRun(c *gin.Context, r newRun) {
	run, status, err := s.enqueue(r)
	if err != nil {
		s.fail(c, status, fmt.Errorf("cannot start run: %w", err))
		return
	}
	c.JSON(http.StatusCreated, apiRun(run))
}

// ownRun finds the run with the given ID in workspace if the user started
// its conversation, and answers not found itself otherwise, as for a run
// that does not exist. Every handler of a run calls it first, before it
// reads the request or touches the run, so another user's run gives nothing
// away. A user who is not a member reaches it only to read their own run,
// and is told what any non-member is: that the workspace is not found.
func (s handlers) ownRun(c *gin.Context, workspace, id string) (store.Run, bool) {
	user := c.GetString(userKey)
	run, err := s.cfg.Store.OwnRun(c.Request.Context(), workspace, user, id)
	// member let the reads of a run through for non-members; one who finds
	// no own run is answered as member would have, so they cannot tell a
	// workspace that exists from one that does not.
	switch {
	case errors.Is(err, store.ErrNotFound) && !s.isMember(user, workspace):
		s.fail(c, http.StatusNotFound, fmt.Errorf("workspace %s: not found", workspace))
	case err != nil:
		s.failStore(c, err)
	default:
		return run, true
	}
	return store.Run{}, false
}

// FollowUpRun starts a run that continues the conversation of a run, the
// conversation's latest, which must have finished; one that failed or was
// cancelled is continued from where it stopped, see ended. The new run runs the
// harness version of the run it follows, so a conversation keeps the version
// it started with; central policy applies as the server has it now. The
// model sees the conversation as stored, redacted again with the secrets
// known now.
func (s handlers) FollowUpRun(c *gin.Context, workspace, id string) {
	if _, ok := s.ownRun(c, workspace, id); !ok {
		return
	}
	var req api.FollowUp
	if err := decode(c, &req); err != nil {
		s.failDecode(c, err)
		return
	}
	if req.Input == "" {
		s.fail(c, http.StatusBadRequest, errors.New("input is required"))
		return
	}
	// Only the conversation's latest run, once finished, is followed up, so
	// a conversation never branches and never has two runs at once. The store
	// refuses a second follow-up of the same run too, for two requests that
	// both pass this check.
	ctx := c.Request.Context()
	runs, err := s.cfg.Store.Conversation(ctx, workspace, id)
	if err != nil {
		s.failStore(c, err)
		return
	}
	last := runs[len(runs)-1]
	switch {
	case last.ID != id:
		s.fail(c, http.StatusConflict, fmt.Errorf("run %s is already followed up; follow up run %s, the conversation's latest", id, last.ID))
		return
	case !last.Finished():
		s.fail(c, http.StatusConflict, fmt.Errorf("run %s has not finished: follow it up once it has", id))
		return
	}
	// The version the run followed, not the harness's latest: a builder's
	// change, a grant taken away included, applies to new conversations only.
	v, err := s.cfg.Store.HarnessVersionByID(ctx, last.HarnessVersionID)
	if err != nil {
		s.failStore(c, err)
		return
	}
	s.startRun(c, newRun{version: v, input: req.Input, user: c.GetString(userKey), follows: last.ID})
}

// GetRun returns one of the user's own runs as stored.
func (s handlers) GetRun(c *gin.Context, workspace, id string) {
	if run, ok := s.ownRun(c, workspace, id); ok {
		c.JSON(http.StatusOK, apiRun(run))
	}
}

// apiRun is a stored run as the API shows it to its owner, input and output
// included; auditors get apiAuditRun instead. An unfinished run has the zero
// FinishedAt.
func apiRun(r store.Run) api.Run {
	out := api.Run{
		ID:             r.ID,
		Harness:        r.Harness,
		HarnessVersion: r.HarnessVersion,
		StartedBy:      r.StartedBy,
		Input:          r.Input,
		Status:         api.RunStatus(r.Status),
		Output:         r.Output,
		Steps:          r.Steps,
		Usage:          api.FromUsage(r.Usage),
		Error:          r.Error,
		CreatedAt:      r.CreatedAt,
		ConversationID: r.ConversationID,
		Follows:        r.Follows,
	}
	if r.FinishedAt != nil {
		out.FinishedAt = *r.FinishedAt
	}
	return out
}

// CancelRun cancels a queued, waiting or running run of this server. A queued
// or waiting run ends at once, a running one as soon as what it is doing
// stops; either is recorded as cancelled by the user. The response may come
// before that, so the run's events tell when it ended.
func (s handlers) CancelRun(c *gin.Context, workspace, id string) {
	h := s.hub(workspace, id) // before the run: see StreamRunEvents
	run, ok := s.ownRun(c, workspace, id)
	if !ok {
		return
	}
	if h != nil && !run.Finished() {
		// A cancel stops an agent: it goes ahead even when the log cannot
		// record the request. How the run ended, and by whom, is recorded
		// as it ends.
		request := auditlog.Event{Actor: c.GetString(userKey), Action: "run.cancel_requested", Workspace: workspace, RunID: id, Details: json.RawMessage("{}")}
		if _, err := s.cfg.Store.AppendEvent(c.Request.Context(), request); err != nil {
			s.cfg.Logger.Error("cannot record a cancel request", "run_id", id, "error", err)
		}
		by := cancelledBy(c.GetString(userKey))
		// A worker that claims the run from now on finds it cancelled.
		// Cancelling the hub's context also stops a running run's agent;
		// cancelIdle then ends a queued or waiting one in the store, which
		// no worker holds. A run claimed in between is left to its worker,
		// which finds its context cancelled with this cause.
		h.cancel(by)
		s.cancelIdle(c.Request.Context(), h, id, by)
		c.Status(http.StatusAccepted)
		return
	}
	if !run.Finished() {
		s.fail(c, http.StatusConflict, errNotRunHere(id))
		return
	}
	s.fail(c, http.StatusConflict, fmt.Errorf("run %s has already finished", id))
}

// errNotRunHere is the error for a run that has not finished, though this
// server does not run it: one serves a database, and stores a run's end
// before it lets go of the run, so the run's end could not be stored.
func errNotRunHere(id string) error {
	return fmt.Errorf("run %s has not finished, but this server does not run it", id)
}

// ListApprovals returns the approval requests the user's own runs in the
// workspace are waiting for, oldest first.
func (s handlers) ListApprovals(c *gin.Context, workspace string) {
	pending, err := s.cfg.Store.PendingApprovals(c.Request.Context(), workspace, c.GetString(userKey))
	if err != nil {
		s.failStore(c, err)
		return
	}
	out := make([]api.ApprovalRequest, len(pending))
	for i, a := range pending {
		out[i] = apiApproval(a)
	}
	c.JSON(http.StatusOK, out)
}

// apiApproval is a stored approval request as the API shows it.
func apiApproval(a store.Approval) api.ApprovalRequest {
	return api.ApprovalRequest{
		ID: a.ID, RunID: a.RunID, Harness: a.Harness, Tool: a.Tool, Args: a.Args, Reasons: a.Reasons,
		CreatedAt: a.CreatedAt, ExpiresAt: a.ExpiresAt,
	}
}

// GetRunTranscript returns one of the user's own runs' messages, as stored:
// redacted, and without the provider forms, which are for the model alone.
func (s handlers) GetRunTranscript(c *gin.Context, workspace, id string) {
	if _, ok := s.ownRun(c, workspace, id); !ok {
		return
	}
	messages, err := s.cfg.Store.Transcript(c.Request.Context(), id)
	if err != nil {
		s.failStore(c, err)
		return
	}
	out := make([]api.TranscriptMessage, len(messages))
	for i, m := range messages {
		out[i] = api.FromMessage(m.Position, m.Message)
	}
	c.JSON(http.StatusOK, out)
}

// StreamRunEvents sends a run's events from the start. A running run's
// stream follows it until it finishes; a finished run's stream replays its
// audit records and its end from the store.
func (s handlers) StreamRunEvents(c *gin.Context, workspace, id string) {
	// The hub is looked up before the run is read: a run's end is stored
	// before its hub is unregistered, so a run without a hub is read as
	// finished.
	h := s.hub(workspace, id)
	run, ok := s.ownRun(c, workspace, id)
	if !ok {
		return
	}
	if h == nil {
		s.replayEvents(c, run)
		return
	}
	// Every event from the first, then each as it is published: the hub
	// keeps them all, so the client misses nothing whenever it connects.
	// Each round flushes, so an event reaches the client as it happens.
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

// replayEvents sends the stream of a run without a hub from the store: its
// audit records, then its end. A run without a hub that has not finished is
// one this server does not run, which is a conflict, not an empty stream.
// A finished run's approval requests are not replayed: they were answered,
// and its audit records hold the answers.
func (s handlers) replayEvents(c *gin.Context, run store.Run) {
	ctx := c.Request.Context()
	id := run.ID
	if !run.Finished() {
		s.fail(c, http.StatusConflict, errNotRunHere(id))
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
		c.SSEvent(api.EventAudit, api.FromRecord(r.Record, r.RecordedAt))
	}
	c.SSEvent(api.EventFinished, apiRun(run))
	c.Writer.Flush()
}

// AnswerApproval answers a request the user's own run waits for, which
// queues the run to resume at the call: no one else answers it. A request is
// answered once, and not once it expired.
func (s handlers) AnswerApproval(c *gin.Context, workspace, id, approval string) {
	if _, ok := s.ownRun(c, workspace, id); !ok {
		return
	}
	var answer api.Answer
	if err := decode(c, &answer); err != nil {
		s.failDecode(c, err)
		return
	}
	reason := answer.Reason
	if reason == "" {
		reason = "rejected through the API"
		if answer.Approved {
			reason = "approved through the API"
		}
	}
	// The store answers only a pending request that has not expired, and
	// queues the run in the same transaction; the signal then wakes a
	// worker to resume it. The signed-in user is recorded as the approver.
	answered, err := s.cfg.Store.AnswerApproval(c.Request.Context(), workspace, id, approval, store.Answer{Approved: answer.Approved, Approver: c.GetString(userKey), Reason: reason})
	switch {
	case err != nil:
		s.failStore(c, err)
	case !answered:
		s.fail(c, http.StatusNotFound, fmt.Errorf("run %s is not waiting for approval %s", id, approval))
	default:
		s.signal()
		c.Status(http.StatusNoContent)
	}
}
