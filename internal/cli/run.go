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
	"github.com/jangraefen/agenty/internal/toolgateway"
)

const runUsage = `usage: agenty run [flags] HARNESS INPUT

Runs the latest version of HARNESS on INPUT on the server, asks at the
terminal when a call needs approval, and prints the answer. Approvals are
recorded as given by the signed-in user. Interrupting the command cancels
the run.
`

func run(ctx context.Context, args []string, env Env) int {
	flags, rest, code, ok := parseClientFlags("run", runUsage, 2, args, env)
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

	finished, err := follow(ctx, logger, c, started.ID, newTerminalApprover(env.Stdin, env.Stderr, env.Interactive))
	if err != nil && ctx.Err() != nil {
		// Interrupted: the run is not left behind on the server.
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
	if finished.Status != api.RunSucceeded {
		logger.Error("run failed", "run_id", finished.ID, "steps", finished.Steps, "error", finished.Error)
		return exitFailure
	}
	logger.Info("run finished", "run_id", finished.ID, "steps", finished.Steps)
	return exitOK
}

// follow reads a run's events until it finishes, logs its tool calls, and
// answers its approval requests at the terminal. It returns the finished
// run.
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
			var rec toolgateway.Record
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
func clientLogger(env Env, flags clientFlags) (*slog.Logger, error) {
	redactor, err := secret.NewRedactor([]string{flags.token})
	if err != nil {
		return newLogger(env.Stderr, flags.logLevel, must.Value(secret.NewRedactor(nil))), fmt.Errorf("%s: %w", tokenVar, err)
	}
	return newLogger(env.Stderr, flags.logLevel, redactor), nil
}

func fail(logger *slog.Logger, msg string, err error) int {
	logger.Error(msg, "error", err)
	return exitFailure
}
