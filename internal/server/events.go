package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

// workspaceChanges are the actions that change a workspace itself, which its
// members see in its audit log.
var workspaceChanges = []string{"harness.changed"}

// eventList is a page of the audit log as the API lists it. It is not the
// generated api.AuditLogEventList: the events are package auditlog's, so
// their details keep the canonical text their hashes cover.
type eventList struct {
	Events []auditlog.Event `json:"events"`
	Next   int64            `json:"next,omitempty"`
}

// listEvents answers with a page of events that list returns, its limit and
// before taken from the request's parameters (before 0 for the newest).
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
	out := eventList{Events: events}
	// A full page may be the last: the next one is then empty.
	if len(events) == limit {
		out.Next = events[len(events)-1].ID
	}
	c.JSON(http.StatusOK, out)
}

// ListMyActivity lists what the user did: the events they are the actor of,
// in the workspaces they are a member of now. What others did is not theirs.
func (s handlers) ListMyActivity(c *gin.Context, params api.ListMyActivityParams) {
	user := c.GetString(userKey)
	s.listEvents(c, params.Limit, params.Before, func(before int64, limit int) ([]auditlog.Event, error) {
		return s.cfg.Store.ActorEvents(c.Request.Context(), user, s.memberships(user), before, limit)
	})
}

// ListWorkspaceAuditEvents lists the changes made to a workspace, for its
// members.
func (s handlers) ListWorkspaceAuditEvents(c *gin.Context, workspace string, params api.ListWorkspaceAuditEventsParams) {
	s.listEvents(c, params.Limit, params.Before, func(before int64, limit int) ([]auditlog.Event, error) {
		return s.cfg.Store.WorkspaceEvents(c.Request.Context(), workspace, workspaceChanges, before, limit)
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
