# Agenty — System Architecture

> **Status**: Draft for review
> **Inputs**: [VISION.md](../../../VISION.md), [CAPABILITIES.md](../../../CAPABILITIES.md)
> **Scope**: System-wide architecture for v1: components, boundaries, key technology choices, and cross-cutting concerns. Delivery slices, epics, and the roadmap are planned separately.

---

## 1. Context and Constraints

| Constraint | Source | Consequence |
|---|---|---|
| Built by one maintainer with AI assistance | Decision | Minimize languages, components, and operational surface. Tests are the primary correctness guarantee. |
| Runs on any container host, including a single VM in production | Decision | No hard dependency on Kubernetes. Sandboxing must work on plain Docker/Podman hosts. |
| Mid-size organizations: up to a few thousand users, hundreds of harnesses, tens of concurrent runs per node | Decision | Single-node installs are realistic; scale out by adding nodes. |
| Languages: Go and TypeScript; no Python in platform code | Decision | Python exists only inside sandboxes (user skill scripts and tools). |
| Required dependencies open source, free to self-host, no paid tier needed, licenses permit hosting | CAPABILITIES §10 | Every dependency is checked against this rule when added. |
| Operable by a regular enterprise IT team | VISION principle 6 | PostgreSQL is the only required stateful service. |
| Harness definition independent of any agent framework | VISION principle 5 | Agenty owns its agent loop and definition schema. |
| Deterministic enforcement outside the model | VISION trust model | All side effects pass through a single Tool Gateway. |

## 2. Key Decisions

| Area | Decision |
|---|---|
| Overall shape | Go modular monolith (`agenty`) plus a separate sandbox runner (`agenty-sandbox`), each in its own Go module. |
| Frontend | React + Vite SPA, built to static assets; embeddable in the server binary or hosted on any static host. |
| State | PostgreSQL with pgvector; blob storage on a local volume or S3-compatible store. |
| Durable execution | Own run state machine on PostgreSQL; no workflow engine or job-queue dependency. |
| Notifications | Postgres `LISTEN/NOTIFY` as a wake-up hint behind an interface; database rows are the source of truth; polling fallback. |
| Agent loop | Own thin loop on official provider SDKs; no agent framework. |
| Policy | OPA embedded as a Go library; three restrict-composable layers. |
| Sandbox | gVisor (`runsc`) on Docker/Podman; one-shot and session modes; egress proxy with per-sandbox allowlists. |
| Connectors | MCP servers outside this repository; remote (streamable HTTP) and local (stdio, in sandbox sessions). |
| Retrieval | Live search for Confluence and SharePoint with delegated credentials; pgvector index for uploads and S3. |
| Internal RPC | Connect RPC between server and sandbox runner, mTLS. |
| Public API | REST/JSON, OpenAPI 3.1 as source of truth, generated Go server and TS client. |
| Logging | `log/slog` API with `charmbracelet/log` as handler. |
| Testing | Unit, module, integration, E2E, UI; coverage and mutation gates; all runnable locally via Task. |

## 3. Repository Layout

```
agenty/
├── go.work          # Go workspace across the Go modules
├── api/             # Contracts: OpenAPI (public API), sandbox protocol (Connect/protobuf),
│                    # generated Go and TypeScript clients
├── schemas/         # JSON Schema for the declarative harness definition (YAML)
├── server/          # Go module → binary `agenty` (roles: api, worker, scheduler)
├── sandbox/         # Go module → binary `agenty-sandbox`
├── web/             # React + Vite SPA → static assets
├── deploy/          # Dockerfiles, Docker Compose; Helm later
├── docs/
└── Taskfile.yml     # All build and test entry points
```

**Boundaries**
- `server` and `sandbox` are separate Go modules and share only the generated contracts in `api/`. Neither can import the other's internals.
- Connectors (MCP servers) are not part of this repository.
- The web app is a pure API client: it uses only the public API, so everything the UI does is also available to automations.

**Isolation**: every component builds, runs, and is tested on its own; Docker Compose runs them together for integration and end-to-end tests.

## 4. Runtime Components

```
             ┌──────────── agenty (one binary; roles can run separately) ───────────┐
 Browser ──► │ api        REST API, auth, entry points, approvals, catalog, admin;  │
 Automations │            optionally serves the web UI                              │
             │ worker     executes runs (agent loop)                                │──► Model providers
             │ scheduler  fires schedules; single leader via Postgres advisory lock │──► Remote MCP servers
             └───────┬─────────────────────────────┬────────────────────────────────┘──► Knowledge sources
                     │                             │ Connect RPC + mTLS              ──► OTel collector (optional)
              PostgreSQL + pgvector          agenty-sandbox ── one-shot: skill scripts, custom tools,
              Blob storage (volume or S3)                      document parsing
                                                           ── session: local MCP servers
```

**Roles**: a single process can run all roles (single-VM installs) or each role can be scaled independently. Roles share the database and coordinate only through it.

**Server packages** (each with a narrow interface, testable in isolation):

| Package | Responsibility |
|---|---|
| `identity` | Principals, sessions, tokens, OIDC/SAML, roles, harness audiences |
| `workspace` | Workspaces, memberships, service accounts |
| `catalog` | Published building blocks, scopes, reviews |
| `harness` | Definitions, immutable versions, lifecycle states, YAML import/export |
| `runs` | Run state machine, step log, claiming, leases, scheduling, recovery |
| `loop` | Agent loop: context building, model calls, built-in tools, output validation |
| `toolgateway` | The single path for every side effect (see §6) |
| `policy` | Embedded OPA, bundle loading, decision evaluation |
| `credentials` | Credential broker: storage, encryption, refresh, injection |
| `approvals` | Approval requests, routing, decisions |
| `mcp` | MCP client for remote servers and sandbox-hosted local servers |
| `sandboxclient` | Client for the sandbox protocol |
| `knowledge` | Live search adapters, indexing, hybrid retrieval, citations |
| `models` | `Model` interface and provider implementations |
| `audit` | Audit events, hash chain, crypto-shredding keys, retention |
| `metering` | Token usage, cost, budgets |
| `notify` | Wake-up notifications (`Notifier` interface) |
| `httpapi` | Public API handlers generated from OpenAPI |

## 5. Runs and Durable Execution

### 5.1 Run state machine

```
queued → running ⇄ waiting_approval
            ⇅
      waiting_input              (chat: waiting for the next user message)
            ↓
   completed | failed | cancelled | needs_attention
```

- A run records the harness version it started with; it finishes on that version. New runs (including schedules and API callers) use the harness's active version.
- `failed` carries a reason (e.g., `budget_exceeded`, `output_invalid`, `policy_denied_final`, `step_limit`).
- `needs_attention` is entered when a non-idempotent tool call's outcome is unknown after a crash (§5.4). A person resumes or cancels the run.

### 5.2 Step log

Each run has an append-only step log: `model_request`, `model_response`, `tool_call_requested`, `policy_decision`, `approval_requested`, `approval_decided`, `tool_call_started`, `tool_call_finished`, `user_message`, `output_validated`. Every step is committed before the next one begins.

The step log is the single record of what happened. The trace view, audit events, usage data, and recovery are all derived from it.

### 5.3 Execution mechanics

All implemented in `runs` without a queue dependency:
- **Claiming**: workers claim runnable runs with `SELECT … FOR UPDATE SKIP LOCKED`.
- **Leases**: a claimed run holds a lease renewed by heartbeat. Expired leases make the run claimable again; the new worker resumes from the step log.
- **Retries**: transient failures (model provider errors, network) retry with exponential backoff and jitter, within per-run limits.
- **Pauses**: `waiting_approval` and `waiting_input` hold no worker. A decision or a user message commits the state change and marks the run runnable in the same transaction.
- **Scheduling**: the `scheduler` role holds a Postgres advisory lock (leader election), evaluates due schedules (cron expressions parsed by a small library), and creates runs transactionally. Schedule identity checks (owner still active, access to the harness, valid credentials) happen at fire time (§7.3, §7.4).
- **Creation**: the API creates the run record and marks it runnable in one transaction; a run cannot be lost between steps.

### 5.4 Crash semantics for tool calls

If a worker dies after `tool_call_started` and before `tool_call_finished`:
- **Idempotent tools** (declared in catalog metadata) are retried.
- **Non-idempotent tools** move the run to `needs_attention`, showing the uncertain call. Nothing is retried silently.

### 5.5 Notifications

- A `Notifier` interface (`Publish`, `Subscribe`) wakes waiters: synchronous API calls, SSE streams, and workers waiting for runnable work.
- Implementations: Postgres `LISTEN/NOTIFY` (sent only after commit) and in-process (single node, tests). NATS or similar can be added later without touching other packages.
- **Rows are the truth, notifications are hints.** Every waiter re-reads state when woken and also polls as a fallback (1–2 s). A lost notification costs latency, never correctness.

## 6. Agent Loop, Tool Gateway, Models, and Policy

### 6.1 Models

- `Model` interface: streaming generation, tool calling, usage reporting.
- Providers in v1: OpenAI and OpenAI-compatible endpoints (covering Azure OpenAI, AI gateways, and local runtimes), Anthropic, Google, AWS Bedrock — each via its official Go SDK.
- A **model configuration** (catalog item) holds provider, endpoint, model, parameters, and, for commercial models, the price per token.

### 6.2 Context and built-in tools

- **System prompt**: platform preamble, harness instructions, and a skill index (name and description per skill).
- **First message**: the entry point's prompt template rendered with parameters inside delimited data blocks (`<data source="parameter" trust="untrusted">…</data>`).
- **Built-in tools**:
  - `load_skill` — loads a skill's instructions on demand.
  - `run_skill_script` — runs a script from a loaded skill in a one-shot sandbox.
  - `search_knowledge` — retrieval (§10.2).
  - `final_answer` — its JSON Schema is the harness's output contract; the result is validated with a bounded number of corrective retries.
- **Limits** per run: maximum steps, maximum tokens, and budgets (§11.3).

### 6.3 Tool Gateway

Every side effect — remote MCP calls, local MCP calls, custom tools, skill scripts, built-in tools, sandbox session starts — passes through `toolgateway`. No other package may import the executors (enforced by architecture tests, §13).

For each call:
1. Resolve the tool, its catalog metadata, and the effective identity (agent grants ∩ caller).
2. Evaluate policy (§6.5) → `allow`, `deny`, or `require_approval`, with reasons.
3. On `require_approval`, persist the approval request and park the run in `waiting_approval`.
4. Obtain the credential from the broker for the effective principal and connection (§7.4).
5. Execute via the matching executor; record `tool_call_started` / `tool_call_finished`.
6. Label the result's trust level, update the run's taint state, write the audit event.

On `deny` or rejected approval, the model receives a tool error with the reason so it can adapt.

### 6.4 Untrusted content

- Tracking is **per run**: once untrusted content enters the context, the run is `tainted`, and the run records the list of taint sources (e.g., `param:description`, `servicenow.get_request`, `knowledge:confluence`).
- Catalog tool metadata declares: output trust (untrusted by default), **effect** (`read`, `write`, `external_communication`), and **idempotency**.
- **Reference policies** shipped with Agenty: writes in tainted runs require approval; `external_communication` in tainted runs is denied. Compliance can tune them.

### 6.5 Policy layers

| Layer | Author | Form | May produce |
|---|---|---|---|
| Central policies | Compliance | Rego, uploaded or loaded as OPA bundles (HTTP, OCI registry, mounted directory); signed bundles supported | `deny`, `require_approval`, trust overrides |
| Structured rules | Business builders | Data (`{tool, condition, action}`) evaluated by one fixed, tested Rego policy | `deny`, `require_approval` |
| Custom harness Rego | Technical builders | Rego modules in the harness definition | `deny`, `require_approval` |

- **Combination**: strictest wins (deny > require_approval > allow). Ungranted tools are denied by default. Harness layers cannot loosen anything; other outputs from harness Rego are ignored.
- **Decision input**: caller, agent, harness, tool and metadata, arguments, run state (tainted, sources, step count, cost).
- **Harness Rego guardrails**: restricted built-ins (no `http.send`, no network or runtime functions), validation on save (compile errors and disallowed rule names are rejected), optional `opa test` suites run on save, evaluation timeouts on every layer.
- **Versioning**: every loaded bundle version is recorded; each `policy_decision` step references the policy versions used.

### 6.6 Approvals

- **Routing**: in a personal context (session, personal API call, personal schedule) the run's user approves; in a workspace context (workspace schedule, service-account call) any workspace member approves.
- **Decisions in v1**: approve, or reject with a comment (returned to the model as a tool error).
- The approvals inbox is part of the web UI and the API.

## 7. Identity, Authentication, and Credentials

### 7.1 Principals and roles

- **Principals**: `user`, `service_account`, `agent`. Each harness has an agent principal with grants (connections and tools it may use).
- **Platform roles** (static, in Go): platform admin, compliance, FinOps, enablement, workspace admin, workspace member. OPA is reserved for runtime policy.
- **Workspace service accounts**: usable by every member of the workspace; every run records both the service account and the configuring person.

### 7.2 Harness audience

Each harness declares who may call it: workspace members only, specific IdP groups, or all authenticated users. Groups come from OIDC and SAML claims until SCIM is available.

### 7.3 Authentication

- **Humans**: OIDC (authorization code + PKCE) and SAML 2.0, handled by the server. Server-side sessions with an HttpOnly, Secure cookie; no tokens in browser storage.
- **Cross-origin UI hosting**: allowed origins are configured explicitly; CORS permits credentials only for them. Same-site deployments need no extra configuration.
- **API callers**: personal access tokens for people; Agenty-issued tokens or an OAuth client-credentials endpoint (short-lived tokens) for service accounts. Tokens are scoped, expiring, and stored as hashes.
- **Departure detection** (until SCIM): a person's failing login or credential refresh disables their personal schedules.

### 7.4 Credential broker

- A **connection** is a remote MCP server, a local MCP image, or an OpenAPI source, plus its auth method. A **credential** is secret material bound to a principal and a connection.
- **Credential types**:
  - User delegation via OAuth with PKCE or the MCP authorization flow ("Connect GitLab"), storing refresh tokens.
  - OAuth client credentials (service accounts only).
  - Static tokens or username/password (service accounts only; policy can disallow them).
- **Storage**: envelope encryption in Postgres; the master key comes from configuration (file or environment) in v1. KMS and Vault integrations come later.
- **Injection**: at call time only — HTTP headers for remote calls, environment variables for sandbox sessions. Never into model context; logs redact them.
- **Refresh failure**: the credential is marked invalid, its owner is notified, and dependent personal schedules are disabled.

## 8. Sandbox Runner

- **Isolation**: gVisor (`runsc`) as the production runtime on Docker or Podman; works on ordinary VMs without nested virtualization. Hardened `runc` (seccomp, no capabilities, read-only root filesystem, non-root user) only behind an explicit `insecure_dev_mode` flag.
- **Backends** behind an interface: Docker/Podman API in v1, Kubernetes later. The runner is the only component that talks to the container runtime.
- **One-shot mode**: create container, write request files (skill scripts, resources, documents to parse) into a tmpfs `/workspace`, execute with a time limit, collect stdout, stderr, exit code, and output files (size-limited), destroy.
- **Session mode**: start a local MCP server, relay its stdio over a bidirectional stream, destroy at run end or idle timeout. One instance per run and identity.
- **Images**: one maintained runtime image (Bash, Python, Node.js, curated common packages) for skill scripts and document parsing. Custom tools and local MCP servers bring their own images. Images must come from configured registries and are pinned by digest; nothing is installed at start time.
- **Egress**: sandboxes sit on an internal network with no default route. The only exit is an egress proxy inside the runner enforcing a per-sandbox hostname allowlist and logging connections. Skill scripts have an empty allowlist by default.
- **Limits**: CPU, memory, process count, tmpfs size, wall-clock time.
- **Protocol**: Connect RPC defined in `api/`, mTLS; the runner accepts calls only from the server.
- **Later**: warm pools, Kubernetes backend.

## 9. MCP Integration

- Agenty is an **MCP client**. Connector code lives outside this repository.
- **Remote servers** (streamable HTTP): servers supporting MCP authorization receive per-user tokens, enabling on-behalf-of calls. Servers with a single static credential can only be bound to service accounts.
- **Local servers** (stdio): run as sandbox sessions (§8). Because each instance serves one run and identity, environment-variable credentials can be the delegated credential of the acting user.
- **Tool discovery**: tools listed by an MCP server become catalog entries only after the enablement team (or a technical builder, in team scope) adds metadata: effect, trust, idempotency, and grants.
- **Starter kit**: curated, tested catalog entries and reference deployments for selected open-source MCP servers that meet the dependency rule.

## 10. Data and Knowledge

### 10.1 PostgreSQL

- Access via `pgx`; type-safe queries generated with `sqlc`; versioned SQL migrations with `goose`, run at startup under an advisory lock.
- **No triggers or stored procedures.** Workspace isolation is enforced in Go repositories (every workspace-scoped query requires a `workspace_id`) and covered by tests. Row-level security is an optional extra layer.
- **Table groups**: principals, sessions, tokens; workspaces, memberships, service accounts; catalog items, versions, reviews; harnesses, versions, lifecycle, audiences; schedules; runs, run steps; approvals; connections, credentials; policy bundles; knowledge sources, documents, chunks; audit events, subject keys; usage records, budgets.
- **Blob storage** (`Blob` interface): local volume or S3-compatible store for uploads, skill resources, and run artifacts.

### 10.2 Knowledge and retrieval

- **Confluence and SharePoint**: searched live through their own search APIs with the caller's delegated credential. Permissions are exact by construction; no document copies are kept.
- **Uploads and S3**: parsed, chunked, embedded (embedding model from a model configuration), and indexed in pgvector with hybrid search (vector similarity plus Postgres full-text, fused by reciprocal rank). Access follows the knowledge source's audience.
- **Parsing** of documents (PDF, DOCX, HTML) runs as one-shot sandbox jobs.
- **Every retrieved passage** keeps its source reference for citations and is marked untrusted (taints the run).
- **Later**: indexed connectors with ACL sync per source.

## 11. Audit, Observability, and Cost

### 11.1 Audit

- `audit_events` is append-only: the application's database role has `INSERT`-only rights on it. Each event contains the hash of the previous event (hash chain); the latest hash can be exported periodically to an external log.
- Audit events record who did what, when, under which policy versions, and reference the related run steps.
- **Crypto-shredding**: personal data in step payloads is encrypted with a key per person (the caller or approver it relates to). Erasure deletes the key; the event chain stays intact.
- **Retention**: personal data of third parties inside payloads (e.g., a customer named in a ticket) cannot be attributed reliably. A configurable payload retention period deletes payloads after N days, keeping metadata. Documentation states this limitation explicitly.

### 11.2 Observability

- **Logging**: code logs through `log/slog`; `charmbracelet/log` is the handler (colored text in development, JSON or logfmt in production). A wrapping handler redacts credentials.
- **Tracing**: OpenTelemetry spans per run, model call, tool call, policy decision, and sandbox execution, following the OTel generative-AI semantic conventions. Exported via OTLP only when an endpoint is configured.
- **Built-in trace view**: rendered from the step log; no external observability stack required.

### 11.3 Cost and budgets

- Each model step records input and output tokens and, for commercial models, the cost at the price valid at that time.
- Budgets per run, harness, and workspace are checked before every model call using running totals and periodic rollups. Exhausted budgets end the run as `failed` with reason `budget_exceeded`.
- Usage aggregates per run, step, harness, and workspace feed the FinOps views.

## 12. Public API, Definition Format, and Frontend

### 12.1 API

- REST/JSON under `/v1`. OpenAPI 3.1 in `api/` is the source of truth; Go server code via `oapi-codegen`, TypeScript client via `openapi-typescript` and `openapi-fetch`. Errors as RFC 9457 problem details.
- **Runs**: `POST /v1/harnesses/{id}/entrypoints/{name}/runs?wait=30s` returns 200 with the structured result if the run completes within the wait, else 202 with a run reference. `Idempotency-Key` header supported. `GET /v1/runs/{id}` for state, `GET /v1/runs/{id}/events` as server-sent events.
- **Chat**: a conversation is one run alternating between `running` and `waiting_input`; each user message resumes it.
- **Other resources**: workspaces, service accounts, harnesses (versions, lifecycle, import/export), catalog, schedules, approvals, connections and credentials (including connect flows), policies and bundles, knowledge sources, usage, audit (read-only).

### 12.2 Harness definition

- YAML with `apiVersion: agenty.dev/v1` and `kind: Harness`, validated against the JSON Schema in `schemas/`.
- Sections map to the harness concept in VISION §6: entry points (parameters, prompt templates), instructions, model configuration reference, tools and grants, skills, knowledge, policy (structured rules and optional Rego files), approvals, output contract, audience.
- Skills are `SKILL.md` directories, referenced from the catalog or included inline.
- The visual builder edits a form that maps one-to-one to the schema.

### 12.3 Frontend

- React, Vite, TypeScript; TanStack Router; TanStack Query with the generated client; shadcn/ui (Radix, Tailwind).
- The API base URL is loaded at runtime from `config.json`, so one build serves all hosting modes: embedded via `go:embed`, any static host (e.g., S3 + CloudFront, nginx), or the Vite dev server.
- Live run views and chat use server-sent events.

## 13. Testing Strategy

Most code will be AI-written; tests are the primary correctness guarantee. Development follows test-driven development.

### 13.1 Layers

| Layer | Scope | Tools |
|---|---|---|
| Unit | Functions and types | Go `testing`; Vitest |
| Module | One package or component in isolation with fakes at its boundaries; `server` against real Postgres with fake sandbox and fake model; `agenty-sandbox` through its protocol against real Docker with `runsc` | testcontainers-go |
| Integration | All components in Docker Compose: server, sandbox, Postgres, fake OpenAI-compatible model server, mock MCP servers (remote and local), test identity provider (Dex) | Go test harness |
| E2E | Both reference scenarios (VISION §9) end to end through the public API | Same stack, scripted model |
| UI | Components (Vitest + Testing Library); browser flows against the integration stack (build a harness, chat, approve, inspect a trace); accessibility checks | Playwright, axe-core |

### 13.2 Deterministic model behavior

A **scripted model** returns predefined responses (including tool calls) step by step. It exists as a Go `Model` implementation for module tests and as a fake OpenAI-compatible HTTP server for integration and E2E tests, exercising the real provider client code. An optional nightly run against a real model checks quality without gating.

### 13.3 Quality gates

- **Coverage**: ≥ 90 % line coverage overall; 100 % branch coverage for `toolgateway`, `policy`, `credentials`, `identity`, and `runs`.
- **Mutation testing** on the critical packages: Gremlins (Go), Stryker (TypeScript).
- **Architecture rules** (golangci-lint): only `toolgateway` imports executors; no cross-package internals; `web/` uses only the generated client.
- **Trust-model invariant suites**: one named suite per guarantee in VISION §10 (e.g., ungranted tools are denied; harness Rego cannot allow; credentials never reach model context or logs; delegation never widens permissions; service-account use records the configuring person).
- **Crash tests**: fault injection kills workers between steps and mid-tool-call; runs must resume or land in `needs_attention`.
- **Policy tests**: reference Rego policies ship with `opa test` suites.
- **Contract tests**: server against the OpenAPI spec; server and runner against the sandbox protocol.

### 13.4 Local execution

No CI for now; everything runs locally.
- **Task** (`Taskfile.yml`) defines `test:unit`, `test:module`, `test:integration`, `test:e2e`, `test:ui`, `test:mutation`, and `check` (everything plus coverage and mutation gates). `task check` must pass before merging to `main`.
- **Lefthook** git hooks: pre-commit runs lint, architecture rules, and unit tests; pre-push runs module tests.
- **macOS**: gVisor requires Linux. Sandbox tests using `runsc` run in a Linux VM (Colima or Lima). Without it, sandbox tests fall back to `insecure_dev_mode` and report that clearly.
- The same `task` commands can run in CI later (GitHub Actions standard runners are free for public repositories).

## 14. Technology Summary

All licenses must be verified against the dependency rule when each dependency is added.

| Purpose | Choice |
|---|---|
| Server, sandbox runner | Go |
| Frontend | TypeScript, React, Vite, TanStack Router/Query, shadcn/ui, Tailwind |
| Database | PostgreSQL + pgvector; `pgx`, `sqlc`, `goose` |
| Policy | Open Policy Agent (embedded Go library) |
| Sandbox isolation | gVisor (`runsc`) on Docker/Podman |
| Internal RPC | Connect RPC (`connect-go`) |
| Public API tooling | OpenAPI 3.1, `oapi-codegen`, `openapi-typescript`, `openapi-fetch` |
| MCP | Official MCP Go SDK |
| Model providers | Official OpenAI, Anthropic, Google, AWS SDKs for Go |
| Authentication | OIDC and SAML libraries for Go |
| Logging | `log/slog` + `charmbracelet/log` |
| Telemetry | OpenTelemetry Go SDK |
| Testing | Go `testing`, testcontainers-go, Vitest, Testing Library, Playwright, axe-core, Gremlins, Stryker, Dex (test IdP) |
| Tooling | Task, Lefthook, golangci-lint, Colima/Lima (macOS) |

## 15. Deliberately Deferred

- Kubernetes sandbox backend, Helm chart, high availability features beyond role separation.
- KMS and Vault integration for the master key; token exchange.
- Indexed connectors with ACL sync; long-term memory.
- Workflow canvas and a generic workflow engine (reassess when the canvas is designed).
- Sandbox warm pools; reuse of local MCP instances across runs.
- NATS or other external notification transport.
- CI pipeline.

## 16. Risks

| Risk | Mitigation |
|---|---|
| Scope of v1 for a single maintainer | Delivery slices and roadmap planned separately; components testable in isolation. |
| Own durable execution has subtle failure modes | Small, explicit state machine; crash-injection tests; 100 % branch coverage on `runs`. |
| Per-run tainting flags most runs, causing approval fatigue | Reference policies focus on writes and external communication; approval evidence (Later) supports deliberate relaxation. |
| gVisor setup burden for operators and macOS development | Documented host setup; Colima/Lima for local development; explicit `insecure_dev_mode`. |
| Third-party MCP server quality and licensing | Curated starter kit; policy enforcement before every call regardless of server behavior. |
| Live search quality for Confluence/SharePoint | Indexed connectors remain a Later option per source. |
| AI-written tests that pass without verifying behavior | Mutation testing, invariant suites, architecture rules, coverage gates. |
