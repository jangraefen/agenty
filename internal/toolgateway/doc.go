// Package toolgateway is the single path every tool call takes.
//
// # Role in the architecture
//
// The agent loop (internal/agent) never executes a tool itself: it builds one
// Gateway per run, from the run's harness grants, the run's tool servers, a
// policy engine (internal/policy) and an Audit sink, and hands every tool call
// the model asks for to Gateway.Call. The API server (internal/server) wires
// the pieces together: it builds the tool servers from operator configuration
// (internal/mcptool), leases them from a Pool for the run's conversation, and
// passes an Audit that stores each record in the database, where the run's
// event stream reads it, and wakes that stream's readers. The gateway itself depends only on internal/secret,
// for redaction, and on the interfaces it declares: Tool, ToolServer,
// ToolSession, Policy and Audit. It knows nothing of MCP, OPA or PostgreSQL,
// so every implementation of those sits behind the same checks, and tests
// substitute the fakes in gatewaytest.
//
// # What it contains
//
//   - gateway.go: Gateway, its Config, and the call path: Call, Resume and the
//     decide, record and execute steps they share.
//   - policy.go: the Policy interface, the Request document policy is written
//     against, the Verdict it returns, and the Suspended error and Approval
//     answer of a call that waits for a person.
//   - audit.go: the Decision and Event vocabulary, the Record format of the
//     audit log, and the Audit interface.
//   - servers.go: the ToolServer and ToolSession interfaces, and starting and
//     stopping the servers of a run all at once.
//   - pool.go: Pool and Lease, which keep a conversation's started servers
//     between its runs without letting any other code hold them.
//
// # How the components interact
//
// New validates the grants against the configured servers, starts only the
// servers that serve a granted tool, and keeps only the granted tools of
// those servers; everything else is unreachable from that point on. Each Call
// then runs the same fixed sequence:
//
//  1. decide: deny if the gateway is closed, the tool is not granted, or the
//     run's call limit is reached; otherwise ask Policy, and deny on any
//     policy error or unknown decision.
//  2. record the decision. If recording fails, the call stops there.
//  3. On require_approval, return a *Suspended error instead of executing:
//     the run stops, a person answers, and Resume decides the call again
//     under current policy and records the answer before going ahead.
//  4. execute: call the tool, redact its result and error, and record the
//     result.
//
// A Pool sits outside this sequence. The server leases a conversation's
// servers for one run and hands the lease's servers to the gateway as its
// ToolServers; starting one takes the conversation's kept session if it still
// answers, and stopping one hands it back to the lease instead of ending the
// process. The gateway does not know whether its servers are pooled.
//
// # Trust-model guarantees
//
// The package upholds the guarantees numbered in docs/IDEA.md:
//
//   - 2, every side effect goes through the gateway: only this package calls
//     Tool.Call, starts tool servers or lists their tools, which the
//     forbidigo rules in .golangci.yml enforce for every other package.
//   - 3, default deny: an ungranted tool is never even listed, so decide
//     denies it before policy runs, and anything short of a clear allow,
//     including a policy error, is a denial.
//   - 4, strictest wins: policy is asked only about granted calls and can
//     only deny or require approval; it cannot grant.
//   - 5, credentials never reach the model: the Redactor in Config is
//     required, and every result, error, denial reason, approval request and
//     audit record passes through it. Tools still receive their arguments
//     unchanged.
//   - 6, every tool call is recorded: a decision, and an approval where one is
//     needed, is recorded before anything executes, and a call is never
//     executed if that record fails.
//
// Tests named TestInvariant_ pin them: default deny and every call recorded
// in invariant_test.go, approval and resumption in suspend_test.go, and the
// pool's per-conversation isolation in pool_test.go; redact_test.go covers
// redaction. Strictest wins is pinned in internal/policy.
package toolgateway
