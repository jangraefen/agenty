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

	"github.com/jangraefen/agenty/internal/api"
)

var approvalRequest = api.ApprovalRequest{
	ID:      "a1",
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
			a := newTerminalApprover(strings.NewReader(tt.input), &out, true)

			req := approvalRequest
			req.Reasons = []string{"writes need a human"}
			got, err := a.Approve(context.Background(), "run-1", req)

			require.NoError(t, err)
			assert.Equal(t, api.Answer{Approved: tt.wantApproved, Reason: tt.wantReason}, got)
			assert.Contains(t, out.String(), "run run-1")
			assert.Contains(t, out.String(), "files_write_file")
			assert.Contains(t, out.String(), `"path": "notes.md"`, "the arguments are shown, indented")
			assert.Contains(t, out.String(), "writes need a human")
			assert.Contains(t, out.String(), "Approve? [y/N]")
		})
	}
}

func TestTerminalApprover_ReadsOneLinePerApproval(t *testing.T) {
	var out bytes.Buffer
	a := newTerminalApprover(strings.NewReader("y\nn\n"), &out, true)

	first, err := a.Approve(context.Background(), "run-1", approvalRequest)
	require.NoError(t, err)
	second, err := a.Approve(context.Background(), "run-1", approvalRequest)
	require.NoError(t, err)

	assert.True(t, first.Approved)
	assert.False(t, second.Approved)
	for range 2 {
		after, err := a.Approve(context.Background(), "run-1", approvalRequest)
		require.NoError(t, err)
		assert.Equal(t, "no answer at the terminal", after.Reason, "once stdin ends, every later approval is rejected")
	}
}

func TestTerminalApprover_NotInteractiveRejectsWithoutReading(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("y\n")
	a := newTerminalApprover(in, &out, false)

	got, err := a.Approve(context.Background(), "run-1", approvalRequest)

	require.NoError(t, err)
	assert.Equal(t, api.Answer{Reason: "stdin is not a terminal, so no one can approve"}, got)
	assert.Equal(t, 2, in.Len(), "stdin is not read: piped input is not a person")
	assert.Contains(t, out.String(), "files_write_file", "the operator still sees what was rejected")
}

func TestTerminalApprover_CancelledWhileWaiting(t *testing.T) {
	r, w := io.Pipe()
	t.Cleanup(func() { assert.NoError(t, w.Close()) })
	a := newTerminalApprover(r, io.Discard, true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := a.Approve(ctx, "run-1", approvalRequest)

	require.ErrorIs(t, err, context.Canceled)
}

func TestTerminalApprover_ReadError(t *testing.T) {
	r, w := io.Pipe()
	require.NoError(t, w.CloseWithError(assert.AnError))
	a := newTerminalApprover(r, io.Discard, true)

	_, err := a.Approve(context.Background(), "run-1", approvalRequest)

	require.ErrorIs(t, err, assert.AnError)
}

func TestTerminalApprover_WriteError(t *testing.T) {
	a := newTerminalApprover(strings.NewReader("y\n"), failingWriter{}, true)

	_, err := a.Approve(context.Background(), "run-1", approvalRequest)

	require.ErrorIs(t, err, assert.AnError, "an approval nobody saw is not given")
}

// TestTerminalApprover_EscapesControlCharacters: the model chooses the
// arguments, so it must not be able to send terminal escape sequences that
// rewrite what the person sees.
func TestTerminalApprover_EscapesControlCharacters(t *testing.T) {
	var out bytes.Buffer
	a := newTerminalApprover(strings.NewReader("n\n"), &out, true)
	req := approvalRequest
	req.Args = json.RawMessage("{\"path\":\"a\u009b2Jb\"}")

	req.Reasons = []string{"reason\x1b[2K"}
	_, err := a.Approve(context.Background(), "run-\x1b]0;x", req)

	require.NoError(t, err)
	assert.NotContains(t, out.String(), "\u009b")
	assert.NotContains(t, out.String(), "\x1b")
	assert.Contains(t, out.String(), `a\u009b2Jb`)
	assert.Contains(t, out.String(), `reason\u001b[2K`)
	assert.Contains(t, out.String(), `run-\u001b]0;x`, "the run ID comes from the server and is escaped too")
}

func TestTerminalApprover_InvalidArgsAreShownAsText(t *testing.T) {
	var out bytes.Buffer
	a := newTerminalApprover(strings.NewReader("n\n"), &out, true)
	req := approvalRequest
	req.Args = json.RawMessage(`{not json`)

	_, err := a.Approve(context.Background(), "run-1", req)

	require.NoError(t, err)
	assert.Contains(t, out.String(), "{not json")
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, assert.AnError }
