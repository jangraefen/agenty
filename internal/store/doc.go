// Package store keeps Agenty's state in PostgreSQL: harness versions, runs,
// their transcripts, approval requests, and the audit log. Harnesses belong
// to a workspace, and so do the runs of them.
//
// # Role in the architecture
//
// The store is the only code that talks to the database. The API server
// (package server) calls it for everything it keeps: harnesses applied
// through the API, runs started and followed up, the transcript a run writes
// message by message, approval requests, and the audit views. `agenty serve`
// (package cli) opens it at start. The tool gateway does not import it: it
// writes to the toolgateway.Audit interface, which *Store implements with
// Record, so every record of a tool call lands in the same audit log as the
// store's own events.
//
// # What it contains
//
//   - store.go: Open and its migrations, harness versions, runs and the run
//     queue, conversations, and transcripts.
//   - approvals.go: the approval requests a waiting run is suspended at, and
//     answering, expiring and withdrawing them.
//   - audit.go: the audit log: appending events in the hash chain, the
//     tool gateway's records, the events of runs and harnesses, and the
//     paged reads the export and the three audit views use.
//   - migrations/: the schema, one goose migration embedded in the binary
//     and applied by Open. Until the first release it is rewritten rather
//     than migrated (see docs/IDEA.md).
//   - queries/: the SQL, from which sqlc generates package db (db/), the
//     typed query functions the store calls. The generated code is never
//     edited by hand: `go generate` (via `task generate`) writes it anew.
//   - storetest/: a Store on a fresh database for tests.
//
// # How the parts interact
//
// Every method of Store maps its arguments onto one or more generated
// queries of package db and maps the rows back into the store's own types,
// which hide pgx types from callers. A change that must be recorded in the
// audit log runs through withEvents: the change and its events commit in
// one transaction, or neither does, so the log never misses a change and
// never records one that did not happen.
//
// The runs table is the job queue; there is no queue library. CreateRun
// stores a run as queued; a worker's ClaimRun marks the oldest queued run as
// running with FOR NO KEY UPDATE SKIP LOCKED, so concurrent workers never
// claim one run twice nor wait for each other. A run whose call needs
// approval is suspended as waiting (SuspendRun) and holds no worker;
// answering or expiring its request queues it again, and the next claim
// resumes it. FinishRun, CancelIdleRun and FailRunningRuns end runs. A run is
// never retried, as a tool may not be idempotent.
//
// The audit log is the table audit_events. appendEvent takes a transaction
// advisory lock, reads the latest event's id and hash, and inserts the next
// event with its hash computed by package auditlog over its fields and the
// previous hash. The lock makes appends one at a time, so ids run from 1
// without gaps and the chain never forks; database triggers refuse every
// UPDATE, DELETE and TRUNCATE of the table.
//
// # Trust-model guarantees upheld (numbers as in docs/IDEA.md)
//
//   - 6, every tool call is recorded: Record fails when the record cannot be
//     stored, and the gateway then does not execute the call. Content never
//     makes it fail: text and JSON a model or tool wrote are made storable.
//   - 7, runs are private: a run follows only a run its own starter started,
//     and OwnRun, OwnConversation, Conversations and PendingApprovals find
//     only the user's own.
//   - 8, only auditors read the audit log: the store keeps the auditor-only
//     reads (AuditRuns, AuditRun, ListAuditEvents) apart, and the server
//     checks the role before calling them; the events of runs name how a run
//     started and ended, never its input or output.
//   - 9, the audit log is append-only and tamper-evident: the triggers and
//     the hash chain above.
//
// The store does not redact secrets (guarantee 5): its callers pass content
// already redacted, and a message stored other than as the model saw it is
// marked altered.
package store
