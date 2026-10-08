package server

import (
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
	"github.com/jangraefen/agenty/internal/policy"
	"github.com/jangraefen/agenty/internal/store"
)

// handlers implements the API's routes, as generated from its spec. Sign-in
// and workspace membership are checked before any of them runs.
type handlers struct {
	*Server
}

var _ ServerInterface = handlers{}

func (s handlers) GetMe(c *gin.Context) {
	user := c.GetString(userKey)
	c.JSON(http.StatusOK, api.Me{User: user, Workspaces: s.memberships(user)})
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

// ListConversations lists the conversations the user started in the
// workspaces they are a member of. Its route is in no workspace, so the
// membership middleware leaves it alone and the workspaces are checked here.
func (s handlers) ListConversations(c *gin.Context, params api.ListConversationsParams) {
	limit, ok := s.pageLimit(c, params.Limit)
	if !ok {
		return
	}
	user := c.GetString(userKey)
	filter := store.ConversationFilter{User: user, Workspaces: s.memberships(user), Limit: limit}
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

// GetConversation finds a conversation in one of the user's workspaces.
func (s handlers) GetConversation(c *gin.Context, id string) {
	cv, err := s.cfg.Store.FindConversation(c.Request.Context(), s.memberships(c.GetString(userKey)), id)
	if err != nil {
		s.failStore(c, err)
		return
	}
	c.JSON(http.StatusOK, apiConversation(cv))
}

func apiConversation(cv store.ConversationSummary) api.ConversationSummary {
	return api.ConversationSummary{ID: cv.ID, Workspace: cv.Workspace, Harness: cv.Harness, Title: cv.Title}
}

// conversationCursor names the conversation a page of them ends with, for
// the next page: its latest activity, in microseconds as stored, and its ID.
// Run IDs hold no dot.
func conversationCursor(cv store.ConversationSummary) string {
	return strconv.FormatInt(cv.UpdatedAt.UnixMicro(), 10) + "." + cv.ID
}

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
		return api.DefaultRunsLimit, true
	}
	if limit < 1 || limit > api.MaxRunsLimit {
		s.fail(c, http.StatusBadRequest, fmt.Errorf("limit %d: must be from 1 to %d", limit, api.MaxRunsLimit))
		return 0, false
	}
	return limit, true
}

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
	v, err := s.cfg.Store.PutHarness(c.Request.Context(), workspace, h)
	if err != nil {
		s.failStore(c, err)
		return
	}
	c.JSON(http.StatusOK, harnessVersion(v))
}

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

func (s handlers) GetHarness(c *gin.Context, workspace, name string) {
	v, err := s.cfg.Store.Harness(c.Request.Context(), workspace, name)
	if err != nil {
		s.failStore(c, err)
		return
	}
	c.JSON(http.StatusOK, harnessVersion(v))
}

func harnessVersion(v store.HarnessVersion) api.HarnessVersion {
	return api.HarnessVersion{ID: v.ID, Version: v.Version, Harness: api.FromHarness(v.Harness), CreatedAt: v.CreatedAt}
}

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

// FollowUpRun starts a run that continues the conversation of a run, the
// conversation's latest, which must have finished; one that failed or was
// cancelled is continued from where it stopped, see ended. The new run runs the
// harness version of the run it follows, so a conversation keeps the version
// it started with; central policy applies as the server has it now. The
// model sees the conversation as stored, redacted again with the secrets
// known now.
func (s handlers) FollowUpRun(c *gin.Context, workspace, id string) {
	var req api.FollowUp
	if err := decode(c, &req); err != nil {
		s.failDecode(c, err)
		return
	}
	if req.Input == "" {
		s.fail(c, http.StatusBadRequest, errors.New("input is required"))
		return
	}
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
	v, err := s.cfg.Store.HarnessVersionByID(ctx, last.HarnessVersionID)
	if err != nil {
		s.failStore(c, err)
		return
	}
	s.startRun(c, newRun{version: v, input: req.Input, user: c.GetString(userKey), follows: last.ID})
}

// GetRunConversation lists the runs of the conversation a run belongs to,
// oldest first.
func (s handlers) GetRunConversation(c *gin.Context, workspace, id string) {
	runs, err := s.cfg.Store.Conversation(c.Request.Context(), workspace, id)
	if err != nil {
		s.failStore(c, err)
		return
	}
	out := make([]api.Run, len(runs))
	for i, r := range runs {
		out[i] = apiRun(r)
	}
	c.JSON(http.StatusOK, out)
}

func (s handlers) ListRuns(c *gin.Context, workspace string, params api.ListRunsParams) {
	if params.Status != "" && !params.Status.Valid() {
		s.fail(c, http.StatusBadRequest, fmt.Errorf("status %q: must be running, succeeded, failed or cancelled", params.Status))
		return
	}
	limit, ok := s.pageLimit(c, params.Limit)
	if !ok {
		return
	}
	runs, err := s.cfg.Store.Runs(c.Request.Context(), workspace, store.RunFilter{
		Harness: params.Harness,
		Status:  store.RunStatus(params.Status),
		Before:  params.Before,
		Limit:   limit,
	})
	if err != nil {
		s.failStore(c, err)
		return
	}
	out := api.RunList{Runs: make([]api.Run, len(runs))}
	for i, r := range runs {
		out.Runs[i] = apiRun(r)
	}
	// A full page may be the last: the next one is then empty.
	if len(runs) == limit {
		out.Next = runs[len(runs)-1].ID
	}
	c.JSON(http.StatusOK, out)
}

func (s handlers) GetRun(c *gin.Context, workspace, id string) {
	run, err := s.cfg.Store.Run(c.Request.Context(), workspace, id)
	if err != nil {
		s.failStore(c, err)
		return
	}
	c.JSON(http.StatusOK, apiRun(run))
}

func apiRun(r store.Run) api.Run {
	out := api.Run{
		ID:               r.ID,
		HarnessVersionID: r.HarnessVersionID,
		Harness:          r.Harness,
		HarnessVersion:   r.HarnessVersion,
		StartedBy:        r.StartedBy,
		Input:            r.Input,
		Status:           api.RunStatus(r.Status),
		Output:           r.Output,
		Steps:            r.Steps,
		Usage:            api.FromUsage(r.Usage),
		Error:            r.Error,
		CreatedAt:        r.CreatedAt,
		ConversationID:   r.ConversationID,
		Follows:          r.Follows,
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
	if h := s.hub(workspace, id); h != nil {
		by := cancelledBy(c.GetString(userKey))
		// A worker that claims the run from now on finds it cancelled.
		h.cancel(by)
		s.cancelIdle(c.Request.Context(), h, id, by)
		c.Status(http.StatusAccepted)
		return
	}
	run, err := s.cfg.Store.Run(c.Request.Context(), workspace, id)
	switch {
	case err != nil:
		s.failStore(c, err)
	case !run.Finished():
		s.fail(c, http.StatusConflict, fmt.Errorf("run %s is running on another server", id))
	default:
		s.fail(c, http.StatusConflict, fmt.Errorf("run %s has already finished", id))
	}
}

// ListApprovals returns the approval requests the workspace's runs are
// waiting for, oldest first.
func (s handlers) ListApprovals(c *gin.Context, workspace string) {
	pending, err := s.cfg.Store.PendingApprovals(c.Request.Context(), workspace)
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

func (s handlers) GetRunAudit(c *gin.Context, workspace, id string) {
	if _, err := s.cfg.Store.Run(c.Request.Context(), workspace, id); err != nil {
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
		out[i] = api.FromRecord(r.Record, r.RecordedAt)
	}
	c.JSON(http.StatusOK, out)
}

func (s handlers) GetRunTranscript(c *gin.Context, workspace, id string) {
	if _, err := s.cfg.Store.Run(c.Request.Context(), workspace, id); err != nil {
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
		out[i] = api.FromMessage(m.Position, m.Message, m.CreatedAt)
	}
	c.JSON(http.StatusOK, out)
}

// StreamRunEvents sends a run's events from the start. A running run's
// stream follows it until it finishes; a finished run's stream replays its
// audit records and its end from the store.
func (s handlers) StreamRunEvents(c *gin.Context, workspace, id string) {
	h := s.hub(workspace, id)
	if h == nil {
		s.replayEvents(c, workspace, id)
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

func (s handlers) replayEvents(c *gin.Context, workspace, id string) {
	ctx := c.Request.Context()
	run, err := s.cfg.Store.Run(ctx, workspace, id)
	if err != nil {
		s.failStore(c, err)
		return
	}
	if !run.Finished() {
		// Every queued or running run of this server has a hub, New fails
		// the running runs of earlier servers and takes up their queued
		// ones, so this is a run of another server sharing the database.
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
		c.SSEvent(api.EventAudit, api.FromRecord(r.Record, r.RecordedAt))
	}
	c.SSEvent(api.EventFinished, apiRun(run))
	c.Writer.Flush()
}

// AnswerApproval answers a request a run waits for, which queues the run to
// resume at the call. A request is answered once, and not once it expired.
func (s handlers) AnswerApproval(c *gin.Context, workspace, id, approval string) {
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
