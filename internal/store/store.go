package store

// sqlc reads the schema from migrations/ and the queries from queries/, and
// writes package db; its version is pinned so every checkout generates the
// same code.
//go:generate go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate -f ../../sqlc.yaml

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/jangraefen/agenty/internal/auditlog"
	"github.com/jangraefen/agenty/internal/harness"
	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/must"
	"github.com/jangraefen/agenty/internal/store/db"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

// migrations is the schema, embedded so the binary carries it and a server
// never runs against a schema other than the one it was built with.
//
//go:embed migrations/*.sql
var migrations embed.FS

// ErrNotFound is returned when a harness or run does not exist. It is also
// what a caller gets for a row it may not see, such as another user's run or
// one of another workspace, so the API can answer both alike and a missing
// row reveals nothing about others' data.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when a run is to follow a run that another run
// already follows. The server answers it as a conflict: a conversation
// never branches.
var ErrConflict = errors.New("conflict")

// Store is Agenty's state in PostgreSQL. It holds a connection pool, which
// makes it safe for concurrent use by every request handler and worker of
// the server, and the sqlc-generated queries bound to that pool; methods
// that need a transaction bind the queries to it instead (see inTx).
type Store struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

// Open connects to the database at url and applies every migration it has
// not applied yet. Close the Store to close its connections.
//
// Migrating at open, rather than in a separate command, means a server
// never serves an older schema than it was built for; a database from a
// schema that was rewritten before the first release fails here instead.
func Open(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	if err := migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	return &Store{pool: pool, queries: db.New(pool)}, nil
}

// migrate applies the embedded migrations with goose. goose works on
// database/sql, so it gets a *sql.DB that borrows connections from the pgx
// pool, closed again once the migrations ran.
func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	sqlDB := stdlib.OpenDBFromPool(pool)
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, must.Value(fs.Sub(migrations, "migrations")))
	if err != nil {
		return errors.Join(err, sqlDB.Close())
	}
	_, err = provider.Up(ctx)
	return errors.Join(err, sqlDB.Close())
}

// inTx runs f in a transaction, which it commits unless f fails. The
// transaction reads committed data, whatever the database's default, so an
// append to the audit log in it sees the event appended just before it.
//
// Under repeatable read or serializable, the transaction's snapshot would be
// taken before it waits for the audit log's lock, so it would read a stale
// latest event and collide with the append that held the lock on the next
// id. Read committed takes a new snapshot per statement, after the wait.
func (s *Store) inTx(ctx context.Context, f func(*db.Queries) error) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if err := f(s.queries.WithTx(tx)); err != nil {
		return errors.Join(err, tx.Rollback(ctx))
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	return nil
}

// Close closes the database connections. `agenty serve` calls it when it
// stops, and storetest when a test ends.
func (s *Store) Close() {
	s.pool.Close()
}

// HarnessVersion is one stored version of a harness. Versions are immutable:
// a run names the version it ran, and a conversation keeps the version it
// started with, so a builder's later change never alters what a past or
// ongoing conversation ran under.
type HarnessVersion struct {
	// ID identifies the version across all harnesses; runs refer to it.
	ID int64
	// Workspace is the workspace the harness belongs to.
	Workspace string
	// Version counts the harness's versions from 1, per workspace and name.
	Version int
	// Harness is the definition as validated and stored.
	Harness   harness.Harness
	CreatedAt time.Time
}

// PutHarness stores h as the next version of its harness in workspace, after
// validating it. If h equals the latest version, nothing is stored and that version is
// returned, so applying an unchanged harness is a no-op. Concurrent puts of
// one harness are serialized, so each gets its own version.
//
// It compares h with the latest version in their canonical JSON form, takes
// the next version number under a lock on the harness's name, and appends a
// harness.changed event naming user in the same transaction, so a new
// version and its record in the audit log commit together.
func (s *Store) PutHarness(ctx context.Context, workspace, user string, h harness.Harness) (HarnessVersion, error) {
	if err := h.Validate(); err != nil {
		return HarnessVersion{}, fmt.Errorf("store: invalid harness: %w", err)
	}
	definition := canonical(h)

	var stored HarnessVersion
	_, err := s.withEvents(ctx, func(q *db.Queries) ([]auditlog.Event, error) {
		// Serialise puts of this harness until the transaction ends. Without
		// the lock, two puts would both read version N and both insert N+1,
		// and one would fail on the unique version; a row lock cannot help,
		// as the first version has no row to lock yet. A workspace name never
		// holds a slash, so the key names one harness in one workspace.
		if err := q.LockHarnessName(ctx, workspace+"/"+h.Name); err != nil {
			return nil, fmt.Errorf("store: %w", err)
		}
		// Read the latest version, if any: an unchanged harness stores
		// nothing and records no event, so applying a file again is a no-op.
		next := int32(1)
		latest, err := q.LatestHarnessVersion(ctx, db.LatestHarnessVersionParams{Workspace: workspace, Name: h.Name})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return nil, fmt.Errorf("store: %w", err)
		default:
			if stored, err = harnessVersion(latest); err != nil {
				return nil, err
			}
			if bytes.Equal(canonical(stored.Harness), definition) {
				return nil, nil
			}
			next = latest.Version + 1
		}
		row, err := q.InsertHarnessVersion(ctx, db.InsertHarnessVersionParams{Workspace: workspace, Name: h.Name, Version: next, Definition: definition})
		if err != nil {
			return nil, fmt.Errorf("store: %w", err)
		}
		if stored, err = harnessVersion(row); err != nil {
			return nil, err
		}
		return []auditlog.Event{harnessChanged(workspace, user, h.Name, next)}, nil
	})
	if err != nil {
		return HarnessVersion{}, err
	}
	return stored, nil
}

// canonical is the stored form of h: its JSON, with empty lists written as
// null, so that a harness without tools or policy has one form. PutHarness
// compares these bytes to tell an unchanged harness from a changed one;
// encoding/json writes struct fields in a fixed order, so equal harnesses
// give equal bytes.
func canonical(h harness.Harness) []byte {
	if len(h.Tools) == 0 {
		h.Tools = nil
	}
	if len(h.Policy) == 0 {
		h.Policy = nil
	}
	return must.Value(json.Marshal(h))
}

// Harness returns the latest version of the named harness in workspace. A
// new conversation runs it; a follow-up runs its conversation's version
// instead, found by HarnessVersionByID.
func (s *Store) Harness(ctx context.Context, workspace, name string) (HarnessVersion, error) {
	row, err := s.queries.LatestHarnessVersion(ctx, db.LatestHarnessVersionParams{Workspace: workspace, Name: name})
	if err != nil {
		return HarnessVersion{}, notFound("harness "+name, err)
	}
	return harnessVersion(row)
}

// HarnessVersionByID returns the harness version with the given ID, of any
// workspace: callers reach it through a run they may already see, so the ID
// alone selects it.
func (s *Store) HarnessVersionByID(ctx context.Context, id int64) (HarnessVersion, error) {
	row, err := s.queries.HarnessVersionByID(ctx, id)
	if err != nil {
		return HarnessVersion{}, notFound(fmt.Sprintf("harness version %d", id), err)
	}
	return harnessVersion(row)
}

// Harnesses returns the latest version of every harness in workspace, sorted
// by name.
func (s *Store) Harnesses(ctx context.Context, workspace string) ([]HarnessVersion, error) {
	rows, err := s.queries.LatestHarnessVersions(ctx, workspace)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	out := make([]HarnessVersion, 0, len(rows))
	for _, row := range rows {
		v, err := harnessVersion(row)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// harnessVersion maps a stored row to a HarnessVersion, decoding its JSON
// definition. A definition that no longer decodes is an error, not a zero
// harness, so a run never starts with an empty definition.
func harnessVersion(row db.HarnessVersion) (HarnessVersion, error) {
	var h harness.Harness
	if err := json.Unmarshal(row.Definition, &h); err != nil {
		return HarnessVersion{}, fmt.Errorf("store: harness %s version %d: %w", row.Name, row.Version, err)
	}
	return HarnessVersion{ID: row.ID, Workspace: row.Workspace, Version: int(row.Version), Harness: h, CreatedAt: row.CreatedAt}, nil
}

// RunStatus is the state of a run, as the runs table's status column holds
// it; the table's CHECK constraint admits exactly these values.
//
// A run moves queued → running (ClaimRun) → succeeded, failed or cancelled
// (FinishRun). A running run may move to waiting (SuspendRun) and back to
// queued when its approval is answered or expires; a queued or waiting run
// may be cancelled (CancelIdleRun), and a running run left by an earlier
// server is failed (FailRunningRuns).
type RunStatus string

const (
	// RunQueued is a run waiting for a worker.
	RunQueued RunStatus = "queued"
	// RunRunning is a run a worker has claimed and executes.
	RunRunning RunStatus = "running"
	// RunWaiting is a run whose call waits for approval; no worker holds it.
	RunWaiting RunStatus = "waiting"
	// RunSucceeded is a run whose agent loop ended with its output.
	RunSucceeded RunStatus = "succeeded"
	// RunFailed is a run that ended with an error, also one that could not
	// start or that a server restart cut short.
	RunFailed RunStatus = "failed"
	// RunCancelled is a run a user cancelled.
	RunCancelled RunStatus = "cancelled"
)

// Finished reports whether the run has ended: it is neither queued, nor
// running, nor waiting. Only a finished run may be followed up.
func (r Run) Finished() bool {
	return r.Status != RunQueued && r.Status != RunRunning && r.Status != RunWaiting
}

// Run is a stored run: one execution of a harness version, the unit of
// tracing and audit. It carries the names of its harness and version, which
// the runs table refers to only by ID, so callers need no second read.
type Run struct {
	ID string
	// HarnessVersionID is the version the run runs; for a follow-up, the
	// version its conversation started with.
	HarnessVersionID int64
	// Harness and HarnessVersion name the harness version the run runs.
	Harness        string
	HarnessVersion int
	// StartedBy names the user who started the run.
	StartedBy string
	// Input is the user's message the run started with.
	Input  string
	Status RunStatus
	// Output, Steps and Error are written when the run finishes.
	Output    string
	Steps     int
	Error     string
	CreatedAt time.Time
	// FinishedAt is nil until the run has finished.
	FinishedAt *time.Time
	// ConversationID names the conversation the run belongs to by the ID of
	// its first run, which may be this one.
	ConversationID string
	// Follows is the ID of the run this run continues the conversation of,
	// if any.
	Follows string
	// PromptDigest identifies what the run sent the model before the
	// conversation: its model, instructions and tools. HistoryDigest
	// identifies the conversation of earlier runs it sent before its input.
	// A run that never ran has neither.
	PromptDigest, HistoryDigest string
	// Usage sums the usage of the run's stored replies.
	Usage model.Usage
}

// NewRun is a run to store. The caller chooses the ID: the server registers
// the run's event stream under it before storing the run, so a worker that
// claims the run at once finds its stream.
type NewRun struct {
	ID               string
	HarnessVersionID int64
	Input            string
	// StartedBy names the user who starts the run.
	StartedBy string
	// Follows, when set, is the ID of the run whose conversation the run
	// continues.
	Follows string
}

// uniqueViolation is PostgreSQL's error code for a violated unique
// constraint. CreateRun tells a second follow-up of one run by it and the
// constraint's name, runs_follows_key.
const uniqueViolation = "23505"

// CreateRun stores a new queued run and returns it as stored, before a
// worker may claim it. It belongs to the workspace of its harness version. A
// run that follows another joins its conversation. It follows only a run its
// own starter started, so a conversation is one user's: following any other
// run, or none that is stored, returns ErrNotFound. Following a run that
// another run already follows returns ErrConflict, so a conversation never
// branches.
//
// The checks live in the insert itself (see InsertRun in queries/runs.sql)
// and in the unique follows column, not in reads before it, so two
// concurrent follow-ups cannot both pass them. The run.started event is
// appended in the same transaction.
func (s *Store) CreateRun(ctx context.Context, r NewRun) (Run, error) {
	var created Run
	_, err := s.withEvents(ctx, func(q *db.Queries) ([]auditlog.Event, error) {
		row, err := q.InsertRun(ctx, db.InsertRunParams{ID: r.ID, HarnessVersionID: r.HarnessVersionID, Input: r.Input, StartedBy: r.StartedBy, Follows: optional(r.Follows)})
		// Tell the two refusals apart: a violated unique follows means the
		// run is already followed; no row means the insert selected nothing,
		// as the run to follow is not one of this user's.
		var pgErr *pgconn.PgError
		switch {
		case errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == "runs_follows_key":
			return nil, fmt.Errorf("store: run %s: run %s is already followed: %w", r.ID, r.Follows, ErrConflict)
		case errors.Is(err, pgx.ErrNoRows):
			return nil, fmt.Errorf("store: run %s: run %s of %s: %w", r.ID, r.Follows, r.StartedBy, ErrNotFound)
		case err != nil:
			return nil, fmt.Errorf("store: run %s: %w", r.ID, err)
		}
		created = run(row.Run, row.Harness, row.HarnessVersion)
		return []auditlog.Event{runStarted(created, row.Workspace)}, nil
	})
	if err != nil {
		return Run{}, err
	}
	return created, nil
}

// ClaimedRun is a run a worker claimed, and its workspace, which together
// name the run's event stream; the rest the worker reads when it prepares
// the run.
type ClaimedRun struct {
	ID, Workspace string
}

// ClaimRun marks the oldest queued run as running and returns it. It reports
// false if no run is queued. Each run is claimed once, also by concurrent
// claims.
//
// It is the dequeue of the job queue the runs table is: one statement that
// picks the oldest queued row with FOR NO KEY UPDATE SKIP LOCKED and marks
// it running, so a claim neither waits for another worker's claim nor takes
// the same run. The server's workers call it when woken, until it reports
// false.
func (s *Store) ClaimRun(ctx context.Context) (ClaimedRun, bool, error) {
	row, err := s.queries.ClaimRun(ctx)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ClaimedRun{}, false, nil
	case err != nil:
		return ClaimedRun{}, false, fmt.Errorf("store: claim a run: %w", err)
	}
	return ClaimedRun{ID: row.ID, Workspace: row.Workspace}, true, nil
}

// IdleRun is a run no worker holds that has not finished, queued or waiting,
// with its workspace and harness. A starting server reads these to give
// every such run its event stream again, and to cancel those of users who
// are no longer members of the run's workspace.
type IdleRun struct {
	ID        string
	Status    RunStatus
	Workspace string
	Harness   string
	// Owner started the run, and so its conversation.
	Owner string
}

// IdleRuns returns the queued and waiting runs, oldest first, the order in
// which workers claim the queued ones.
func (s *Store) IdleRuns(ctx context.Context) ([]IdleRun, error) {
	rows, err := s.queries.IdleRuns(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: idle runs: %w", err)
	}
	out := make([]IdleRun, len(rows))
	for i, r := range rows {
		out[i] = IdleRun{ID: r.ID, Status: RunStatus(r.Status), Workspace: r.Workspace, Harness: r.Harness, Owner: r.Owner}
	}
	return out, nil
}

// Closing is what a cancelled run did not do, recorded before its end: the
// tool gateway's records of the calls that did not run, and the messages of
// its transcript that say so.
type Closing struct {
	Records  []toolgateway.Record
	Messages []NewMessage
}

// ClosingReader reads what a Closer needs of a run.
type ClosingReader interface {
	LatestApproval(ctx context.Context, runID string) (Approval, bool, error)
	Transcript(ctx context.Context, runID string) ([]TranscriptMessage, error)
	AuditRecords(ctx context.Context, runID string) ([]AuditRecord, error)
}

// Closer returns the Closing of the run being cancelled, read from tx, the
// run as the cancel's transaction sees it.
type Closer func(ctx context.Context, tx ClosingReader) (Closing, error)

// CancelIdleRun records a queued or waiting run as cancelled, giving why, and
// withdraws the approval request it waits for. It reports false if the run
// is neither, such as one a worker has claimed. If closing is set, what it
// returns is recorded in the same transaction, before the run's end. A
// closing that fails does not stop the cancel: the run is cancelled without
// it, and the error says why.
func (s *Store) CancelIdleRun(ctx context.Context, id, why string, closing Closer) (bool, error) {
	cancelled := false
	var closeErr error
	_, err := s.withEvents(ctx, func(q *db.Queries) ([]auditlog.Event, error) {
		// The request first, then the run, as AnswerApproval and
		// ExpireApprovals lock them: one order for all three means they
		// never wait for each other in a circle and deadlock.
		if err := q.WithdrawApprovals(ctx, db.WithdrawApprovalsParams{RunID: id, Reason: why}); err != nil {
			return nil, fmt.Errorf("store: run %s: %w", id, err)
		}
		rows, err := q.CancelIdleRun(ctx, db.CancelIdleRunParams{ID: id, Error: why})
		if err != nil || len(rows) == 0 {
			return nil, wrapRun(id, err)
		}
		cancelled = true
		finished := runFinished(rows[0].Workspace, id, RunCancelled, rows[0].Steps, why)
		if closing == nil {
			return []auditlog.Event{finished}, nil
		}
		events, err := s.closeOut(ctx, q, id, closing)
		if err != nil {
			closeErr = fmt.Errorf("store: run %s is cancelled, but what it did not do is not recorded: %w", id, err)
			events = nil
		}
		return append(events, finished), nil
	})
	if err != nil {
		return false, err
	}
	return cancelled, closeErr
}

// closeOut stores the messages of the Closing of the run id that closing
// returns, read and stored in q's transaction, and returns the events of
// its records. It stores nothing if it fails: the transaction rolls back to
// a savepoint.
func (s *Store) closeOut(ctx context.Context, q *db.Queries, id string, closing Closer) ([]auditlog.Event, error) {
	// Only the reads the closer is given, and AppendMessage below, use it.
	tx := &Store{queries: q}
	if err := q.SavepointClosing(ctx); err != nil {
		return nil, err
	}
	events, err := func() ([]auditlog.Event, error) {
		c, err := closing(ctx, tx)
		if err != nil {
			return nil, err
		}
		owner, err := q.RunOwner(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, m := range c.Messages {
			if err := tx.AppendMessage(ctx, id, m); err != nil {
				return nil, err
			}
		}
		events := make([]auditlog.Event, len(c.Records))
		for i, rec := range c.Records {
			if rec.RunID != id {
				return nil, fmt.Errorf("a record of run %s", rec.RunID)
			}
			if events[i], err = toolEvent(rec, owner.StartedBy, owner.Workspace); err != nil {
				return nil, err
			}
		}
		return events, nil
	}()
	if err != nil {
		return nil, errors.Join(err, q.RollbackToClosing(ctx))
	}
	return events, nil
}

// SetRunDigests records what a running run sends the model; see Run. A
// later follow-up compares them with its own to tell whether the replies of
// this run, in the provider's own form, may be sent again as they were.
// Only a running run takes them, so a finished run's digests stay as they
// were when it ran.
func (s *Store) SetRunDigests(ctx context.Context, id, prompt, history string) error {
	n, err := s.queries.SetRunDigests(ctx, db.SetRunDigestsParams{ID: id, PromptDigest: prompt, HistoryDigest: history})
	switch {
	case err != nil:
		return fmt.Errorf("store: run %s: %w", id, err)
	case n == 0:
		return fmt.Errorf("store: running run %s: %w", id, ErrNotFound)
	}
	return nil
}

// FinishRun records the end of a running run. Finishing a run that does not
// exist or has already finished returns ErrNotFound.
//
// The update matches only a running run, so a run ends once: a run that was
// already ended otherwise is not ended again. The run.finished event, which
// names the status, steps and error but never the output, is appended in
// the same transaction.
func (s *Store) FinishRun(ctx context.Context, id string, status RunStatus, output string, steps int, runErr string) error {
	if status == RunRunning || status == RunQueued {
		return fmt.Errorf("store: run %s: cannot finish as %s", id, status)
	}
	_, err := s.withEvents(ctx, func(q *db.Queries) ([]auditlog.Event, error) {
		workspace, err := q.FinishRun(ctx, db.FinishRunParams{ID: id, Status: string(status), Output: output, Steps: int32(steps), Error: runErr}) //nolint:gosec // G115: steps is bounded by the harness's max_steps.
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil, fmt.Errorf("store: running run %s: %w", id, ErrNotFound)
		case err != nil:
			return nil, fmt.Errorf("store: run %s: %w", id, err)
		}
		return []auditlog.Event{runFinished(workspace, id, status, int32(steps), runErr)}, nil //nolint:gosec // G115: as above.
	})
	return err
}

// Run returns the run with the given ID in workspace. A run of another
// workspace is not found. It does not check who started the run; the
// server uses OwnRun where privacy applies.
func (s *Store) Run(ctx context.Context, workspace, id string) (Run, error) {
	row, err := s.queries.GetRun(ctx, db.GetRunParams{ID: id, Workspace: workspace})
	if err != nil {
		return Run{}, notFound("run "+id, err)
	}
	return run(row.Run, row.Harness, row.HarnessVersion), nil
}

// Conversation returns the runs of the conversation that the run with the
// given ID in workspace belongs to, oldest first. A run of another workspace
// is not found.
//
// The query finds the conversation through the given run's
// conversation_id, so any run of the conversation names it, and lists the
// runs by creation, which is the order in which they follow one another.
func (s *Store) Conversation(ctx context.Context, workspace, id string) ([]Run, error) {
	rows, err := s.queries.ConversationRuns(ctx, db.ConversationRunsParams{Workspace: workspace, ID: id})
	switch {
	case err != nil:
		return nil, fmt.Errorf("store: conversation of run %s: %w", id, err)
	case len(rows) == 0:
		return nil, fmt.Errorf("store: run %s: %w", id, ErrNotFound)
	}
	out := make([]Run, len(rows))
	for i, row := range rows {
		out[i] = run(row.Run, row.Harness, row.HarnessVersion)
	}
	return out, nil
}

// OwnRun returns the run with the given ID in workspace, if user started its
// conversation. Anyone else's run, as one of another workspace, is not
// found.
//
// This is the read behind trust-model guarantee 7: the user is part of the
// query, so another user's run is not found rather than forbidden, and its
// existence is not revealed.
func (s *Store) OwnRun(ctx context.Context, workspace, user, id string) (Run, error) {
	row, err := s.queries.GetOwnRun(ctx, db.GetOwnRunParams{ID: id, Workspace: workspace, Owner: user})
	if err != nil {
		return Run{}, notFound("run "+id, err)
	}
	return run(row.Run, row.Harness, row.HarnessVersion), nil
}

// AuditRun is a run as auditors see it, with its workspace, as an auditor
// reads runs across every workspace. It holds the whole run, but the API's
// audit run has no field for its input or output, so auditors never get
// them (trust-model guarantee 8).
type AuditRun struct {
	Run
	Workspace string
}

// RunFilter selects runs to list for auditors. Pages are keyed by a run
// rather than by an offset, so a run started while an auditor pages does
// not shift the pages after it.
type RunFilter struct {
	// Workspace, Harness, StartedBy and Status, when set, select the runs of
	// that workspace or harness, started by that user, or with that status.
	Workspace string
	Harness   string
	StartedBy string
	Status    RunStatus
	// Before, when set, lists the runs after the run with that ID, in the
	// order AuditRuns returns them: a page after one ending with that run.
	Before string
	// Limit is the most runs to return; it must be greater than 0.
	Limit int
}

// AuditRuns lists the runs of every workspace that f selects, newest first.
// Only auditors may see them; the server checks the role before it calls.
func (s *Store) AuditRuns(ctx context.Context, f RunFilter) ([]AuditRun, error) {
	// The limit becomes an int32 query argument; out of range is a caller's
	// mistake, not a reason to wrap around.
	if f.Limit <= 0 || f.Limit > math.MaxInt32 {
		return nil, fmt.Errorf("store: runs: limit %d is out of range", f.Limit)
	}
	rows, err := s.queries.ListAuditRuns(ctx, db.ListAuditRunsParams{
		Workspace: optional(f.Workspace),
		Harness:   optional(f.Harness),
		StartedBy: optional(f.StartedBy),
		Status:    optional(string(f.Status)),
		Before:    optional(f.Before),
		MaxRows:   int32(f.Limit),
	})
	if err != nil {
		return nil, fmt.Errorf("store: runs: %w", err)
	}
	out := make([]AuditRun, len(rows))
	for i, row := range rows {
		out[i] = AuditRun{Run: run(row.Run, row.Harness, row.HarnessVersion), Workspace: row.Workspace}
	}
	return out, nil
}

// AuditRun returns the run with the given ID, of any workspace. Only
// auditors may see it; the server checks the role before it calls.
func (s *Store) AuditRun(ctx context.Context, id string) (AuditRun, error) {
	row, err := s.queries.GetAuditRun(ctx, id)
	if err != nil {
		return AuditRun{}, notFound("run "+id, err)
	}
	return AuditRun{Run: run(row.Run, row.Harness, row.HarnessVersion), Workspace: row.Workspace}, nil
}

// ConversationSummary is a conversation as a list shows it, named by the ID
// of its first run. A conversation is not a table of its own: it is derived
// from the runs that follow one another, so its fields come from its first
// run (ID, workspace, harness, title) and its latest (status, activity).
type ConversationSummary struct {
	ID        string
	Workspace string
	Harness   string
	// Title is the first run's input, cut to its first 100 characters.
	Title string
	// Status is the latest run's status.
	Status RunStatus
	// UpdatedAt is when the conversation's latest run was created. Only
	// Conversations sets it.
	UpdatedAt time.Time
}

// ConversationFilter selects conversations to list. The cursor carries the
// previous page's last UpdatedAt itself rather than a run to look it up by,
// as a follow-up moves a conversation's UpdatedAt between page reads.
type ConversationFilter struct {
	// User started the conversations, in any workspace.
	User string
	// BeforeAt and BeforeID, when BeforeID is set, list the conversations
	// after the one with that UpdatedAt and ID, in the order Conversations
	// returns them.
	BeforeAt time.Time
	BeforeID string
	// Limit is the most conversations to return; it must be greater than 0.
	Limit int
}

// Conversations lists the conversations f selects, latest activity first.
// They are the user's own, in every workspace, those they left too, so the
// list never shows another user's (trust-model guarantee 7).
func (s *Store) Conversations(ctx context.Context, f ConversationFilter) ([]ConversationSummary, error) {
	if f.Limit <= 0 || f.Limit > math.MaxInt32 {
		return nil, fmt.Errorf("store: conversations: limit %d is out of range", f.Limit)
	}
	// BeforeID decides whether there is a cursor; a zero BeforeAt alone is
	// a valid time, so it cannot.
	var before *time.Time
	if f.BeforeID != "" {
		before = &f.BeforeAt
	}
	rows, err := s.queries.ListConversations(ctx, db.ListConversationsParams{
		StartedBy: f.User,
		BeforeID:  optional(f.BeforeID),
		BeforeAt:  before,
		MaxRows:   int32(f.Limit),
	})
	if err != nil {
		return nil, fmt.Errorf("store: conversations: %w", err)
	}
	out := make([]ConversationSummary, len(rows))
	for i, row := range rows {
		out[i] = ConversationSummary{
			ID:        row.ID,
			Workspace: row.Workspace,
			Harness:   row.Harness,
			Title:     row.Title,
			Status:    RunStatus(row.Status),
			UpdatedAt: row.UpdatedAt,
		}
	}
	return out, nil
}

// OwnConversation returns the conversation named by id, if user started it,
// in any workspace, with its runs, oldest first. A later run of a
// conversation does not name it, and anyone else's conversation is not
// found. Its status is that of the last run returned, so the two agree
// though a run changes between the reads.
//
// It reads in two steps: FindConversation checks that id names a
// conversation user started, and only then Conversation reads its runs.
func (s *Store) OwnConversation(ctx context.Context, user, id string) (ConversationSummary, []Run, error) {
	row, err := s.queries.FindConversation(ctx, db.FindConversationParams{ID: id, StartedBy: user})
	if err != nil {
		return ConversationSummary{}, nil, notFound("conversation "+id, err)
	}
	runs, err := s.Conversation(ctx, row.Workspace, row.ID)
	if err != nil {
		return ConversationSummary{}, nil, err
	}
	return ConversationSummary{
		ID:        row.ID,
		Workspace: row.Workspace,
		Harness:   row.Harness,
		Title:     row.Title,
		Status:    runs[len(runs)-1].Status,
	}, runs, nil
}

// optional is s as a nullable query argument: empty is NULL. The filter
// queries read a NULL argument as "any", and a nullable column, such as
// follows, stores NULL for none.
func optional(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: s != ""}
}

// run maps a stored row and its harness's name and version to a Run,
// widening database integer types and unwrapping nullable columns, so no
// pgx type reaches the callers.
func run(row db.Run, harness string, version int32) Run {
	return Run{
		ID:               row.ID,
		HarnessVersionID: row.HarnessVersionID,
		Harness:          harness,
		HarnessVersion:   int(version),
		StartedBy:        row.StartedBy,
		Input:            row.Input,
		Status:           RunStatus(row.Status),
		Output:           row.Output,
		Steps:            int(row.Steps),
		Error:            row.Error,
		CreatedAt:        row.CreatedAt,
		FinishedAt:       row.FinishedAt,
		ConversationID:   row.ConversationID,
		Follows:          row.Follows.String,
		PromptDigest:     row.PromptDigest,
		HistoryDigest:    row.HistoryDigest,
		Usage: model.Usage{
			InputTokens:      row.InputTokens,
			OutputTokens:     row.OutputTokens,
			CacheWriteTokens: row.CacheWriteTokens,
			CacheReadTokens:  row.CacheReadTokens,
		},
	}
}

// FailRunningRuns marks every run that is still running as failed with
// reason, and returns how many it marked. A server calls it on startup: a
// run cannot outlive the process that ran it.
//
// This assumes one server per database: a run marked running belonged to
// the earlier process, whose goroutine is gone. A run is failed, never
// queued again, as a tool it called may not be idempotent. Each run failed
// gets its run.finished event in the same transaction.
func (s *Store) FailRunningRuns(ctx context.Context, reason string) (int64, error) {
	var n int64
	_, err := s.withEvents(ctx, func(q *db.Queries) ([]auditlog.Event, error) {
		failed, err := q.FailRunningRuns(ctx, reason)
		if err != nil {
			return nil, fmt.Errorf("store: %w", err)
		}
		n = int64(len(failed))
		events := make([]auditlog.Event, len(failed))
		for i, r := range failed {
			events[i] = runFinished(r.Workspace, r.ID, RunFailed, r.Steps, reason)
		}
		return events, nil
	})
	return n, err
}

// text makes s storable in a text column, which accepts neither NUL bytes
// nor invalid UTF-8: both become U+FFFD. Text that a model or tool wrote is
// untrusted and may hold either; replacing them, rather than failing, means
// such content can never make a record or message fail to store.
func text(s string) string {
	return strings.ReplaceAll(strings.ToValidUTF8(s, "\uFFFD"), "\x00", "\uFFFD")
}

// jsonText makes raw storable in a json column: empty stays NULL, invalid
// UTF-8 becomes U+FFFD, and anything that is then still not valid JSON, such
// as text with a raw NUL byte, becomes a JSON string, where json.Marshal
// escapes the NUL. Valid JSON is kept byte for byte, not re-encoded, so key
// order and a provider's exact form survive.
func jsonText(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return nil
	}
	valid := []byte(strings.ToValidUTF8(string(raw), "\uFFFD"))
	if json.Valid(valid) {
		return valid
	}
	return must.Value(json.Marshal(string(valid)))
}

// notFound wraps err for what was looked up, turning pgx's "no rows" into
// ErrNotFound, which callers test for, and leaving every other error as an
// error of the database.
func notFound(what string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("store: %s: %w", what, ErrNotFound)
	}
	return fmt.Errorf("store: %s: %w", what, err)
}

// TranscriptMessage is a stored message of a run's transcript. The
// transcript is what users see of a run, and what a follow-up sends the
// model as the conversation so far.
type TranscriptMessage struct {
	// Position is the message's index in the run's conversation.
	Position int
	model.Message
	// Altered is set for a message stored other than as the model saw or
	// wrote it.
	Altered   bool
	CreatedAt time.Time
}

// NewMessage is a message to append to a run's transcript.
type NewMessage struct {
	// Position is the message's index in the run's conversation.
	Position int
	Message  model.Message
	// Altered says that Message is not as the model saw or wrote it, for
	// example because secrets were redacted from it.
	Altered bool
}

// AppendMessage stores m in the transcript of the run runID, which must
// exist, and adds its usage to the run's. A position is written once. As
// with Record, content never makes storing fail: invalid UTF-8 and NUL bytes
// in the text are replaced, and tool call arguments and provider parts that
// are not valid JSON are kept as a JSON string; a message changed so is
// stored as altered. A provider part's JSON is otherwise kept as written, so
// the provider can replay it exactly. A usage of all zeros is read back as
// none.
//
// Messages are stored one at a time as the run goes, not at its end, so a
// run cut short still leaves what it said, and a follow-up continues from
// there.
func (s *Store) AppendMessage(ctx context.Context, runID string, m NewMessage) error {
	msg, position := m.Message, m.Position
	// Make every field storable, and note whether doing so changed it: a
	// follow-up sends the provider's own form only for messages stored as
	// the model saw them.
	altered := m.Altered || text(msg.Text) != msg.Text
	// Tool results are stored through jsonList, where json.Marshal replaces
	// invalid UTF-8, so only that changes them.
	for _, r := range msg.ToolResults {
		altered = altered || !utf8.ValidString(r.Content)
	}
	calls := make([]model.ToolCall, len(msg.ToolCalls))
	for i, c := range msg.ToolCalls {
		c.Args = jsonText(c.Args)
		altered = altered || !bytes.Equal(c.Args, msg.ToolCalls[i].Args)
		calls[i] = c
	}
	var provider string
	var providerData []byte
	// A provider part without a name could never be matched to its
	// provider again, so it is refused rather than stored.
	if p := msg.Provider; p != nil {
		if p.Name == "" {
			return fmt.Errorf("store: run %s: message %d: provider part without a name", runID, position)
		}
		provider, providerData = text(p.Name), jsonText(p.Data)
		altered = altered || !bytes.Equal(providerData, p.Data)
	}
	params := db.InsertRunMessageParams{
		RunID:        text(runID),
		Position:     int32(position), //nolint:gosec // G115: a position is bounded by the harness's max_steps.
		Role:         text(string(msg.Role)),
		Text:         text(msg.Text),
		ToolCalls:    jsonList(calls),
		ToolResults:  jsonList(msg.ToolResults),
		Provider:     provider,
		ProviderData: providerData,
		Altered:      altered,
	}
	if u := msg.Usage; u != nil {
		params.InputTokens, params.OutputTokens = u.InputTokens, u.OutputTokens
		params.CacheWriteTokens, params.CacheReadTokens = u.CacheWriteTokens, u.CacheReadTokens
	}
	// One statement inserts the message and adds its tokens to the run's
	// totals, so the totals always equal the sum of the stored replies.
	err := s.queries.InsertRunMessage(ctx, params)
	if err != nil {
		return fmt.Errorf("store: run %s: message %d: %w", runID, position, err)
	}
	return nil
}

// jsonList encodes a list for a json column; an empty list stays NULL.
// json.Marshal replaces invalid UTF-8 and escapes NUL, which json accepts.
func jsonList[T any](list []T) []byte {
	if len(list) == 0 {
		return nil
	}
	return must.Value(json.Marshal(list))
}

// Transcript returns the stored transcript of a run, in order. A run without
// messages, or one that does not exist, has an empty transcript.
func (s *Store) Transcript(ctx context.Context, runID string) ([]TranscriptMessage, error) {
	rows, err := s.queries.RunMessages(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("store: run %s: transcript: %w", runID, err)
	}
	out := make([]TranscriptMessage, len(rows))
	for i, row := range rows {
		msg := model.Message{Role: model.Role(row.Role), Text: row.Text}
		if row.ToolCalls != nil {
			if err := json.Unmarshal(row.ToolCalls, &msg.ToolCalls); err != nil {
				return nil, fmt.Errorf("store: run %s: message %d: tool calls: %w", runID, row.Position, err)
			}
		}
		if row.ToolResults != nil {
			if err := json.Unmarshal(row.ToolResults, &msg.ToolResults); err != nil {
				return nil, fmt.Errorf("store: run %s: message %d: tool results: %w", runID, row.Position, err)
			}
		}
		if row.Provider != "" {
			msg.Provider = &model.ProviderPart{Name: row.Provider, Data: row.ProviderData}
		}
		// Only replies carry usage; all zeros was stored for none.
		if u := (model.Usage{InputTokens: row.InputTokens, OutputTokens: row.OutputTokens, CacheWriteTokens: row.CacheWriteTokens, CacheReadTokens: row.CacheReadTokens}); u != (model.Usage{}) {
			msg.Usage = &u
		}
		out[i] = TranscriptMessage{Position: int(row.Position), Message: msg, Altered: row.Altered, CreatedAt: row.CreatedAt}
	}
	return out, nil
}
