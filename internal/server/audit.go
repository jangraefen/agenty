package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"

	"github.com/jangraefen/agenty/internal/api"
	"github.com/jangraefen/agenty/internal/auditlog"
	"github.com/jangraefen/agenty/internal/must"
	"github.com/jangraefen/agenty/internal/policy"
	"github.com/jangraefen/agenty/internal/store"
)

// errNotAuditor is the error of an audit route called by anyone but an
// auditor.
var errNotAuditor = errors.New("only auditors read the audit log")

// auditor answers forbidden unless the user is an auditor, and is then not
// ok. The audit routes are in no workspace, so the membership check leaves
// them alone: their workspace filter is a query parameter.
func (s handlers) auditor(c *gin.Context) bool {
	if !s.cfg.Operator.Users[c.GetString(userKey)].Auditor {
		s.fail(c, http.StatusForbidden, errNotAuditor)
		return false
	}
	return true
}

// ListAuditRuns lists the runs of every workspace for an auditor, newest
// first, filtered by workspace, harness, starter and status, and paged by
// the ID of the last run of the page before.
func (s handlers) ListAuditRuns(c *gin.Context, params api.ListAuditRunsParams) {
	if !s.auditor(c) {
		return
	}
	if params.Status != "" && !params.Status.Valid() {
		s.fail(c, http.StatusBadRequest, fmt.Errorf("status %q: not a run status", params.Status))
		return
	}
	limit, ok := s.pageLimit(c, params.Limit)
	if !ok {
		return
	}
	runs, err := s.cfg.Store.AuditRuns(c.Request.Context(), store.RunFilter{
		Workspace: params.Workspace,
		Harness:   params.Harness,
		StartedBy: params.StartedBy,
		Status:    store.RunStatus(params.Status),
		Before:    params.Before,
		Limit:     limit,
	})
	if err != nil {
		s.failStore(c, err)
		return
	}
	out := api.AuditRunList{Runs: make([]api.AuditRun, len(runs))}
	for i, r := range runs {
		out.Runs[i] = apiAuditRun(r)
	}
	// A full page may be the last: the next one is then empty.
	if len(runs) == limit {
		out.Next = runs[len(runs)-1].ID
	}
	c.JSON(http.StatusOK, out)
}

// GetAuditRun shows an auditor a run of any workspace with its audit records:
// the tool gateway's decisions, approvals and results, never the run's
// input, output or transcript.
func (s handlers) GetAuditRun(c *gin.Context, id string) {
	if !s.auditor(c) {
		return
	}
	ctx := c.Request.Context()
	run, err := s.cfg.Store.AuditRun(ctx, id)
	if err != nil {
		s.failStore(c, err)
		return
	}
	records, err := s.cfg.Store.AuditRecords(ctx, id)
	if err != nil {
		s.failStore(c, err)
		return
	}
	out := api.AuditRunDetail{Run: apiAuditRun(run), Records: make([]api.AuditRecord, len(records))}
	for i, r := range records {
		out.Records[i] = api.FromRecord(r.Record, r.RecordedAt)
	}
	c.JSON(http.StatusOK, out)
}

// apiAuditRun is a run as auditors see it: what happened, without its input
// and output.
func apiAuditRun(r store.AuditRun) api.AuditRun {
	out := api.AuditRun{
		ID:             r.ID,
		Follows:        r.Follows,
		Workspace:      r.Workspace,
		Harness:        r.Harness,
		HarnessVersion: r.HarnessVersion,
		StartedBy:      r.StartedBy,
		Status:         api.RunStatus(r.Status),
		Steps:          r.Steps,
		Usage:          api.FromUsage(r.Usage),
		Error:          r.Error,
		CreatedAt:      r.CreatedAt,
	}
	if r.FinishedAt != nil {
		out.FinishedAt = *r.FinishedAt
	}
	return out
}

// exportPage is how many events an export reads at a time: the log is read
// as a stream, never whole.
const exportPage = 500

// exportComplete is the trailer an export ends with once its last event is
// written: an export cut short by an error lacks it.
const exportComplete = "Audit-Export-Complete"

// ExportAuditLog writes the audit log after the given event to an auditor,
// as JSON lines.
func (s handlers) ExportAuditLog(c *gin.Context, params api.ExportAuditLogParams) {
	if !s.auditor(c) {
		return
	}
	if params.After < 0 {
		s.fail(c, http.StatusBadRequest, fmt.Errorf("after %d: must not be negative", params.After))
		return
	}
	ctx := c.Request.Context()
	// The log as it is now: events appended while it is written are left
	// for the next export, so the export ends.
	last, err := s.cfg.Store.LastAuditEventID(ctx)
	if err != nil {
		s.failStore(c, err)
		return
	}
	// The status is sent before the first event, so an error from here on
	// cannot change it; the missing trailer is what tells the client the
	// export is cut short. The trailer is declared before the header is
	// written, as net/http needs it to be, and set only once the last page
	// is written.
	c.Header("Content-Type", "application/jsonl")
	c.Header("Trailer", exportComplete)
	c.Status(http.StatusOK)
	for after := params.After; ; {
		page, err := s.cfg.Store.AuditEvents(ctx, after, last, exportPage)
		if err != nil {
			s.cfg.Logger.Error("cannot export the audit log", "error", err)
			return
		}
		if len(page) == 0 {
			c.Writer.Header().Set(exportComplete, "true")
			return
		}
		for _, e := range page {
			if err := auditlog.WriteLine(c.Writer, e); err != nil {
				return
			}
			after = e.ID
		}
		c.Writer.Flush()
	}
}

// serverStarted is the event of a server's start: the central policy in
// force, as written, who may use the server and in which roles, the
// workspaces with their members, and the MCP servers with their commands.
// Nothing read from the environment is part of it, and an MCP server's
// arguments, which may hold a credential, only as a digest that shows when
// they change. Config is read once, at the start, so this is what applies to
// every event until the next one.
func serverStarted(cfg Config) auditlog.Event {
	type user struct {
		Name    string `json:"name"`
		Auditor bool   `json:"auditor"`
	}
	type workspace struct {
		Name    string   `json:"name"`
		Members []string `json:"members"`
	}
	type mcpServer struct {
		Name    string `json:"name"`
		Command string `json:"command"`
		// ArgsSHA256 is the SHA-256 of the arguments as JSON. It shows a
		// change, but does not hide a guessable credential: put credentials
		// in env, from the environment, not in arguments.
		ArgsSHA256 string `json:"args_sha256"`
	}
	var d struct {
		Policy     []policy.Module `json:"policy"`
		Users      []user          `json:"users"`
		Workspaces []workspace     `json:"workspaces"`
		MCPServers []mcpServer     `json:"mcp_servers"`
	}
	// Sorted by name, so two starts with the same config record the same
	// details.
	d.Policy = cfg.Operator.Policy
	for _, name := range slices.Sorted(maps.Keys(cfg.Operator.Users)) {
		d.Users = append(d.Users, user{name, cfg.Operator.Users[name].Auditor})
	}
	for _, name := range slices.Sorted(maps.Keys(cfg.Operator.Workspaces)) {
		d.Workspaces = append(d.Workspaces, workspace{name, cfg.Operator.Workspaces[name].Members})
	}
	for _, name := range slices.Sorted(maps.Keys(cfg.Operator.MCPServers)) {
		srv := cfg.Operator.MCPServers[name]
		args := sha256.Sum256(must.Value(json.Marshal(srv.Args)))
		d.MCPServers = append(d.MCPServers, mcpServer{name, srv.Command, hex.EncodeToString(args[:])})
	}
	return auditlog.Event{Action: "server.started", Details: must.Value(json.Marshal(d))}
}

// workspaceChange is the action that changes a workspace itself, which its
// members see in its audit log: so far, a harness's new version.
const workspaceChange = "harness.changed"

// listEvents answers with a page of events that list returns, its limit and
// before taken from the request's parameters (before 0 for the newest). The
// three views of the audit log share it and differ only in list, which
// decides whose events the user sees.
func (s handlers) listEvents(c *gin.Context, limit int, before int64, list func(before int64, limit int) ([]auditlog.Event, error)) {
	if before < 0 {
		s.fail(c, http.StatusBadRequest, fmt.Errorf("before %d: must not be negative", before))
		return
	}
	limit, ok := s.pageLimit(c, limit)
	if !ok {
		return
	}
	events, err := list(before, limit)
	if err != nil {
		s.failStore(c, err)
		return
	}
	// The spec makes api.AuditLogEvent package auditlog's Event, so the
	// details keep the canonical text their hashes cover.
	out := api.AuditLogEventList{Events: events}
	// A full page may be the last: the next one is then empty.
	if len(events) == limit {
		out.Next = events[len(events)-1].ID
	}
	c.JSON(http.StatusOK, out)
}

// ListMyActivity lists what the user did: the events they are the actor of,
// in every workspace, those they left too. What others did is not theirs.
func (s handlers) ListMyActivity(c *gin.Context, params api.ListMyActivityParams) {
	user := c.GetString(userKey)
	s.listEvents(c, params.Limit, params.Before, func(before int64, limit int) ([]auditlog.Event, error) {
		return s.cfg.Store.ActorEvents(c.Request.Context(), user, before, limit)
	})
}

// ListWorkspaceAuditEvents lists the changes made to a workspace, for its
// members: only workspaceChange events, not its members' runs, which are
// private to each of them. member has checked membership already.
func (s handlers) ListWorkspaceAuditEvents(c *gin.Context, workspace string, params api.ListWorkspaceAuditEventsParams) {
	s.listEvents(c, params.Limit, params.Before, func(before int64, limit int) ([]auditlog.Event, error) {
		return s.cfg.Store.WorkspaceEvents(c.Request.Context(), workspace, workspaceChange, before, limit)
	})
}

// ListAuditEvents lists every event of the audit log for an auditor.
func (s handlers) ListAuditEvents(c *gin.Context, params api.ListAuditEventsParams) {
	if !s.auditor(c) {
		return
	}
	f := store.EventFilter{Actor: params.Actor, Workspace: params.Workspace, Action: params.Action, RunID: params.Run}
	s.listEvents(c, params.Limit, params.Before, func(before int64, limit int) ([]auditlog.Event, error) {
		f.Before, f.Limit = before, limit
		return s.cfg.Store.ListAuditEvents(c.Request.Context(), f)
	})
}
