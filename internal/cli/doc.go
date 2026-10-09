// Package cli is the agenty command line. "agenty serve" runs the server,
// which keeps harnesses, runs and the audit log in PostgreSQL and runs
// agents. "agenty apply", "agenty run" and "agenty audit export" are its
// clients: they store a harness, run one, answering approvals at the
// terminal, and export the audit log, which "agenty audit verify" checks
// offline. Clients sign in with the token in AGENTY_TOKEN; apply and run
// work in one workspace.
//
// # Place in the architecture
//
// The package sits between the process (cmd/agenty) and the rest of
// Agenty. It owns no domain logic of its own: the server, the store, policy
// and the tool gateway live in their own packages, and this package wires
// them together for "serve" and speaks the HTTP API for every other
// command. Main dispatches on the first argument; Env carries the process
// around a command, so tests run every command in-process with their own
// streams, environment and tool servers.
//
// # agenty serve
//
// serve builds the server in a fixed order, each step depending on the one
// before:
//
//  1. a logger with an empty redactor, so even a failure to load the config
//     is logged through the redacting handler;
//  2. the listen address is checked to be loopback, before anything else is
//     opened, as the API has no TLS yet;
//  3. config.Load reads and validates the operator config, agenty.yaml, with
//     its central policy files;
//  4. Config.Resolve reads the {env: NAME} values (model API key, database
//     URL, MCP server environments, user tokens) and builds the redactor for
//     them, which replaces the first logger;
//  5. store.Open connects to PostgreSQL and applies pending migrations;
//  6. server.New compiles central policy, records server.started in the
//     audit log, marks the runs an earlier server left running as failed,
//     takes up those it left queued or waiting, and starts its workers;
//  7. the HTTP listener is opened, and the address it resolved to is checked
//     to be loopback again.
//
// On interrupt the server's runs are stopped first, so their event streams
// end and the HTTP server can shut down within its timeout.
//
// # Client commands
//
// apply, run and audit export are thin API clients (client.go): they share
// parseFlags, which adds --server and --log-level, reads the token from
// AGENTY_TOKEN and, for apply and run, the workspace from --workspace or
// AGENTY_WORKSPACE. run starts a run, follows its server-sent events, and
// answers approval requests through terminalApprover; on interrupt it
// cancels the run on the server. audit verify is the one command that needs
// no server: it checks an export's hash chain with package auditlog.
//
// # Trust-model guarantees upheld here
//
// The numbers refer to the trust model in docs/IDEA.md.
//
//   - Guarantee 5, credentials never reach logs: serve logs through the
//     redactor of every value read from the environment, and each client
//     logs through a redactor of its own token. The token is read from the
//     environment only, never a flag, so it stays out of shell history and
//     process lists.
//   - Guarantee 7, runs are private: run answers approvals as the signed-in
//     user, the only one the server lets answer them.
//   - Guarantee 9, the audit log is tamper-evident: audit export fails unless
//     the server marked the export complete, and audit verify checks the
//     chain and the anchors kept from earlier exports.
//   - Guarantee 1, the model is not trusted: text the model chose, such as a
//     tool's arguments in an approval prompt or the run's answer, is escaped
//     before it reaches the terminal, so it cannot send control sequences;
//     and an approval is given only by a person at a terminal, never by piped
//     input.
//
// Only "serve" starts tool servers, inside the server's tool gateway
// (guarantee 2); the client commands never execute a tool.
package cli
