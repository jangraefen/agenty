package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"slices"

	"github.com/gin-gonic/gin"

	"github.com/jangraefen/agenty/internal/auditlog"
	"github.com/jangraefen/agenty/internal/must"
	"github.com/jangraefen/agenty/internal/policy"
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

// recordRead records an auditor's read, with what was read, and answers the
// request itself when it cannot: a read the log cannot record does not
// happen, and is then not ok. A read is no event of the run it read, so a
// run's events never hold who looked at it.
func (s handlers) recordRead(c *gin.Context, what map[string]any) bool {
	e := auditlog.Event{Actor: c.GetString(userKey), Action: "audit.read", Details: must.Value(json.Marshal(what))}
	if _, err := s.cfg.Store.AppendEvent(c.Request.Context(), e); err != nil {
		s.failStore(c, err)
		return false
	}
	return true
}
