package server

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/jangraefen/agenty/internal/api"
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
// first.
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

// GetAuditRun shows an auditor a run of any workspace with its audit records.
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
		ConversationID: r.ConversationID,
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
