package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/must"
	"github.com/jangraefen/agenty/internal/store/db"
)

// ApprovalStatus says whether an approval request is answered, and how. A
// request leaves pending once and never returns to it: the updates that
// answer, expire and withdraw it all match only a pending request.
type ApprovalStatus string

const (
	// ApprovalPending is a request nobody has answered yet; its run waits.
	ApprovalPending ApprovalStatus = "pending"
	// ApprovalApproved is a request the run's owner approved.
	ApprovalApproved ApprovalStatus = "approved"
	// ApprovalRejected is a request a person rejected, or that expired.
	ApprovalRejected ApprovalStatus = "rejected"
	// ApprovalWithdrawn is a request whose run was cancelled.
	ApprovalWithdrawn ApprovalStatus = "withdrawn"
)

// NewApproval is the request of a run suspended at a call that waits for
// approval: the call as the approver sees it, redacted, and what the run
// needs to resume at it.
//
// Storing what the run needs to resume is what makes approvals durable: a
// waiting run holds no worker and no memory, and a worker that takes it up
// after an answer, also after a restart, resumes from this request and the
// run's transcript and audit log.
type NewApproval struct {
	ID, RunID string
	// CallID identifies the call in the run's audit log, and Call is its
	// index in the model's reply. Results are the results of the calls
	// before it in that reply.
	CallID  string
	Call    int
	Results []model.ToolResult
	// Tool and Args are the call as the approver sees it; the request is
	// bound to these arguments, so an answer approves this call only.
	Tool string
	Args json.RawMessage
	// Reasons are the policy's reasons for asking.
	Reasons []string
	// CreatedAt is when the run asked, and ExpiresAt when the request is
	// rejected if nobody answered it.
	CreatedAt, ExpiresAt time.Time
}

// Approval is a stored approval request, with its answer once it has one.
type Approval struct {
	NewApproval
	// Harness names the harness of the request's run.
	Harness string
	Status  ApprovalStatus
	// Approver names who answered; it is empty for a request that expired or
	// was withdrawn. Reason is the answer's reason, or why it expired or was
	// withdrawn.
	Approver string
	Reason   string
}

// Answer is a person's answer to an approval request. The server sets
// Approver to the signed-in user, the run's owner, as no one else may
// answer.
type Answer struct {
	Approved bool
	Approver string
	Reason   string
}

// SuspendRun stores a's request and marks its running run as waiting for
// it. A run that is not running returns ErrNotFound.
//
// Both happen in one transaction, so there is never a waiting run without
// its request, nor a request whose run still runs. The run is marked first:
// if it is not running, such as one that has already ended, no request is
// stored.
func (s *Store) SuspendRun(ctx context.Context, a NewApproval) error {
	return s.inTx(ctx, func(q *db.Queries) error {
		n, err := q.SuspendRun(ctx, a.RunID)
		switch {
		case err == nil && n == 0:
			err = fmt.Errorf("running run %s: %w", a.RunID, ErrNotFound)
		case err == nil:
			err = q.InsertApproval(ctx, db.InsertApprovalParams{
				ID:        a.ID,
				RunID:     a.RunID,
				CallID:    a.CallID,
				CallIndex: int32(a.Call), //nolint:gosec // G115: a reply makes few calls.
				Tool:      a.Tool,
				Args:      a.Args,
				// Slices of strings and of plain structs always marshal.
				Reasons:   must.Value(json.Marshal(nonNil(a.Reasons))),
				Results:   must.Value(json.Marshal(nonNil(a.Results))),
				CreatedAt: a.CreatedAt,
				ExpiresAt: a.ExpiresAt,
			})
		}
		if err != nil {
			return fmt.Errorf("store: suspend: %w", err)
		}
		return nil
	})
}

// nonNil returns s, or an empty slice for nil, which marshals as []. The
// reasons and results columns are NOT NULL jsonb, and reading them back
// expects a list.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// PendingApprovals returns the requests the runs of workspace wait for whose
// conversations user started, oldest first. Only the owner may answer, so
// only the owner's requests are listed (trust-model guarantee 7).
func (s *Store) PendingApprovals(ctx context.Context, workspace, user string) ([]Approval, error) {
	rows, err := s.queries.PendingApprovals(ctx, db.PendingApprovalsParams{Workspace: workspace, Owner: user})
	if err != nil {
		return nil, fmt.Errorf("store: approvals: %w", err)
	}
	out := make([]Approval, len(rows))
	for i, r := range rows {
		if out[i], err = approval(r.Approval, r.Harness); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// LatestApproval returns the latest approval request of the run, and false
// if it has none. A worker resuming a run reads it for the answer to
// resume with, and a cancelled run's close-out for the call that did not
// run. A run waits for one call at a time, so the latest is the one that
// matters.
func (s *Store) LatestApproval(ctx context.Context, runID string) (Approval, bool, error) {
	row, err := s.queries.LatestApproval(ctx, runID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Approval{}, false, nil
	case err != nil:
		return Approval{}, false, fmt.Errorf("store: approvals of run %s: %w", runID, err)
	}
	a, err := approval(row.Approval, row.Harness)
	return a, err == nil, err
}

// AnswerApproval answers the pending request id of the run runID in
// workspace, and queues the run to resume. It reports false if there is no
// such request, or it is not pending or has expired: a request is answered
// once.
//
// The answer and the run's queueing commit together, so an answered
// request always has its run queued for a worker. It appends no audit
// event: when a worker resumes the run, the tool gateway records the
// answer as the call's tool.approval event, under the call's own ID.
func (s *Store) AnswerApproval(ctx context.Context, workspace, runID, id string, a Answer) (bool, error) {
	status := ApprovalRejected
	if a.Approved {
		status = ApprovalApproved
	}
	answered := false
	err := s.inTx(ctx, func(q *db.Queries) error {
		run, err := q.AnswerApproval(ctx, db.AnswerApprovalParams{Status: string(status), Approver: a.Approver, Reason: a.Reason, ID: id, RunID: runID, Workspace: workspace})
		// No row: the request is not this run's or this workspace's, or it
		// was answered, expired or withdrawn first. That is a refusal, not
		// an error, and the transaction has changed nothing.
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err == nil {
			_, err = q.QueueWaitingRuns(ctx, []string{run})
		}
		if err != nil {
			return fmt.Errorf("store: approval %s: %w", id, err)
		}
		answered = true
		return nil
	})
	return answered, err
}

// ExpireApprovals rejects the pending requests that have expired, giving
// reason, and queues their runs to resume. It returns how many it rejected.
// The server's expiry goroutine calls it as each request falls due; a
// resumed run then tells the model that its call was not approved.
func (s *Store) ExpireApprovals(ctx context.Context, reason string) (int, error) {
	expired := 0
	err := s.inTx(ctx, func(q *db.Queries) error {
		runs, err := q.ExpireApprovals(ctx, reason)
		if err == nil && len(runs) > 0 {
			_, err = q.QueueWaitingRuns(ctx, runs)
		}
		if err != nil {
			return fmt.Errorf("store: expire approvals: %w", err)
		}
		expired = len(runs)
		return nil
	})
	return expired, err
}

// NextApprovalExpiry returns when the next pending request expires, and false
// if none is pending. The expiry goroutine sleeps until then rather than
// polling.
func (s *Store) NextApprovalExpiry(ctx context.Context) (time.Time, bool, error) {
	at, err := s.queries.NextApprovalExpiry(ctx)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return time.Time{}, false, nil
	case err != nil:
		return time.Time{}, false, fmt.Errorf("store: approvals: %w", err)
	}
	return at, true, nil
}

// approval maps a stored request and its run's harness name to an
// Approval, decoding its reasons and results.
func approval(row db.Approval, harness string) (Approval, error) {
	a := Approval{
		NewApproval: NewApproval{
			ID:        row.ID,
			RunID:     row.RunID,
			CallID:    row.CallID,
			Call:      int(row.CallIndex),
			Tool:      row.Tool,
			Args:      row.Args,
			CreatedAt: row.CreatedAt,
			ExpiresAt: row.ExpiresAt,
		},
		Harness:  harness,
		Status:   ApprovalStatus(row.Status),
		Approver: row.Approver,
		Reason:   row.Reason,
	}
	if err := json.Unmarshal(row.Reasons, &a.Reasons); err != nil {
		return Approval{}, fmt.Errorf("store: approval %s: reasons: %w", row.ID, err)
	}
	if err := json.Unmarshal(row.Results, &a.Results); err != nil {
		return Approval{}, fmt.Errorf("store: approval %s: results: %w", row.ID, err)
	}
	return a, nil
}
