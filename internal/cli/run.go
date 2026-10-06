package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/jangraefen/agenty/internal/api"
	"github.com/jangraefen/agenty/internal/must"
	"github.com/jangraefen/agenty/internal/secret"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

const runUsage = `usage: agenty run [flags] HARNESS INPUT

Runs the latest version of HARNESS on INPUT on the server, asks at the
terminal when a call needs approval, and prints the answer.
`

func run(ctx context.Context, args []string, env Env) int {
	flags, rest, code, ok := parseClientFlags("run", runUsage, 2, args, env.Stderr)
	if !ok {
		return code
	}
	// The client knows no secret; the server redacts what it sends.
	logger := newLogger(env.Stderr, flags.logLevel, must.Value(secret.NewRedactor(nil)))
	c, err := newClient(flags.server)
	if err != nil {
		return fail(logger, "run failed", err)
	}
	var started api.Run
	if err := c.do(ctx, http.MethodPost, "/v1/runs", api.CreateRun{Harness: rest[0], Input: rest[1]}, &started); err != nil {
		return fail(logger, "run failed", err)
	}
	logger.Info("run started", "harness", rest[0], "run_id", started.ID)

	finished, err := follow(ctx, logger, c, started.ID, newTerminalApprover(env.Stdin, env.Stderr, env.Interactive, env.User))
	if err != nil {
		return fail(logger, "run failed", fmt.Errorf("run %s: %w", started.ID, err))
	}
	if finished.Output != "" {
		if _, err := io.WriteString(env.Stdout, finished.Output+"\n"); err != nil {
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
			path := "/v1/runs/" + url.PathEscape(runID) + "/approvals/" + url.PathEscape(req.ID)
			if err := c.do(ctx, http.MethodPost, path, answer, nil); err != nil {
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

func fail(logger *slog.Logger, msg string, err error) int {
	logger.Error(msg, "error", err)
	return exitFailure
}
