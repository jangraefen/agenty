package toolgateway_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/secret"
	"github.com/jangraefen/agenty/internal/toolgateway"
	"github.com/jangraefen/agenty/internal/toolgateway/gatewaytest"
)

// suspending is a run's gateway whose policy requires approval for
// tickets_close.
type suspending struct {
	cfg    toolgateway.Config
	gw     *toolgateway.Gateway
	audit  *gatewaytest.Audit
	policy *gatewaytest.Policy
	read   *gatewaytest.Tool
	close  *gatewaytest.Tool
}

func newSuspending(t *testing.T, maxToolCalls int, redactor *secret.Redactor) *suspending {
	t.Helper()
	s := &suspending{
		audit: &gatewaytest.Audit{},
		policy: &gatewaytest.Policy{Verdicts: map[string]toolgateway.Verdict{
			"tickets_close": {Decision: toolgateway.RequireApproval, Reasons: []string{"closing needs a human"}},
		}},
		read:  &gatewaytest.Tool{Name: "tickets_read", Result: json.RawMessage(`{"ok":true}`)},
		close: &gatewaytest.Tool{Name: "tickets_close", Result: json.RawMessage(`{"closed":true}`)},
	}
	s.cfg = toolgateway.Config{
		RunID:        "r1",
		Redactor:     redactor,
		Harness:      "triage",
		MaxToolCalls: maxToolCalls,
		Policy:       s.policy,
		Granted:      []string{"tickets_read", "tickets_close"},
		Servers:      gatewaytest.Servers(s.read, s.close),
		Audit:        s.audit,
	}
	gw, err := toolgateway.New(context.Background(), s.cfg)
	require.NoError(t, err)
	s.gw = gw
	return s
}

// resumed returns a new gateway for the run, as a server builds one to
// resume it, with its call counts restored from records.
func (s *suspending) resumed(t *testing.T, records []toolgateway.Record) *toolgateway.Gateway {
	t.Helper()
	cfg := s.cfg
	cfg.Records = records
	gw, err := toolgateway.New(context.Background(), cfg)
	require.NoError(t, err)
	return gw
}

// suspend calls tickets_close and returns the suspension.
func (s *suspending) suspend(t *testing.T, args string) *toolgateway.Suspended {
	t.Helper()
	_, err := s.gw.Call(context.Background(), toolgateway.ToolCall{Name: "tickets_close", Args: json.RawMessage(args)})
	var suspended *toolgateway.Suspended
	require.ErrorAs(t, err, &suspended)
	return suspended
}

// TestInvariant_ApprovalSuspendsTheRun guards a trust-model guarantee: a
// call that policy marks as requiring approval never runs without a recorded
// approval. Call suspends the run with only the decision recorded and the
// tool not called.
func TestInvariant_ApprovalSuspendsTheRun(t *testing.T) {
	s := newSuspending(t, 10, gatewaytest.NoSecrets)

	suspended := s.suspend(t, `{"id":7}`)

	require.NotErrorIs(t, suspended, toolgateway.ErrDenied)
	assert.Zero(t, s.close.Calls, "a suspended call does not run")
	require.Len(t, s.audit.Records, 1, "only the decision is recorded")
	decision := s.audit.Records[0]
	assert.Equal(t, toolgateway.EventDecision, decision.Event)
	assert.Equal(t, toolgateway.RequireApproval, decision.Decision)
	assert.Equal(t, decision.CallID, suspended.CallID, "the suspension names the call")
	assert.Equal(t, "r1", suspended.Request.RunID)
	assert.Equal(t, "tickets_close", suspended.Request.Tool)
	assert.JSONEq(t, `{"id":7}`, string(suspended.Request.Args))
	assert.Equal(t, []string{"closing needs a human"}, suspended.Reasons)
	assert.EqualError(t, suspended, "tool tickets_close: waiting for approval")
}

// TestInvariant_ACallHoldingASecretNeverWaits guards a trust-model
// guarantee: a call waits for approval as a person sees it, redacted, and
// runs later as it is stored, so a call whose arguments hold a secret could
// only run as something other than what the model asked for. It is denied
// instead, and the secret is in no record.
func TestInvariant_ACallHoldingASecretNeverWaits(t *testing.T) {
	redactor, err := secret.NewRedactor([]string{"sk-secret-0123456789"})
	require.NoError(t, err)
	s := newSuspending(t, 10, redactor)

	_, err = s.gw.Call(context.Background(), toolgateway.ToolCall{Name: "tickets_close", Args: json.RawMessage(`{"note":"sk-secret-0123456789"}`)})

	require.ErrorIs(t, err, toolgateway.ErrDenied)
	require.ErrorContains(t, err, "its arguments hold a secret")
	var suspended *toolgateway.Suspended
	assert.NotErrorAs(t, err, &suspended)
	assert.Zero(t, s.close.Calls)
	require.Len(t, s.audit.Records, 2)
	approval := s.audit.Records[1]
	assert.Equal(t, toolgateway.EventApproval, approval.Event)
	assert.Equal(t, toolgateway.Deny, approval.Decision)
	for _, r := range s.audit.Records {
		assert.NotContains(t, string(r.Args), "sk-secret")
	}
}

func TestResume_RunsTheApprovedCallUnderItsOwnID(t *testing.T) {
	s := newSuspending(t, 1, gatewaytest.NoSecrets)
	suspended := s.suspend(t, `{"id":7}`)

	resumed := s.resumed(t, s.audit.Records)
	out, err := resumed.Resume(context.Background(), suspended.CallID, toolgateway.ToolCall{Name: "tickets_close", Args: json.RawMessage(`{"id":7}`)}, toolgateway.Approval{Approved: true, Approver: "ana", Reason: "fine"})

	require.NoError(t, err, "deciding again is not another attempt, so the limit of one call holds")
	assert.JSONEq(t, `{"closed":true}`, string(out))
	assert.Equal(t, 1, s.close.Calls)
	require.Len(t, s.audit.Records, 4)
	for _, r := range s.audit.Records {
		assert.Equal(t, suspended.CallID, r.CallID, "every record of the call carries its own ID")
		assert.Equal(t, "r1", r.RunID)
	}
	again := s.audit.Records[1]
	assert.Equal(t, toolgateway.EventDecision, again.Event, "the call is decided again")
	assert.Equal(t, toolgateway.RequireApproval, again.Decision)
	approval := s.audit.Records[2]
	assert.Equal(t, toolgateway.EventApproval, approval.Event)
	assert.Equal(t, toolgateway.Allow, approval.Decision)
	assert.Equal(t, "ana", approval.Approver)
	assert.Equal(t, toolgateway.EventResult, s.audit.Records[3].Event)
}

func TestResume_DeniesARejectedCall(t *testing.T) {
	s := newSuspending(t, 10, gatewaytest.NoSecrets)
	suspended := s.suspend(t, `{}`)

	_, err := s.resumed(t, s.audit.Records).Resume(context.Background(), suspended.CallID, toolgateway.ToolCall{Name: "tickets_close", Args: json.RawMessage(`{}`)}, toolgateway.Approval{Approver: "ana", Reason: "not today"})

	require.ErrorIs(t, err, toolgateway.ErrDenied)
	require.ErrorContains(t, err, "approval rejected: not today")
	assert.Zero(t, s.close.Calls)
	last := s.audit.Records[len(s.audit.Records)-1]
	assert.Equal(t, toolgateway.EventApproval, last.Event)
	assert.Equal(t, toolgateway.Deny, last.Decision)
	assert.Equal(t, "ana", last.Approver)
}

// TestInvariant_ResumedCallsAreDecidedAgain guards a trust-model guarantee:
// an approved call is checked against policy as it is when the call
// resumes, so a rule added while the call waited denies it; an answer never
// loosens policy.
func TestInvariant_ResumedCallsAreDecidedAgain(t *testing.T) {
	s := newSuspending(t, 10, gatewaytest.NoSecrets)
	suspended := s.suspend(t, `{}`)
	s.policy.Verdicts["tickets_close"] = toolgateway.Verdict{Decision: toolgateway.Deny, Reasons: []string{"closing is frozen"}}

	_, err := s.resumed(t, s.audit.Records).Resume(context.Background(), suspended.CallID, toolgateway.ToolCall{Name: "tickets_close", Args: json.RawMessage(`{}`)}, toolgateway.Approval{Approved: true, Approver: "ana"})

	require.ErrorIs(t, err, toolgateway.ErrDenied)
	require.ErrorContains(t, err, "closing is frozen")
	assert.Zero(t, s.close.Calls)
	last := s.audit.Records[len(s.audit.Records)-1]
	assert.Equal(t, suspended.CallID, last.CallID)
	assert.Equal(t, toolgateway.EventDecision, last.Event)
	assert.Equal(t, toolgateway.Deny, last.Decision)
	require.Len(t, s.policy.Inputs, 2)
	assert.Equal(t, s.policy.Inputs[0], s.policy.Inputs[1], "policy sees the call as it did before")
}

// TestInvariant_LimitsSurviveARestore guards a trust-model guarantee: a run
// restored from its audit log keeps its call counts, denied calls included,
// so resuming a run never resets its limits or what policy sees.
func TestInvariant_LimitsSurviveARestore(t *testing.T) {
	s := newSuspending(t, 4, gatewaytest.NoSecrets)
	run := s.gw
	ctx := context.Background()
	_, err := run.Call(ctx, toolgateway.ToolCall{Name: "tickets_read"})
	require.NoError(t, err)
	_, err = run.Call(ctx, toolgateway.ToolCall{Name: "tickets_nope"})
	require.ErrorIs(t, err, toolgateway.ErrDenied)
	_, err = run.Call(ctx, toolgateway.ToolCall{Name: "tickets_close", Args: json.RawMessage(`{}`)})
	var suspended *toolgateway.Suspended
	require.ErrorAs(t, err, &suspended)

	resumed := s.resumed(t, s.audit.Records)
	_, err = resumed.Resume(ctx, suspended.CallID, toolgateway.ToolCall{Name: "tickets_close", Args: json.RawMessage(`{}`)}, toolgateway.Approval{Approved: true})
	require.NoError(t, err)
	_, err = resumed.Call(ctx, toolgateway.ToolCall{Name: "tickets_read"})
	require.NoError(t, err, "the fourth attempt is within the limit")
	_, err = resumed.Call(ctx, toolgateway.ToolCall{Name: "tickets_read"})

	require.ErrorIs(t, err, toolgateway.ErrDenied)
	require.ErrorContains(t, err, "tool call limit reached", "three attempts before the restore count")
	last := s.policy.Inputs[len(s.policy.Inputs)-1]
	assert.Equal(t, toolgateway.CallCounts{Total: 2, ByTool: map[string]int{"tickets_read": 1, "tickets_close": 1}}, last.Calls, "policy sees the calls executed before the restore")
}

func TestNew_RestoresOnlyItsRunsRecords(t *testing.T) {
	s := newSuspending(t, 1, gatewaytest.NoSecrets)
	_, err := s.gw.Call(context.Background(), toolgateway.ToolCall{Name: "tickets_read"})
	require.NoError(t, err)

	cfg := s.cfg
	cfg.RunID, cfg.Records = "mine", s.audit.Records
	mine, err := toolgateway.New(context.Background(), cfg)
	require.NoError(t, err)
	_, err = mine.Call(context.Background(), toolgateway.ToolCall{Name: "tickets_read"})

	require.NoError(t, err, "another run's calls do not count")
}

func TestCall_ACancelledRunIsNotSuspended(t *testing.T) {
	s := newSuspending(t, 10, gatewaytest.NoSecrets)
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(errors.New("cancelled by ana"))

	_, err := s.gw.Call(ctx, toolgateway.ToolCall{Name: "tickets_close", Args: json.RawMessage(`{}`)})

	require.ErrorIs(t, err, toolgateway.ErrDenied)
	var suspended *toolgateway.Suspended
	require.NotErrorAs(t, err, &suspended)
	last := s.audit.Records[len(s.audit.Records)-1]
	assert.Equal(t, toolgateway.EventApproval, last.Event)
	assert.Equal(t, "approval failed: cancelled by ana", last.Reason, "the cancellation is the cause")
	assert.Zero(t, s.close.Calls)
}
