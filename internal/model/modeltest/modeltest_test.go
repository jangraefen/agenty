package modeltest_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/model/modeltest"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

func TestScripted_ReplaysStepsInOrder(t *testing.T) {
	call := model.ToolCall{ID: "c1", Name: "tickets_read", Args: json.RawMessage(`{"id":1}`)}
	m := modeltest.NewScripted(
		modeltest.CallTools(call),
		modeltest.Reply("done"),
	)

	first, err := m.Generate(context.Background(), model.Request{})
	require.NoError(t, err)
	second, err := m.Generate(context.Background(), model.Request{})
	require.NoError(t, err)

	assert.Equal(t, model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{call}}, first)
	assert.Equal(t, model.Message{Role: model.RoleAssistant, Text: "done"}, second)
}

func TestScripted_ReturnsScriptedErrors(t *testing.T) {
	boom := errors.New("provider overloaded")
	m := modeltest.NewScripted(modeltest.Fail(boom))

	msg, err := m.Generate(context.Background(), model.Request{})

	require.ErrorIs(t, err, boom)
	assert.Zero(t, msg)
}

func TestScripted_ExhaustedScriptIsAnError(t *testing.T) {
	m := modeltest.NewScripted(modeltest.Reply("only"))
	_, err := m.Generate(context.Background(), model.Request{})
	require.NoError(t, err)

	_, err = m.Generate(context.Background(), model.Request{})

	assert.ErrorIs(t, err, modeltest.ErrScriptExhausted)
}

func TestScripted_HonoursCancelledContext(t *testing.T) {
	m := modeltest.NewScripted(modeltest.Reply("never"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := m.Generate(ctx, model.Request{})
	require.ErrorIs(t, err, context.Canceled)

	msg, err := m.Generate(context.Background(), model.Request{})
	require.NoError(t, err, "a cancelled call must not consume a step")
	assert.Equal(t, "never", msg.Text)
}

func TestScripted_RecordsRequestCopies(t *testing.T) {
	m := modeltest.NewScripted(modeltest.Reply("a"), modeltest.Reply("b"))
	msgs := []model.Message{{Role: model.RoleUser, Text: "hello"}}
	req := model.Request{
		System:   "be brief",
		Messages: msgs,
		Tools:    []toolgateway.Definition{{Name: "tickets_read"}},
	}

	_, err := m.Generate(context.Background(), req)
	require.NoError(t, err)
	msgs[0].Text = "changed after the call"
	_, err = m.Generate(context.Background(), model.Request{System: "second"})
	require.NoError(t, err)

	got := m.Requests()
	require.Len(t, got, 2)
	assert.Equal(t, "be brief", got[0].System)
	assert.Equal(t, "hello", got[0].Messages[0].Text, "recorded request must not change with the caller's slice")
	assert.Equal(t, "tickets_read", got[0].Tools[0].Name)
	assert.Equal(t, "second", got[1].System)

	got[0].System = "mutated"
	assert.Equal(t, "be brief", m.Requests()[0].System, "Requests returns a copy")
}
