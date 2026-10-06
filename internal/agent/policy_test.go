package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/agent"
	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/model/modeltest"
	"github.com/jangraefen/agenty/internal/policy"
	"github.com/jangraefen/agenty/internal/toolgateway"
	"github.com/jangraefen/agenty/internal/toolgateway/gatewaytest"
)

func TestRun_CentralAndHarnessPolicyBothApply(t *testing.T) {
	f := newFixture(5)
	f.harness.Policy = []policy.Module{{Name: "triage.rego", Source: `package agenty.tool

deny contains "ticket 13 is off limits" if input.args.id == 13`}}
	approver := &gatewaytest.Approver{Approval: toolgateway.Approval{Approved: true, Approver: "alice"}}
	cfg := f.config(modeltest.NewScripted(
		modeltest.CallTools(
			model.ToolCall{ID: "c1", Name: "tickets_read", Args: []byte(`{"id":13}`)},
			model.ToolCall{ID: "c2", Name: "tickets_label", Args: []byte(`{"id":7}`)},
		),
		modeltest.Reply("done"),
	))
	cfg.Policy = []policy.Module{{Name: "central.rego", Source: `package agenty.tool

require_approval contains "writes need a human" if input.tool == "tickets_label"`}}
	cfg.Approver = approver
	a, err := agent.New(context.Background(), cfg)
	require.NoError(t, err)

	res, err := a.Run(context.Background(), "ticket 7")
	require.NoError(t, err)

	assert.Zero(t, f.read.Calls, "harness policy denied the read")
	assert.Equal(t, 1, f.label.Calls, "central policy required approval, and alice approved")
	require.Len(t, approver.Calls, 1)
	assert.Equal(t, []string{"writes need a human"}, approver.Calls[0].Reasons)

	results := res.Messages[2].ToolResults
	require.Len(t, results, 2)
	assert.True(t, results[0].IsError)
	assert.Contains(t, results[0].Content, "ticket 13 is off limits")
	assert.False(t, results[1].IsError)
}

func TestRun_ToolCallLimitIsReportedToTheModel(t *testing.T) {
	f := newFixture(5)
	f.harness.Limits.MaxToolCalls = 1
	m := modeltest.NewScripted(
		modeltest.CallTools(call("c1", "tickets_read"), call("c2", "tickets_read")),
		modeltest.Reply("done"),
	)

	res, err := f.run(t, m)

	require.NoError(t, err)
	assert.Equal(t, 1, f.read.Calls)
	results := res.Messages[2].ToolResults
	require.Len(t, results, 2)
	assert.False(t, results[0].IsError)
	assert.True(t, results[1].IsError)
	assert.Contains(t, results[1].Content, "tool call limit reached")
}

func TestRun_ApprovalWithoutApproverIsDenied(t *testing.T) {
	f := newFixture(5)
	cfg := f.config(modeltest.NewScripted(modeltest.CallTools(call("c1", "tickets_read")), modeltest.Reply("done")))
	cfg.Policy = []policy.Module{{Name: "central.rego", Source: `package agenty.tool

require_approval contains "everything needs a human" if true`}}
	a, err := agent.New(context.Background(), cfg)
	require.NoError(t, err)

	_, err = a.Run(context.Background(), "ticket 7")

	require.NoError(t, err)
	assert.Zero(t, f.read.Calls)
	require.Len(t, f.audit.Records, 1)
	assert.Equal(t, toolgateway.Deny, f.audit.Records[0].Decision)
}

func TestRun_InlineHarnessRulesApply(t *testing.T) {
	f := newFixture(5)
	f.harness.Policy = []policy.Module{policy.RulesModule("triage (inline policy)", `deny contains "no labels from this harness" if input.tool == "tickets_label"`)}
	m := modeltest.NewScripted(modeltest.CallTools(call("c1", "tickets_label"), call("c2", "tickets_read")), modeltest.Reply("done"))

	_, err := f.run(t, m)

	require.NoError(t, err)
	assert.Zero(t, f.label.Calls)
	assert.Equal(t, 1, f.read.Calls)
	assert.Equal(t, "policy: no labels from this harness", f.audit.Records[0].Reason)
}

func TestRun_HarnessWithoutPolicyUsesCentralPolicyOnly(t *testing.T) {
	f := newFixture(5)
	require.Nil(t, f.harness.Policy)
	cfg := f.config(modeltest.NewScripted(modeltest.CallTools(call("c1", "tickets_label"), call("c2", "tickets_read")), modeltest.Reply("done")))
	cfg.Policy = []policy.Module{{Name: "central.rego", Source: `package agenty.tool

deny contains "labels are frozen" if input.tool == "tickets_label"`}}
	a, err := agent.New(context.Background(), cfg)
	require.NoError(t, err)

	_, err = a.Run(context.Background(), "ticket 7")

	require.NoError(t, err)
	assert.Zero(t, f.label.Calls, "central policy still applies")
	assert.Equal(t, 1, f.read.Calls, "nothing else is restricted")
}

func TestRun_HarnessModulesShareOneLayer(t *testing.T) {
	f := newFixture(5)
	f.harness.Policy = []policy.Module{
		{Name: "helpers.rego", Source: "package agenty.tool\n\nwrites := {\"tickets_label\", \"tickets_close\"}\n"},
		policy.RulesModule("triage (inline policy)", `deny contains "no writes from this harness" if writes[input.tool]`),
	}
	m := modeltest.NewScripted(modeltest.CallTools(call("c1", "tickets_label"), call("c2", "tickets_read")), modeltest.Reply("done"))

	_, err := f.run(t, m)

	require.NoError(t, err)
	assert.Zero(t, f.label.Calls, "the inline rule used the helper from the other module")
	assert.Equal(t, 1, f.read.Calls)
	assert.Equal(t, "policy: no writes from this harness", f.audit.Records[0].Reason)
}
