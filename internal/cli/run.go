package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/jangraefen/agenty/internal/api"
	"github.com/jangraefen/agenty/internal/must"
	"github.com/jangraefen/agenty/internal/secret"
)

// runUsage is the usage text of "agenty run".
const runUsage = `usage: agenty run [flags] HARNESS INPUT

Runs the latest version of HARNESS on INPUT on the server, asks at the
terminal when a call needs approval, and prints the answer. Approvals are
recorded as given by the signed-in user. Interrupting the command cancels
the run.
`

// run implements "agenty run": it starts a run of a harness on the server,
// follows it to its end, and prints the answer.
//
// The CLI only starts runs and never follows one up: a run it starts is the
// first of a new conversation, owned by the signed-in user, who alone may
// answer its approvals (guarantee 7). The answer goes to stdout and the logs
// to stderr, so the answer can be piped on its own. The exit code is
// exitOK only for a succeeded run; a failed or cancelled one prints what
// output it has, logs its error and exits with exitFailure.
func run(ctx context.Context, args []string, env Env) int {
	flags, rest, code, ok := parseFlags(command{name: "run", usage: runUsage, nargs: 2, client: true, workspace: true}, args, env)
	if !ok {
		return code
	}
	logger, err := clientLogger(env, flags)
	if err != nil {
		return fail(logger, "run failed", err)
	}
	c, err := newClient(flags)
	if err != nil {
		return fail(logger, "run failed", err)
	}
	var started api.Run
	if err := c.do(ctx, http.MethodPost, c.path("runs"), api.CreateRun{Harness: rest[0], Input: rest[1]}, &started); err != nil {
		return fail(logger, "run failed", err)
	}
	logger.Info("run started", "harness", rest[0], "run_id", started.ID)

	// The server runs the agent and its tools; the CLI only watches the
	// events and answers approvals, so it never executes a tool itself
	// (guarantee 2).
	finished, err := follow(ctx, logger, c, started.ID, newTerminalApprover(env.Stdin, env.Stderr, env.Interactive))
	if err != nil && ctx.Err() != nil {
		// Interrupted: the run is not left behind on the server. The cancel
		// gets a context of its own, detached from the cancelled one, with a
		// timeout so an unreachable server cannot hang the exit.
		cancelCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer stop()
		if cerr := c.do(cancelCtx, http.MethodPost, c.path("runs", started.ID, "cancel"), nil, nil); cerr != nil {
			err = errors.Join(err, fmt.Errorf("cancel: %w", cerr))
		} else {
			logger.Info("run cancelled", "run_id", started.ID)
		}
	}
	if err != nil {
		return fail(logger, "run failed", fmt.Errorf("run %s: %w", started.ID, err))
	}
	if finished.Output != "" {
		// The model wrote the answer: escape what could steer the terminal.
		if _, err := io.WriteString(env.Stdout, escape(finished.Output)+"\n"); err != nil {
			return fail(logger, "run failed", fmt.Errorf("write answer: %w", err))
		}
	}
	if finished.Status != api.RunStatusSucceeded {
		logger.Error("run failed", "run_id", finished.ID, "steps", finished.Steps, "error", finished.Error)
		return exitFailure
	}
	logger.Info("run finished", "run_id", finished.ID, "steps", finished.Steps)
	return exitOK
}

// follow reads a run's events until it finishes, logs its tool calls, and
// answers its approval requests at the terminal. It returns the finished
// run.
//
// Three event kinds matter: audit records, logged at debug level only, since
// they repeat what the server already recorded in its audit log; approval
// requests, answered through approver and posted back; and the run's end,
// which carries the finished run. Other event names are ignored, so a newer
// server can add kinds without breaking an older CLI. A stream that ends
// before the run's end is an error: the CLI cannot tell how the run ended.
func follow(ctx context.Context, logger *slog.Logger, c *client, runID string, approver *terminalApprover) (finished api.Run, err error) {
	events, err := c.events(ctx, runID)
	if err != nil {
		return api.Run{}, err
	}
	defer func() {
		if cerr := events.Close(); cerr != nil {
			err = errors.Join(err, cerr)
		}
	}()
	for {
		name, data, err := events.next()
		if errors.Is(err, io.EOF) {
			return api.Run{}, errors.New("the event stream ended before the run finished")
		}
		if err != nil {
			return api.Run{}, err
		}
		switch name {
		case api.EventAudit:
			var rec api.AuditRecord
			if err := json.Unmarshal(data, &rec); err != nil {
				return api.Run{}, fmt.Errorf("audit event: %w", err)
			}
			logger.Debug("tool call", "event", rec.Event, "tool", rec.Tool, "decision", rec.Decision, "reason", rec.Reason)
		case api.EventApproval:
			var req api.ApprovalRequest
			if err := json.Unmarshal(data, &req); err != nil {
				return api.Run{}, fmt.Errorf("approval event: %w", err)
			}
			answer, err := approver.Approve(ctx, runID, req)
			if err != nil {
				return api.Run{}, err
			}
			if err := c.do(ctx, http.MethodPost, c.path("runs", runID, "approvals", req.ID), answer, nil); err != nil {
				return api.Run{}, err
			}
		case api.EventFinished:
			if err := json.Unmarshal(data, &finished); err != nil {
				return api.Run{}, fmt.Errorf("finished event: %w", err)
			}
			return finished, nil
		}
	}
}

// clientLogger returns the logger of a client command. The client's only
// secret is its token; the server redacts what it sends.
//
// A token shorter than secret.MinLength cannot be redacted reliably, so it
// is refused; the logger returned with that error has no secrets to redact,
// which is safe, as the error does not repeat the token.
func clientLogger(env Env, flags clientFlags) (*slog.Logger, error) {
	redactor, err := secret.NewRedactor([]string{flags.token})
	if err != nil {
		return newLogger(env.Stderr, flags.logLevel, must.Value(secret.NewRedactor(nil))), fmt.Errorf("%s: %w", tokenVar, err)
	}
	return newLogger(env.Stderr, flags.logLevel, redactor), nil
}

// fail logs err under msg at error level and returns exitFailure. Every
// command reports failures through it, so they all go through the command's
// redacting logger rather than straight to stderr.
func fail(logger *slog.Logger, msg string, err error) int {
	logger.Error(msg, "error", err)
	return exitFailure
}
