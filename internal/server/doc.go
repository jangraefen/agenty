// Package server is Agenty's HTTP API: it stores harnesses, runs them, and
// streams each run's events, approval requests included, to its clients.
// Runs execute in the server's own process. schema/openapi.yaml defines the
// routes, which oapi-codegen generates the gin interface of into this package.
//
// Every request signs in with a configured user's bearer token, and sees
// only the workspaces that user is a member of.
//
// # Place in the architecture
//
// `agenty serve` builds a Server from the operator config and a store, and
// serves its Handler; the CLI and the web frontend are its clients. The
// server owns no agent logic of its own: package agent runs the loop, package
// toolgateway decides and executes every tool call, package store holds all
// state in PostgreSQL, and package api holds the JSON types. What the server
// adds is who may do what (sign-in, membership, private runs, auditors), the
// job queue that runs agents on workers, durable approvals, conversations
// that continue across runs, and live event streams.
//
// # Files
//
//   - doc.go: this overview.
//   - server.go: Config, Server, New and Close; the gin engine with its
//     global middleware (request logging, loopback-only hosts, CORS) and the
//     helpers every handler answers errors and decodes bodies with.
//   - auth.go: bearer-token sign-in, compared as hashes in constant time,
//     and the workspace membership check the generated routes run.
//   - handlers.go: the routes of the user's own data: themselves, workspaces,
//     conversations, harnesses, runs, approvals, transcripts and event
//     streams.
//   - audit.go: the auditors' routes, the members' and the user's views of
//     the audit log, and the server.started event.
//   - run.go: the run lifecycle: queueing, the workers, preparing and
//     executing a run, suspending it for approval, expiring requests,
//     cancelling, and recording its end; and the agent's audit and transcript
//     sinks, which store before they publish.
//   - history.go: what a follow-up sends the model of the conversation so
//     far: redacted again, provider forms kept only where still valid, runs
//     that failed or were cancelled completed, and a note on tool servers
//     whose state was lost.
//   - hub.go: the in-memory event hub of an unfinished run.
//   - generate.go, api.gen.go: the oapi-codegen directive and its output,
//     ServerInterface with its gin registration. Never edited by hand.
//
// # Request path
//
// A request passes gin's global middleware in this order: recovery,
// logRequests, localOnly (refuses a host that is not loopback, against DNS
// rebinding while there is no TLS), cors (refuses a page of an unlisted
// origin and answers preflights before sign-in, as browsers send those
// without credentials), and authenticate (sets the user, or answers 401).
// The generated router then reads the path parameters and runs member,
// which answers 404 for a workspace the user is not a member of, as for one
// that does not exist, except for the three routes that read a run, whose
// owner may read it in a workspace they left. The handler last: a handler of
// a run calls ownRun first, which finds only runs of the user's own
// conversations, so another user's run is not found either. Handlers reach
// PostgreSQL only through package store, and answer every error through
// fail, which redacts it.
//
// # Run lifecycle
//
// The runs table is the job queue; a run's status moves as follows:
//
//	queued  --worker claims-->            running
//	running --agent answers-->            succeeded
//	running --error, limit, restart-->    failed
//	running --call needs approval-->      waiting   (request stored, no worker held)
//	waiting --answered, or expired-->     queued    (resumes at the call)
//	queued, waiting, running --owner cancels--> cancelled
//
// CreateRun and FollowUpRun call enqueue, which gives the run its hub, stores
// it as queued and signals the wake channel. Workers, runs.workers
// goroutines started by New, block on that channel, then claim queued runs
// oldest first (store.ClaimRun, FOR NO KEY UPDATE SKIP LOCKED) until none is
// left. take prepares a claimed run: its pinned harness version, the
// conversation so far, a model, an agent whose gateway gets the servers
// leased from the pool, and, for a run that waited, the answered request and
// the call counts restored from its audit log. execute then runs the agent.
// A call that needs approval returns agent.Suspended: suspend stores the
// request, publishes it and lets the worker go. Answering the request
// (AnswerApproval) or its expiry (the expire goroutine) queues the run, and
// a worker resumes it at the call, which the gateway decides again under
// central policy as it is then. finish stores how a run ended, publishes
// that as its last event and drops its hub. A restart fails runs left
// running, gives queued and waiting runs their hubs again with what they
// recorded, and cancels those of users who left the run's workspace.
//
// # Goroutines
//
// Besides gin's per-request goroutines, New starts the workers and one
// expire goroutine, which rejects approval requests as each falls due and
// sleeps until the next one, or until suspend wakes it for a new request.
// The pool runs a timer per kept MCP server that stops it once idle. Close
// cancels the context they all share and waits for the workers and expire.
//
// # Event hubs and server-sent events
//
// Every unfinished run of this server has a hub in Server.runs: an
// append-only list of events with a channel that is closed and replaced on
// each publish. runAudit publishes each audit record once it is stored,
// suspend publishes the approval request, and finish or cancelIdle the
// finished event, after which the hub publishes nothing. StreamRunEvents
// sends a hub's events from the first and waits on its channel for more, so
// any number of clients follow a run from its start, whenever they connect.
// A run without a hub has finished, as its end is stored before its hub is
// dropped, and its stream is replayed from the store.
//
// # MCP servers per conversation
//
// One toolgateway.Pool keeps the MCP servers of each conversation between
// its runs, so what they hold outlasts a run, but never for another
// conversation. A run leases them in prepare and hands the lease's servers
// to its gateway, the only code that starts them or calls their tools. When
// the run finishes or suspends they are kept; when it was cancelled while
// running they are stopped, as a call may still run in them. A server that
// had to start anew is named to the model, as its earlier state is gone.
//
// # Trust model
//
// The server upholds IDEA.md's guarantees as follows:
//
//  1. The model is not trusted: a resumed call runs only as the approver saw
//     it, and is decided again by the gateway, never by the server.
//  2. Every side effect goes through the gateway: the server never calls a
//     tool; it hands the gateway the servers and records what it reports.
//  3. Default deny: a run gets the grants of its conversation's pinned
//     harness version, which its gateway enforces.
//  4. Strictest wins: every run compiles central policy from the operator
//     config beside the harness's, and a resumed call's answer settles only
//     require_approval, so a deny under policy as it is now still stops it.
//  5. Credentials never reach the model: the logger, every error answer,
//     every transcript message, the results stored with an approval request
//     and the server.started event pass through the redactor; tokens are
//     kept only as hashes.
//  6. Every tool call is recorded: runAudit stores each record even while
//     the run is being cancelled, and a cancelled run's calls that did not
//     run are recorded as such.
//  7. Runs are private: ownRun, OwnConversation, PendingApprovals and
//     AnswerApproval find only the user's own runs.
//  8. Only auditors read the audit log of every run, without inputs or
//     outputs; members and users see only their workspace's changes and
//     their own actions.
//  9. The audit log is append-only: the server only appends events, and an
//     export ends with a trailer that shows it was not cut short.
package server
