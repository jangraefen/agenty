package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/toolgateway"
)

var approvalRequest = toolgateway.Request{
	RunID:   "run-1",
	Harness: "notes",
	Tool:    "files_write_file",
	Args:    json.RawMessage(`{"path":"notes.md","content":"hi"}`),
}

func TestTerminalApprover_Answers(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantApproved bool
		wantReason   string
	}{
		{"yes", "y\n", true, "approved at the terminal"},
		{"yes in full, any case", "  YES \n", true, "approved at the terminal"},
		{"no", "n\n", false, "rejected at the terminal"},
		{"empty answer is no", "\n", false, "rejected at the terminal"},
		{"anything else is no", "sure\n", false, "rejected at the terminal"},
		{"no answer", "", false, "no answer at the terminal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			a := newTerminalApprover(strings.NewReader(tt.input), &out, true, "alice")

			got, err := a.Approve(context.Background(), approvalRequest, []string{"writes need a human"})

			require.NoError(t, err)
			assert.Equal(t, toolgateway.Approval{Approved: tt.wantApproved, Approver: "alice", Reason: tt.wantReason}, got)
			assert.Contains(t, out.String(), "files_write_file")
			assert.Contains(t, out.String(), `"path": "notes.md"`, "the arguments are shown, indented")
			assert.Contains(t, out.String(), "writes need a human")
			assert.Contains(t, out.String(), "Approve? [y/N]")
		})
	}
}

func TestTerminalApprover_ReadsOneLinePerApproval(t *testing.T) {
	var out bytes.Buffer
	a := newTerminalApprover(strings.NewReader("y\nn\n"), &out, true, "alice")

	first, err := a.Approve(context.Background(), approvalRequest, nil)
	require.NoError(t, err)
	second, err := a.Approve(context.Background(), approvalRequest, nil)
	require.NoError(t, err)

	assert.True(t, first.Approved)
	assert.False(t, second.Approved)
	for range 2 {
		after, err := a.Approve(context.Background(), approvalRequest, nil)
		require.NoError(t, err)
		assert.Equal(t, "no answer at the terminal", after.Reason, "once stdin ends, every later approval is rejected")
	}
}

func TestTerminalApprover_NotInteractiveRejectsWithoutReading(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("y\n")
	a := newTerminalApprover(in, &out, false, "alice")

	got, err := a.Approve(context.Background(), approvalRequest, nil)

	require.NoError(t, err)
	assert.Equal(t, toolgateway.Approval{Reason: "stdin is not a terminal, so no one can approve"}, got)
	assert.Equal(t, 2, in.Len(), "stdin is not read: piped input is not a person")
	assert.Contains(t, out.String(), "files_write_file", "the operator still sees what was rejected")
}

func TestTerminalApprover_CancelledWhileWaiting(t *testing.T) {
	r, w := io.Pipe()
	t.Cleanup(func() { assert.NoError(t, w.Close()) })
	a := newTerminalApprover(r, io.Discard, true, "alice")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := a.Approve(ctx, approvalRequest, nil)

	require.ErrorIs(t, err, context.Canceled)
}

func TestTerminalApprover_ReadError(t *testing.T) {
	r, w := io.Pipe()
	require.NoError(t, w.CloseWithError(assert.AnError))
	a := newTerminalApprover(r, io.Discard, true, "alice")

	_, err := a.Approve(context.Background(), approvalRequest, nil)

	require.ErrorIs(t, err, assert.AnError)
}

func TestTerminalApprover_WriteError(t *testing.T) {
	a := newTerminalApprover(strings.NewReader("y\n"), failingWriter{}, true, "alice")

	_, err := a.Approve(context.Background(), approvalRequest, nil)

	require.ErrorIs(t, err, assert.AnError, "an approval nobody saw is not given")
}

// TestTerminalApprover_EscapesControlCharacters: the model chooses the
// arguments, so it must not be able to send terminal escape sequences that
// rewrite what the person sees.
func TestTerminalApprover_EscapesControlCharacters(t *testing.T) {
	var out bytes.Buffer
	a := newTerminalApprover(strings.NewReader("n\n"), &out, true, "alice")
	req := approvalRequest
	req.Args = json.RawMessage("{\"path\":\"a\u009b2Jb\"}")

	_, err := a.Approve(context.Background(), req, []string{"reason\x1b[2K"})

	require.NoError(t, err)
	assert.NotContains(t, out.String(), "\u009b")
	assert.NotContains(t, out.String(), "\x1b")
	assert.Contains(t, out.String(), `a\u009b2Jb`)
	assert.Contains(t, out.String(), `reason\u001b[2K`)
}

func TestTerminalApprover_InvalidArgsAreShownAsText(t *testing.T) {
	var out bytes.Buffer
	a := newTerminalApprover(strings.NewReader("n\n"), &out, true, "alice")
	req := approvalRequest
	req.Args = json.RawMessage(`{not json`)

	_, err := a.Approve(context.Background(), req, nil)

	require.NoError(t, err)
	assert.Contains(t, out.String(), "{not json")
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, assert.AnError }
