# Agenty

Agenty is an open-source, self-hostable platform for building governed AI agent harnesses. Teams define agents from building blocks the enterprise approves, and every action an agent takes is checked against policy and recorded, without the builder having to think about it. [docs/IDEA.md](docs/IDEA.md) holds the idea, the trust model and the decisions behind each stage; this page shows how the pieces fit together.

The bet is that governance can live in one deterministic place, the path every tool call takes, rather than in the model or the builder's diligence. Most of the architecture below follows from that.

## Trying it

Needs [Task](https://taskfile.dev), Docker for the database, Node.js for the example's filesystem MCP server, and `ANTHROPIC_API_KEY` in the environment or a `.env` file at the module root.

```sh
task serve     # starts PostgreSQL, builds agenty, serves the notes example
task demo      # in a second terminal: applies the notes harness and runs it
task web:dev   # the web frontend, at http://127.0.0.1:5173
```

`task check` runs what CI runs; `task --list` shows the rest.

## Architecture

### The system and its boundaries

One Go binary, `agenty serve`, holds all state in PostgreSQL and runs agents in its own process. The web frontend and the CLI are both clients of its JSON API, which `schema/openapi.yaml` defines. Two people configure it, each in their own file: the operator owns `agenty.yaml` (model provider, MCP servers, central policy, users, workspaces), and the builder owns a harness (instructions, model, granted tools, limits, harness policy). Builders never see a credential.

```mermaid
flowchart LR
    user(["User<br/>chats with harnesses"])
    auditor(["Auditor<br/>reads the audit log"])
    builder(["Builder<br/>writes harnesses"])
    operator(["Operator<br/>writes agenty.yaml"])

    subgraph clients["Clients of the JSON API"]
        web["Web frontend<br/>web/ · React + TanStack"]
        cli["CLI<br/>agenty apply · run · audit"]
    end

    subgraph serve["agenty serve · one Go process"]
        api["HTTP API<br/>internal/server"]
        loop["Agent loop<br/>internal/agent"]
        gw["Tool Gateway<br/>internal/toolgateway"]
        pol["Policy · OPA<br/>internal/policy"]
    end

    db[("PostgreSQL<br/>harnesses · runs · transcripts<br/>approvals · audit_events")]
    llm["Model provider<br/>Anthropic API"]
    mcp["MCP servers<br/>local processes over stdio"]

    user --> web
    auditor --> web
    builder --> web
    builder --> cli
    operator -. "agenty.yaml" .-> serve

    web -- "bearer token, JSON + SSE" --> api
    cli -- "bearer token, JSON + SSE" --> api
    api --> loop
    loop -- "Generate" --> llm
    loop -- "every tool call" --> gw
    gw --> pol
    gw -- "only path to tools" --> mcp
    api --> db
    gw -. "audit records" .-> db
```

### Backend packages

The packages form layers, and the lint configuration enforces them: `depguard` lets only `internal/policy` import OPA, only `internal/mcptool` import the MCP SDK, only `internal/store` touch PostgreSQL and only `internal/server` use gin; `forbidigo` lets only the tool gateway call a tool, start a tool server or list its tools. A boundary that a linter checks cannot erode through one convenient shortcut.

```mermaid
flowchart TB
    cmd["cmd/agenty<br/>process entry point"] --> cli
    cli["internal/cli<br/>serve · apply · run · audit"] --> config & server & harness
    cli --> auditlog & store

    server["internal/server<br/>HTTP API · job queue · approvals · SSE hubs"]
    server --> api["internal/api<br/>JSON types from the spec"]
    server --> store["internal/store<br/>sqlc on pgx · goose migrations"]
    server --> agent
    server --> mcptool
    server --> config
    server -- "builds the run's model" --> model

    agent["internal/agent<br/>the agent loop, suspend and resume"] --> model
    agent --> toolgateway
    agent --> policy
    agent --> harness["internal/harness<br/>the builder's YAML"]

    model["internal/model<br/>provider-neutral messages"] --> anthropic["internal/model/anthropic<br/>official SDK adapter"]

    toolgateway["internal/toolgateway<br/>grant · limit · policy · record · execute · redact"] --> secret
    mcptool["internal/mcptool<br/>MCP servers as ToolServers"] -. "implements ToolServer" .-> toolgateway
    policy["internal/policy<br/>embedded OPA, layered"] -. "implements Policy" .-> toolgateway
    store --> auditlog["internal/auditlog<br/>hash chain, export, verify"]
    config["internal/config<br/>agenty.yaml, env secrets"] --> secret["internal/secret<br/>exact-value redaction"]

    classDef core fill:#fde68a,stroke:#b45309,color:#1c1917
    class toolgateway,policy,secret core
```

The highlighted packages are where the trust model lives; they carry the most thorough tests and `task coverage` requires them to be fully covered. The tool gateway knows nothing of MCP, OPA or PostgreSQL: it declares the interfaces it needs (`Tool`, `ToolServer`, `Policy`, `Audit`), and every implementation sits behind the same checks.

### A tool call through the gateway

Every call the model asks for takes the same fixed path, cheapest check first. Anything short of a clear allow is a denial, and nothing runs until its decision is recorded. A call that needs approval does not block the run: the run suspends, holds no worker, and survives a restart. When a person answers, a worker resumes the run on a new gateway, whose call counts are rebuilt from the audit log, and the call is decided again under the central policy in force *then*, because an approval answers a person's question; it does not override policy.

```mermaid
sequenceDiagram
    autonumber
    participant M as Model
    participant A as Agent loop
    participant G as Tool Gateway
    participant P as Policy (OPA)
    participant L as Audit log
    participant T as MCP server

    A->>M: Generate(instructions, history, granted tools)
    M-->>A: reply with tool call
    A->>G: Call(tool, args)
    Note over G: closed? granted? under the call limit?<br/>otherwise deny before policy runs
    G->>P: evaluate central layer, then harness layer
    P-->>G: allow · require_approval · deny (strictest wins)
    G->>L: record decision
    alt deny
        G-->>A: ErrDenied, sent to the model as an error result
    else require_approval
        G-->>A: *Suspended: run → waiting, worker freed
        Note over A,G: person answers (or approvals.timeout rejects)<br/>run → queued → a worker resumes it
        A->>G: Resume(call, answer)
        G->>P: decide again with current policy
        G->>L: record answer
        G->>T: execute
    else allow
        G->>T: execute
    end
    T-->>G: result
    Note over G: redact every configured secret
    G->>L: record result
    G-->>A: result, back to the model
```

### A run's life

The `runs` table is the job queue; there is no queue library. Workers, `runs.workers` goroutines inside `agenty serve`, claim the oldest queued run with `FOR NO KEY UPDATE SKIP LOCKED`, so they never wait for each other or take the same run. A run is never retried, as a tool may not be idempotent; a run that ends badly is continued by a follow-up instead, which tells the model how it ended.

```mermaid
stateDiagram-v2
    [*] --> queued: POST runs / follow-up<br/>run.started recorded
    queued --> running: a worker claims it
    running --> waiting: a call needs approval<br/>request stored, worker freed
    waiting --> queued: owner answers, or the request expires
    running --> succeeded: the model answers
    running --> failed: error · step limit · server restart
    queued --> cancelled: owner cancels
    waiting --> cancelled: owner cancels<br/>request withdrawn
    running --> cancelled: owner cancels
    succeeded --> [*]
    failed --> [*]
    cancelled --> [*]
```

Queued and waiting runs outlive a restart; a running one does not, and is failed by the next server's start. A follow-up starts a new run of the harness version the conversation started with, so a conversation is a chain of runs, each of which stays its own unit of audit, limits and approvals. A conversation's MCP servers stay running between its runs, kept in a pool keyed by conversation and never shared with another one.

### Inside `agenty serve`

```mermaid
flowchart LR
    subgraph http["Request path (gin)"]
        direction TB
        mw["Recovery → logRequests → localOnly<br/>→ cors → authenticate"] --> member["member<br/>non-members get 404"]
        member --> h["handlers<br/>ownRun finds only the user's runs"]
    end

    h -- "enqueue" --> q[("runs: queued")]
    h -- "answer approval" --> q
    q -- "wake channel" --> w["workers × runs.workers<br/>claim · prepare · execute"]
    w -- "agent.Continue / Resume" --> ag["Agent + Gateway"]
    ag -- "suspend" --> wt[("runs: waiting<br/>approvals: pending")]
    exp["expiry goroutine<br/>one timer, next due request"] -- "reject, requeue" --> q
    wt -.-> exp

    ag -- "audit · approval · finished" --> hub["hub per unfinished run<br/>append-only event list"]
    hub -- "SSE from event 0" --> sse["GET …/runs/{id}/events"]
    w -- "lease / keep / close" --> pool["MCP pool<br/>(conversation, server)"]
```

Each unfinished run has a hub, an append-only list of its events, so a client that subscribes late still sees everything from the start; a finished run's stream is replayed from the database. The run's audit sink stores each record before it publishes it, so a client never sees an event the database does not hold.

### Data model

There are no tables for users, workspaces or conversations: users and workspaces come from the operator config, read at each start, and a conversation is the chain of runs that follow one another, named by its first run's ID.

```mermaid
erDiagram
    harness_versions ||--o{ runs : "ran"
    runs |o--o| runs : "follows (unique: never branches)"
    runs ||--o{ run_messages : "transcript"
    runs ||--o{ approvals : "requests"
    runs |o--o{ audit_events : "recorded"

    harness_versions {
        bigint id PK
        text workspace
        text name
        int version "immutable; unique per workspace and name"
        jsonb definition
    }
    runs {
        text id PK
        bigint harness_version_id FK
        text follows FK "nullable, unique"
        text conversation_id "first run's id"
        text started_by
        text status "queued running waiting succeeded failed cancelled"
        text prompt_digest
        text history_digest
    }
    run_messages {
        text run_id PK
        int position PK
        text role
        json provider_data "the model's own form, never shown"
        bool altered "redaction changed it"
    }
    approvals {
        text id PK
        text run_id FK
        text call_id
        json args
        text status "pending approved rejected withdrawn"
        timestamptz expires_at
    }
    audit_events {
        bigint id PK "gapless"
        text actor
        text action
        text workspace
        jsonb details "canonical"
        bytea prev_hash
        bytea hash "SHA-256 over prev_hash and the event"
    }
```

The audit log is append-only and tamper-evident. Database triggers refuse every `UPDATE`, `DELETE` and `TRUNCATE` on `audit_events`. Appends take an advisory lock, so ids run from 1 without gaps, and each event's hash covers the one before it. A change is written in the same transaction as the events that record it. `agenty audit verify` checks an export offline and prints the anchor, the last event's id and hash, to keep outside Agenty.

### Web frontend

```mermaid
flowchart TB
    html["index.html<br/>theme.js sets data-theme before first paint"] --> main["main.tsx<br/>Theme + Session on localStorage"]
    main --> app["App.tsx<br/>QueryClient · typed client · router"]

    app --> router["TanStack Router"]
    router --> signin["/sign-in<br/>checks a pasted token with GET /v1/me"]
    router --> authed["_authed layout<br/>redirects without a valid token · Shell + AppSidebar"]
    authed --> chat["/ and /c/{conversation}<br/>New chat · Chat"]
    authed --> manage["/w/{ws}/…<br/>overview · harnesses · audit"]
    authed --> audit["/audit/…<br/>auditors only"]
    authed --> activity["/activity"]

    chat & manage & audit & activity --> queries["api/queries.ts<br/>cache keys + fetchers"]
    queries --> client["api/client.ts<br/>openapi-fetch typed by schema.ts<br/>token only in Authorization"]
    chat --> events["api/run-events.ts<br/>fetch + eventsource-parser<br/>(EventSource cannot send a token)"]
    events -- "invalidate transcript, approvals, chats" --> queries
    client & events --> server[["agenty API"]]
```

Route loaders and components read the same query options, so they share one cache entry. A Content-Security-Policy, written into the built `index.html`, lets the page run only its own scripts and talk only to the API. Model and tool output is always rendered as text, never as HTML: a prompt injection can shape that output, and the browser holds a token worth stealing.

### Where each guarantee lives

| Trust-model guarantee ([IDEA.md](docs/IDEA.md#trust-model-the-parts-that-matter-from-day-one)) | Enforced in |
|---|---|
| 1. The model is not trusted | the gateway's deterministic checks; nothing depends on detecting prompt injection |
| 2. Every side effect goes through the gateway | `internal/toolgateway`, with `forbidigo` and `depguard` rules in `.golangci.yml` |
| 3. Default deny | `toolgateway`: an ungranted tool is never listed, and denied before policy runs |
| 4. Strictest wins | `internal/policy`: central and harness layers evaluated apart; only `deny` and `require_approval` rules |
| 5. Credentials never reach the model | `internal/secret`, built once by `internal/config` from every `{env: …}` value; applied by the gateway, the server and the log handler |
| 6. Every tool call is recorded | `toolgateway` records before it executes; `internal/store` appends to `audit_events` |
| 7. Runs are private | `internal/server`: `ownRun` and the conversation queries find only the user's own |
| 8. Only auditors read the audit log | `internal/server/audit.go`; auditors see tool calls and decisions, never inputs or replies |
| 9. Append-only and tamper-evident | triggers in the migration; hash chain in `internal/auditlog` and `internal/store/audit.go` |

Each guarantee has a named invariant test (`TestInvariant_…`) that fails if it is broken.

## Repository layout

| Path | What it holds |
|---|---|
| `cmd/agenty` | the binary's entry point |
| `internal/` | the backend packages above; each package comment explains its role in detail |
| `schema/openapi.yaml` | the API spec, from which the Go server types and the frontend client types are generated |
| `web/` | the web frontend, its own deployable |
| `examples/notes` | an operator config, central policy and harness to try it with |
| `docs/IDEA.md` | the idea, trust model, technology choices and stage decisions |
