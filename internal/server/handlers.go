package server

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jangraefen/agenty/internal/api"
	"github.com/jangraefen/agenty/internal/policy"
	"github.com/jangraefen/agenty/internal/store"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

// handlers implements the API's routes, as generated from its spec. Sign-in
// and workspace membership are checked before any of them runs.
type handlers struct {
	*Server
}

var _ ServerInterface = handlers{}

func (s handlers) GetMe(c *gin.Context) {
	user := c.GetString(userKey)
	workspaces := []string{}
	for _, name := range slices.Sorted(maps.Keys(s.cfg.Operator.Workspaces)) {
		if slices.Contains(s.cfg.Operator.Workspaces[name].Members, user) {
			workspaces = append(workspaces, name)
		}
	}
	c.JSON(http.StatusOK, api.Me{User: user, Workspaces: workspaces})
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
	s.startRun(c, workspace, newRun{version: v, input: req.Input, user: c.GetString(userKey)})
}

// startRun starts r and responds with the started run.
func (s handlers) startRun(c *gin.Context, workspace string, r newRun) {
	id, status, err := s.start(r)
	if err != nil {
		s.fail(c, status, fmt.Errorf("cannot start run: %w", err))
		return
	}
	run, err := s.cfg.Store.Run(c.Request.Context(), workspace, id)
	if err != nil {
		s.failStore(c, err)
		return
	}
	c.JSON(http.StatusCreated, apiRun(run))
}

// FollowUpRun starts a run that continues the conversation of a run, the
// conversation's latest, which must have finished; one that failed or was
// cancelled is continued from where it stopped, see ended. The new run runs the
// harness's latest version, so grants and rules taken away since apply to
// no conversation. The model sees the conversation as stored, redacted
// again with the secrets known now.
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
	case last.Status == store.RunRunning:
		s.fail(c, http.StatusConflict, fmt.Errorf("run %s is still running: follow it up once it has finished", id))
		return
	}
	prior := make([]priorRun, len(runs))
	for i, r := range runs {
		messages, err := s.cfg.Store.Transcript(ctx, r.ID)
		if err != nil {
			s.failStore(c, err)
			return
		}
		prior[i] = priorRun{digest: r.PromptDigest, historyDigest: r.HistoryDigest, messages: ended(r, messages)}
	}
	v, err := s.cfg.Store.Harness(ctx, workspace, last.Harness)
	if err != nil {
		s.failStore(c, err)
		return
	}
	s.startRun(c, workspace, newRun{version: v, input: req.Input, user: c.GetString(userKey), follows: last.ID, prior: prior})
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
	limit := api.DefaultRunsLimit
	if _, set := c.GetQuery("limit"); set {
		if params.Limit < 1 || params.Limit > api.MaxRunsLimit {
			s.fail(c, http.StatusBadRequest, fmt.Errorf("limit %d: must be from 1 to %d", params.Limit, api.MaxRunsLimit))
			return
		}
		limit = params.Limit
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

// CancelRun cancels a running run of this server. The run ends as soon as
// what it is doing stops, and is recorded as cancelled by the user; the
// response comes before that, so the run's events tell when it ended.
func (s handlers) CancelRun(c *gin.Context, workspace, id string) {
	if h := s.hub(workspace, id); h != nil {
		h.cancel(cancelledBy(c.GetString(userKey)))
		c.Status(http.StatusAccepted)
		return
	}
	run, err := s.cfg.Store.Run(c.Request.Context(), workspace, id)
	switch {
	case err != nil:
		s.failStore(c, err)
	case run.Status == store.RunRunning:
		s.fail(c, http.StatusConflict, fmt.Errorf("run %s is running on another server", id))
	default:
		s.fail(c, http.StatusConflict, fmt.Errorf("run %s has already finished", id))
	}
}

// ListApprovals returns the approval requests the workspace's runs are
// waiting for, oldest first.
func (s handlers) ListApprovals(c *gin.Context, workspace string) {
	s.mu.Lock()
	var hubs []*hub
	for _, h := range s.runs {
		if h.workspace == workspace {
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
		c.SSEvent(api.EventAudit, api.FromRecord(r.Record, r.RecordedAt))
	}
	c.SSEvent(api.EventFinished, apiRun(run))
	c.Writer.Flush()
}

func (s handlers) AnswerApproval(c *gin.Context, workspace, id, approval string) {
	var answer api.Answer
	if err := decode(c, &answer); err != nil {
		s.failDecode(c, err)
		return
	}
	h := s.hub(workspace, id)
	reason := answer.Reason
	if reason == "" {
		reason = "rejected through the API"
		if answer.Approved {
			reason = "approved through the API"
		}
	}
	if h == nil || !h.answer(approval, toolgateway.Approval{Approved: answer.Approved, Approver: c.GetString(userKey), Reason: reason}) {
		s.fail(c, http.StatusNotFound, fmt.Errorf("run %s is not waiting for approval %s", id, approval))
		return
	}
	c.Status(http.StatusNoContent)
}
