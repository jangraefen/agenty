package agent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/agent"
	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/model/modeltest"
)

// transcript records every appended message, or fails at index failAt.
type transcript struct {
	runIDs   []string
	indexes  []int
	messages []model.Message
	failAt   int
}

func (tr *transcript) Append(_ context.Context, runID string, index int, msg model.Message) error {
	if index == tr.failAt {
		return errors.New("transcript store down")
	}
	tr.runIDs = append(tr.runIDs, runID)
	tr.indexes = append(tr.indexes, index)
	tr.messages = append(tr.messages, msg)
	return nil
}

func TestRun_RecordsTheTranscriptAsItGoes(t *testing.T) {
	f := newFixture(5)
	tr := &transcript{failAt: -1}
	cfg := f.config(modeltest.NewScripted(
		modeltest.CallTools(call("c1", "tickets_read"), call("c2", "tickets_delete")),
		modeltest.Reply("labelled"),
	))
	cfg.Transcript = tr
	a, err := agent.New(context.Background(), cfg)
	require.NoError(t, err)

	res, err := a.Run(context.Background(), "ticket 7")

	require.NoError(t, err)
	require.Len(t, res.Messages, 4)
	assert.Equal(t, res.Messages, tr.messages, "every message is recorded, in order")
	assert.Equal(t, []int{0, 1, 2, 3}, tr.indexes)
	for _, id := range tr.runIDs {
		assert.Equal(t, res.RunID, id)
	}
	assert.Equal(t, "ticket 7", tr.messages[0].Text)
	assert.Equal(t, model.RoleAssistant, tr.messages[1].Role)
	require.Len(t, tr.messages[2].ToolResults, 2)
	assert.True(t, tr.messages[2].ToolResults[1].IsError, "a denied call is recorded as the model saw it")
	assert.Equal(t, "labelled", tr.messages[3].Text)
}

func TestRun_KeepsTheTranscriptOfAFailedRun(t *testing.T) {
	f := newFixture(5)
	tr := &transcript{failAt: -1}
	cfg := f.config(modeltest.NewScripted(
		modeltest.CallTools(call("c1", "tickets_read")),
		modeltest.Fail(errors.New("model down")),
	))
	cfg.Transcript = tr
	a, err := agent.New(context.Background(), cfg)
	require.NoError(t, err)

	_, err = a.Run(context.Background(), "ticket 7")

	require.ErrorContains(t, err, "model down")
	assert.Equal(t, []int{0, 1, 2}, tr.indexes, "everything up to the failure is recorded")
}

func TestRun_StopsWhenTheTranscriptCannotBeRecorded(t *testing.T) {
	for _, failAt := range []int{0, 1, 2} {
		f := newFixture(5)
		m := modeltest.NewScripted(
			modeltest.CallTools(call("c1", "tickets_read")),
			modeltest.CallTools(call("c2", "tickets_label")),
			modeltest.Reply("done"),
		)
		cfg := f.config(m)
		cfg.Transcript = &transcript{failAt: failAt}
		a, err := agent.New(context.Background(), cfg)
		require.NoError(t, err)

		_, err = a.Run(context.Background(), "ticket 7")

		require.ErrorContains(t, err, "transcript store down", "failAt %d", failAt)
		require.ErrorContains(t, err, "transcript")
		assert.Zero(t, f.label.Calls, "nothing runs after a message that could not be recorded (failAt %d)", failAt)
		if failAt == 0 {
			assert.Empty(t, m.Requests(), "the model is not called before the input is recorded")
			assert.Zero(t, f.read.Calls)
		}
		if failAt == 1 {
			assert.Zero(t, f.read.Calls, "the model's tool calls do not run before its reply is recorded")
		}
	}
}
