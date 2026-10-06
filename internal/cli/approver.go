package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"unicode"

	"github.com/jangraefen/agenty/internal/api"
)

// terminalApprover asks the person at the terminal to approve calls. The
// server sends each request with secrets already redacted.
type terminalApprover struct {
	out io.Writer
	// interactive is false when stdin is not a terminal: piped input is not a
	// person, so every call is rejected without reading it.
	interactive bool

	in    io.Reader
	start sync.Once
	lines chan line
}

// line is one line read from stdin, or the error that ended reading.
type line struct {
	text string
	err  error
}

func newTerminalApprover(in io.Reader, out io.Writer, interactive bool) *terminalApprover {
	return &terminalApprover{in: in, out: out, interactive: interactive, lines: make(chan line)}
}

// Approve shows the call of run runID and reads the answer: "y" or "yes"
// approves, anything else rejects.
func (a *terminalApprover) Approve(ctx context.Context, runID string, req api.ApprovalRequest) (api.Answer, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "\nApproval needed: %s (harness %s, run %s)\n", escape(req.Tool), escape(req.Harness), escape(runID))
	fmt.Fprintf(&b, "Arguments:\n%s\n", escape(indent(req.Args)))
	for _, reason := range req.Reasons {
		fmt.Fprintf(&b, "Reason: %s\n", escape(reason))
	}
	if !a.interactive {
		b.WriteString("Rejected: stdin is not a terminal.\n")
	} else {
		b.WriteString("Approve? [y/N] ")
	}
	if _, err := io.WriteString(a.out, b.String()); err != nil {
		return api.Answer{}, fmt.Errorf("approval prompt: %w", err)
	}
	if !a.interactive {
		return api.Answer{Reason: "stdin is not a terminal, so no one can approve"}, nil
	}

	answer, err := a.readLine(ctx)
	switch {
	case errors.Is(err, io.EOF):
		return api.Answer{Reason: "no answer at the terminal"}, nil
	case err != nil:
		return api.Answer{}, fmt.Errorf("approval answer: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return api.Answer{Approved: true, Reason: "approved at the terminal"}, nil
	default:
		return api.Answer{Reason: "rejected at the terminal"}, nil
	}
}

// readLine returns the next line of stdin. Reading happens in one goroutine,
// started on the first approval, so that waiting can be cancelled. A
// cancelled run ends, so a line read after cancelling is never used.
func (a *terminalApprover) readLine(ctx context.Context) (string, error) {
	a.start.Do(func() {
		go func() {
			r := bufio.NewReader(a.in)
			for {
				text, err := r.ReadString('\n')
				if err != nil && (text == "" || !errors.Is(err, io.EOF)) {
					a.lines <- line{err: err}
					close(a.lines)
					return
				}
				a.lines <- line{text: text}
			}
		}()
	})
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case l, ok := <-a.lines:
		if !ok {
			return "", io.EOF
		}
		return l.text, l.err
	}
}

// indent pretty-prints JSON arguments, or returns them as they are if they
// are not valid JSON.
func indent(args json.RawMessage) string {
	var b bytes.Buffer
	if err := json.Indent(&b, args, "  ", "  "); err != nil {
		return "  " + string(args)
	}
	return "  " + b.String()
}

// escape replaces control characters, except newlines and tabs, with \u
// escapes, so text the model chose cannot steer the terminal.
func escape(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			fmt.Fprintf(&b, `\u%04x`, r)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
