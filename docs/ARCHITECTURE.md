# Agenty — Architecture

> **Status**: Approved (2026-10-04)
> **Companions**: [VISION.md](VISION.md) (why, principles, concepts, trust model) · [CAPABILITIES.md](CAPABILITIES.md) (what, phased v1 / Later / Explore)
> **Audience**: Maintainers and AI coding agents. This is the canonical reference for how Agenty is built and why. Read it before changing structure, adding dependencies, or touching security-relevant code.

---

## 0. How to Use This Document

- **VISION.md** defines the product: concepts (harness, entry point, skill, workspace, schedule, …), principles, and the trust model. This document assumes its vocabulary.
- **CAPABILITIES.md** says what ships in v1. If a capability is marked *Later* or *Explore*, do not build it as part of v1 work.
- **This document** says how the system is built. Section 2 records decisions with rationale and rejected alternatives — do not reopen them without a new decision recorded there. Section 3 lists invariants that every change must preserve.
- **Changing a decision**: update the relevant section and the decision record (§2) in the same change, with date and reason. Code that contradicts this document is a bug in one of the two; resolve it explicitly.

---

## 1. Context and Constraints

| Constraint | Origin | Consequence |
|---|---|---|
| Built by one maintainer, mostly AI-written code | Project reality | Few languages, few components, small operational surface. Tests are the primary correctness guarantee (§15). |
| Runs on any container host, including a single VM in production | Decision | No hard Kubernetes dependency. Sandboxing must work on plain Docker/Podman. |
| Mid-size organizations: up to a few thousand users, hundreds of harnesses, tens of concurrent runs per node | Decision | Single-node installs are normal; scale out by adding nodes and splitting roles. |
| Platform languages: Go and TypeScript only | Decision | **No Python in platform code.** Python exists only inside sandboxes (user skill scripts and tools). Rust is not used in v1. |
| Required dependencies must be open source, free to self-host, need no paid tier, and permit a hosted offering | CAPABILITIES §10 | Every new dependency is license-checked (§16). Commercial products only as optional integrations. |
| Operable by a regular enterprise IT team | VISION principle 6 | PostgreSQL is the only required stateful service. |
| Harness definition independent of any agent framework | VISION principle 5 | Agenty owns its agent loop and definition schema. |
| Deterministic enforcement outside the model | VISION trust model | Every side effect passes through one Tool Gateway (§7.3). |
| Public repository; CI on GitHub Actions (since 2026-10-05) | Decision | Every change lands through a pull request with required CI checks; all CI jobs run the same Task targets as local development (§15.4). |

---

## 2. Decision Record

Each decision lists what was chosen, why, and what was rejected. All decided 2026-10-04 unless noted.

| # | Decision | Rationale | Rejected alternatives |
|---|---|---|---|
| D1 | **Go modular monolith** (`agenty`) plus separate **sandbox runner** (`agenty-sandbox`), each its own Go module in one monorepo. | Single binary is easy to operate; OPA embeds natively in Go; full control over the tool-call path; one backend language for a solo maintainer. | *TypeScript throughout* (agent frameworks own the tool-call path, OPA not native, heavier Node operations). *Go control plane + TS runtime* (two backend languages and an internal contract for one maintainer). |
| D2 | **Own thin agent loop** on official provider SDKs; no agent framework. | Governance (policy, taint, approvals, audit) must sit inside the tool-call path; keeps the harness definition framework-free. Strands inspired the model-driven approach. | Strands, Mastra, Vercel AI SDK, LangGraph, Genkit, Eino. |
| D3 | **Own durable run state machine on PostgreSQL**, no queue or workflow dependency. | An agent run's state is already a log (transcript + pending action); explicit rows are transparent and debuggable; no replay-determinism rules; no young SDK risk. | *Temporal* (separate cluster, breaks minimal-components goal). *DBOS Transact Go* (young Go SDK, replay determinism, state hidden in its tables). *River* (maintainer preferred zero queue dependency). |
| D4 | **Postgres `LISTEN/NOTIFY` as wake-up hint only**, behind a `Notifier` interface, with polling fallback. | Low latency for chat and sync API calls without putting logic in the database; rows remain the truth so lost notifications only cost latency. | Polling only (acceptable, slower); external broker such as NATS (extra component). |
| D5 | **No logic in the database**: no triggers, no stored procedures. Workspace isolation enforced in Go. | Keeps behavior in testable Go code. | Row-level security as the primary isolation mechanism (allowed only as an optional extra layer). |
| D6 | **Connectors are MCP servers outside this repository.** Agenty is an MCP client for remote (streamable HTTP) and local (stdio) servers. | Keeps the codebase small; reuses the MCP ecosystem. The starter kit is curation (catalog entries, reference deployments), not code. | Connector binaries bundled in the monorepo. |
| D7 | **Local MCP servers run as sandbox sessions**, one instance per run and identity. | Same isolation as scripts; per-identity instances allow on-behalf-of credentials even for servers that only accept a static token. | Remote-only MCP in v1; shared long-lived local server processes. |
| D8 | **OPA as the single policy engine**, embedded. Three restrict-only layers (§7.5). | One engine, auditable; builders can tighten but never loosen central policy. | Pluggable engines (Cedar, CEL); generating Rego from the rule builder (rules are data evaluated by one fixed policy instead). |
| D9 | **Custom harness Rego allowed** for technical builders; **central policy bundles loadable from outside** (HTTP, OCI, directory; signed). | Requested power for technical users and compliance, made safe by restrict-only composition and restricted built-ins. | Rule builder only. |
| D10 | **Per-run taint tracking.** | Once content enters the model context it mixes; fine-grained flow tracking would be dishonest. Protection lives in policy on effects. | Per-message or per-value data-flow tracking; injection detection classifiers as a control. |
| D11 | **Sandbox: gVisor (`runsc`) on Docker/Podman**, egress via proxy with per-sandbox allowlist. | Strong isolation on ordinary VMs without nested virtualization. | Firecracker / Kata (need KVM). Plain `runc` (only as explicit `insecure_dev_mode`). |
| D12 | **Retrieval**: v1 indexes **uploads and S3** in pgvector. **Confluence and SharePoint** follow later (decided 2026-10-05) via **live search** with the caller's delegated credential. | Exact permissions by construction, no document copies, no ACL sync; deferring the live connectors reduces v1 scope. | Indexing all sources with ACL sync (more work, stale permissions, copies of enterprise data) — remains a Later option per source. |
| D13 | **Workspaces are shared team ownership**; they hold no credentials. **Service accounts belong to workspaces and are usable by every member**; workspace membership is a security boundary. | Business continuity without personal identities; simple mental model. | Workspace-level credentials; per-member service-account authorization. |
| D14 | **Schedules are separate from harnesses**: personal schedules run as their owner and are disabled when the owner leaves or credentials fail; workspace schedules run as a workspace service account. | Clean on-behalf-of semantics; continuity via workspace schedules. | Schedules owned by harnesses; schedule adoption/transfer (rejected as unnecessary). |
| D15 | **Approval routing**: personal context → the run's user; workspace context → any workspace member. | Simple v1; four-eyes and designated approvers come Later. | Named approvers, manager lookup in v1. |
| D16 | **Harness audience** (workspace / IdP groups / all authenticated). | Copilots used by people outside the owning workspace (e.g., HR copilot for everyone). | Callers limited to workspace members. |
| D17 | **Frontend: React + Vite SPA**, static assets, runtime `config.json`; embeddable in the server binary or hosted on any static host. | No SSR need for an internal app; keeps all hosting options open. | Next.js (adds a Node server). |
| D18 | **Public API: REST/JSON with OpenAPI 3.1 as source of truth**; **internal: Connect RPC** between server and sandbox. | Generated Go server and TS client; Connect supports streaming for sandbox sessions. | gRPC-only public API; GraphQL. |
| D19 | **Logging via `log/slog` API with `charmbracelet/log` as handler.** | Maintainer preference; slog keeps libraries consistent and the handler swappable. | Raw charm logger API throughout; zap/zerolog. |
| D20 | **Testing: five tiers, coverage and mutation gates, run through Task locally and in CI.** | AI-written code needs strong, honest tests; one set of commands for both environments. | Separate CI-only scripts. |
| D21 | **Cron parsing with `gronx`**. | Maintained, MIT. | `robfig/cron` (inactive since mid-2024). |
| D23 | **CI on GitHub Actions with a pull-request workflow; squash merge only** (decided 2026-10-05). | Repository is public, so standard runners are free; required checks keep `main` green; squash merge gives one conventional commit per PR. | Local-only gates; rebase or merge commits. |
| D24 | **Gin** as the HTTP framework for the public API (decided 2026-10-05). | Maintainer preference; mature and widely used; `oapi-codegen` generates Gin server code; supports streaming for server-sent events. | Standard library `net/http` routing; Echo; Chi. |
| D22 | **Biome** for TypeScript linting and formatting (decided 2026-10-05). | One fast tool for lint and format; configuration already in the repository. | ESLint + Prettier. |
| D25 | **pnpm, never npm,** as the package manager for `web/` and any Node tooling (decided 2026-10-05). | Maintainer decision; strict `node_modules` layout, content-addressable store, dependency build scripts blocked by default. | npm. |
| D26 | **testify** for Go test assertions: `assert`, `require`, `mock`, and `suite` (decided 2026-10-05). | Readable assertions and informative failure diffs for AI-written tests; `require` for preconditions where a test cannot continue, `assert` for independent checks. | Standard library `testing` only (hand-written `if … { t.Fatalf }` checks). |

---

## 3. Invariants

These must hold for every change. Each has a dedicated test suite (§15.3).

1. **Single side-effect path.** Every tool call, skill script, local MCP session start, and built-in tool with effects goes through `toolgateway`. No other package imports the executors.
2. **Default deny.** A tool not granted to the harness's agent principal is denied.
3. **Strictest wins.** Policy results combine as deny > require_approval > allow. Harness layers (structured rules, custom Rego) can only add `deny` or `require_approval`.
4. **Permissions never widen.** Effective permissions are agent grants ∩ caller permissions; delegation chains are recorded and cannot widen them.
5. **Every run has an authenticated caller** (person or service account); every service-account run records the configuring person.
6. **Credentials never reach model context, prompts, logs, traces, or skill scripts.** They are injected only at call time into HTTP headers or sandbox-session environments.
7. **Parameters and tool outputs are untrusted data.** Parameters are rendered in delimited data blocks; untrusted content taints the run.
8. **Step before action.** Each run step is committed before the next begins; `tool_call_started` is committed before execution.
9. **No silent retry of non-idempotent tools.** Unknown outcomes move the run to `needs_attention`.
10. **Runs are pinned** to the harness version they started with.
11. **Audit is append-only.** The application's database role cannot update or delete audit events; events form a hash chain.
12. **No phone-home.** Agenty makes no outbound calls beyond operator-configured endpoints.
13. **Workspace isolation.** Every workspace-scoped query is filtered by `workspace_id` in the repository layer.
14. **Sandboxed code has no network and no credentials unless explicitly granted.**

---

## 4. Repository Layout

```
agenty/
├── AGENTS.md        # Guidance for AI coding agents
├── .editorconfig, .golangci.yaml, biome.json   # Editor, Go lint, TS lint/format
├── .covergate.yaml  # Per-package coverage thresholds (§15.3)
├── docs/            # VISION.md, CAPABILITIES.md, ARCHITECTURE.md, ROADMAP.md, epics/, guides/, spikes/ (planned)
├── go.work          # Go workspace across the Go modules
├── api/             # Contracts: OpenAPI 3.1 (public API), protobuf/Connect (sandbox protocol),
│                    # generated Go and TypeScript code
├── schemas/         # JSON Schema for the harness definition (YAML)
├── server/          # Go module → binary `agenty` (roles: api, worker, scheduler)
├── sandbox/         # Go module → binary `agenty-sandbox`
├── web/             # React + Vite SPA → static assets
├── deploy/          # Dockerfiles, Docker Compose; Helm later
├── go-licenses-allowlist.txt   # Licenses allowed for Go dependencies (§16)
└── Taskfile.yml     # All build, test, and check entry points
```

- `server` and `sandbox` are separate Go modules; they share only generated contracts from `api/`.
- **Go package paths**: server packages live at `server/internal/<package>`, named as in §5.1 (others, such as `config`, only when needed). Tool executors (remote MCP, sandbox session, sandbox one-shot, built-in) live at `server/internal/executor/<kind>`. The architecture rules (§15.3) are written against these paths.
- **Generated code is never edited by hand**; change the contract and regenerate.
- The web app is a pure API client: only the public API, only through the generated client.
- Every component builds, runs, and is tested on its own; Docker Compose runs them together.

---

## 5. Runtime Components

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

- **Required infrastructure**: PostgreSQL (with pgvector), a volume or S3-compatible bucket, and the customer's identity provider. Production sandbox hosts need `runsc` installed.
- **Roles** coordinate only through the database. One process can run all roles.

### 5.1 Server packages

| Package | Responsibility |
|---|---|
| `identity` | Principals, sessions, tokens, OIDC/SAML, platform roles, harness audiences |
| `workspace` | Workspaces, memberships, service accounts |
| `catalog` | Published building blocks, visibility scopes, reviews, tool metadata |
| `harness` | Definitions, immutable versions, active version, lifecycle states, YAML import/export |
| `runs` | Run state machine, step log, claiming, leases, retries, scheduling, recovery |
| `loop` | Agent loop: context building, model calls, built-in tools, output validation |
| `toolgateway` | Single side-effect path (§7.3) |
| `policy` | Embedded OPA, bundle loading, decision evaluation |
| `credentials` | Credential broker: storage, envelope encryption, refresh, injection |
| `approvals` | Approval requests, routing, decisions |
| `mcp` | MCP client for remote servers and sandbox-hosted local servers |
| `sandboxclient` | Client for the sandbox protocol |
| `knowledge` | Live search adapters, indexing, hybrid retrieval, citations |
| `models` | `Model` interface and provider implementations |
| `audit` | Audit events, hash chain, per-person keys, retention |
| `metering` | Token usage, cost, budgets |
| `notify` | `Notifier` interface and implementations |
| `httpapi` | Public API handlers (generated interfaces) |

Packages expose narrow interfaces and are testable in isolation with fakes at their boundaries.

---

## 6. Runs and Durable Execution

### 6.1 Run states

```
queued → running ⇄ waiting_approval
            ⇅
      waiting_input              (chat: waiting for the next user message)
            ↓
   completed | failed | cancelled | needs_attention
```

| Transition | Trigger |
|---|---|
| `queued → running` | A worker claims the run |
| `running → waiting_approval` | Policy returns `require_approval` |
| `waiting_approval → running` | Approval decided (approve or reject) |
| `running → waiting_input` | Chat turn finished; waiting for the user |
| `waiting_input → running` | User sends a message |
| `running → completed` | Valid `final_answer` |
| `running → failed` | Reasons: `budget_exceeded`, `output_invalid`, `step_limit`, unrecoverable error |
| `* → cancelled` | Caller, owner, or admin cancels |
| `running → needs_attention` | Non-idempotent tool call with unknown outcome after a crash |
| `needs_attention → running / cancelled` | A person resumes or cancels |

- A run stores the harness version it started with and finishes on it. New runs (including schedules and API callers) use the active version.

### 6.2 Step log

Append-only per run: `model_request`, `model_response`, `tool_call_requested`, `policy_decision`, `approval_requested`, `approval_decided`, `tool_call_started`, `tool_call_finished`, `user_message`, `output_validated`. It is the single source for the trace view, audit references, usage data, and recovery.

### 6.3 Execution mechanics (all in `runs`)

- **Creation**: the API inserts the run and marks it runnable in one transaction.
- **Claiming**: `SELECT … FOR UPDATE SKIP LOCKED`.
- **Leases**: renewed by heartbeat; expired leases make the run claimable again; the new worker resumes from the step log.
- **Retries**: transient failures (provider errors, network) retry with exponential backoff and jitter within per-run limits.
- **Pauses**: `waiting_approval` and `waiting_input` hold no worker. The decision or message commits the state change and marks the run runnable in one transaction.
- **Scheduling**: the `scheduler` role holds a Postgres advisory lock, evaluates due schedules (`gronx`), checks schedule identity at fire time (owner active, access to the harness, valid credentials), and creates runs transactionally.

### 6.4 Notifications

`Notifier` (`Publish`, `Subscribe`) wakes waiters (sync API calls, SSE streams, idle workers). Implementations: Postgres `LISTEN/NOTIFY` (sent after commit) and in-process. Waiters always re-read state and poll as fallback every 1–2 s.

---

## 7. Agent Loop, Tool Gateway, Models, Policy

### 7.1 Models

- `Model` interface: streaming generation, tool calling, usage reporting.
- v1 providers via official Go SDKs: OpenAI and OpenAI-compatible endpoints (Azure OpenAI, AI gateways, local runtimes), Anthropic, Google, AWS Bedrock.
- A **model configuration** (catalog item): provider, endpoint, model, parameters, price per token (commercial models).

### 7.2 Context and built-in tools

- **System prompt**: platform preamble, harness instructions, skill index (name + description).
- **First message**: entry point template rendered with parameters in `<data source="parameter" trust="untrusted">…</data>` blocks.
- **Built-in tools**: `load_skill` (on-demand skill instructions), `run_skill_script` (one-shot sandbox), `search_knowledge` (§11.2), `final_answer` (its JSON Schema is the output contract; validated with bounded corrective retries).
- **Per-run limits**: steps, tokens, budgets.

### 7.3 Tool Gateway

For every call:
1. Resolve the tool, its catalog metadata, and the effective identity (agent grants ∩ caller).
2. Evaluate policy → `allow` / `deny` / `require_approval` with reasons; record `policy_decision`.
3. On `require_approval`: persist the request, park the run in `waiting_approval`.
4. Obtain the credential for the effective principal and connection from the broker.
5. Record `tool_call_started`, execute via the matching executor (remote MCP, sandbox session, sandbox one-shot, built-in), record `tool_call_finished`.
6. Label result trust, update run taint, write the audit event.

On deny or rejection, the model receives a tool error with the reason.

### 7.4 Untrusted content

- The run carries `tainted` and the list of taint sources (e.g., `param:description`, `servicenow.get_request`, `knowledge:confluence`).
- Catalog tool metadata: **output trust** (untrusted by default), **effect** (`read` / `write` / `external_communication`), **idempotent** (bool).
- **Reference policies**: writes in tainted runs require approval; `external_communication` in tainted runs is denied. Compliance can tune them.

### 7.5 Policy layers

| Layer | Author | Form | May produce |
|---|---|---|---|
| Central policies | Compliance | Rego; uploaded or loaded as OPA bundles (HTTP, OCI, directory); signed bundles supported | `deny`, `require_approval`, trust overrides |
| Structured rules | Business builders | Data `{tool, condition, action}` evaluated by one fixed, tested Rego policy | `deny`, `require_approval` |
| Custom harness Rego | Technical builders | Rego modules in the harness definition | `deny`, `require_approval` (anything else is ignored) |

- **Decision input**: caller, agent, harness, tool and metadata, arguments, run state (tainted, sources, steps, cost).
- **Harness Rego guardrails**: restricted built-ins (no `http.send`, no network or runtime functions); compiled and validated on save; optional `opa test` suites run on save; evaluation timeouts on all layers.
- **Versioning**: loaded bundle versions are recorded; each `policy_decision` references the versions used.

### 7.6 Approvals

- **Routing**: personal context (session, personal API call, personal schedule) → the run's user. Workspace context (workspace schedule, service-account call) → any workspace member.
- **v1 decisions**: approve; reject with comment (returned to the model as a tool error). Editing arguments, approval evidence, designated approvers, four-eyes, SLAs: Later.

---

## 8. Identity, Authentication, Credentials

### 8.1 Principals and authorization

- **Principals**: `user`, `service_account`, `agent` (one per harness, with grants on connections and tools).
- **Platform roles** (static, in Go): platform admin, compliance, FinOps, enablement, workspace admin, workspace member. OPA is used for runtime (tool) policy, not platform authorization.
- **Harness audience**: workspace members, specific IdP groups (from OIDC/SAML claims until SCIM), or all authenticated users.
- **Service accounts**: belong to a workspace, usable by every member; configured in Agenty.

### 8.2 Authentication

- **Humans**: OIDC (authorization code + PKCE) and SAML 2.0, handled by the server. Server-side sessions, HttpOnly Secure cookie; no tokens in browser storage.
- **Cross-origin UI**: explicitly configured allowed origins; CORS with credentials only for them.
- **API**: personal access tokens for people; Agenty-issued tokens or an OAuth client-credentials endpoint (short-lived tokens) for service accounts. Scoped, expiring, stored as hashes.
- **Departure detection** (until SCIM): failing login or credential refresh disables the person's personal schedules.

### 8.3 Credential broker

- **Connection**: remote MCP server, local MCP image, or OpenAPI source, plus auth method. **Credential**: secret bound to (principal, connection).
- **Types**: user delegation via OAuth + PKCE or MCP authorization ("Connect GitLab", refresh tokens stored); OAuth client credentials (service accounts only); static token or username/password (service accounts only; policy can disallow). Service-account downstream credentials must represent non-human accounts; the consenting person is recorded.
- **Storage**: envelope encryption in Postgres; master key from configuration (file or environment) in v1; KMS/Vault Later.
- **Injection**: only at call time (HTTP headers; sandbox-session environment). Logs redact.
- **Refresh failure**: credential invalid, owner notified, dependent personal schedules disabled.

---

## 9. Sandbox Runner (`agenty-sandbox`)

- **Isolation**: gVisor `runsc` on Docker/Podman in production. Hardened `runc` only with `insecure_dev_mode`.
- **Backends** behind an interface: Docker/Podman API (v1), Kubernetes (Later). The runner is the only component talking to the container runtime.
- **One-shot mode**: create container → write request files into tmpfs `/workspace` → execute with time limit → collect stdout, stderr, exit code, output files (size-limited) → destroy.
- **Session mode**: start a local MCP server → relay stdio over a bidirectional stream → destroy at run end or idle timeout. One instance per run and identity.
- **Images**: one maintained runtime image (Bash, Python, Node.js, curated packages) for skill scripts and document parsing; custom tools and local MCP servers bring their own images. Configured registries only; pinned by digest; nothing installed at start.
- **Egress**: internal network without default route; the runner's egress proxy enforces per-sandbox hostname allowlists and logs connections. Skill scripts: empty allowlist by default.
- **Limits**: CPU, memory, process count, tmpfs size, wall-clock time; read-only root filesystem.
- **Protocol**: Connect RPC (`api/`), mTLS, accepts only the server.
- **Later**: warm pools, Kubernetes backend.

---

## 10. MCP Integration

- **Remote servers** (streamable HTTP): MCP authorization yields per-user tokens → on-behalf-of. Static-credential servers bind to service accounts only.
- **Local servers** (stdio): sandbox sessions; per-identity instances can receive the acting user's delegated credential.
- **Tool onboarding**: tools listed by a server enter the catalog only with metadata (effect, trust, idempotency) added by the enablement team or a technical builder (team scope).
- **Starter kit**: curated, tested catalog entries and reference deployments for selected open-source MCP servers meeting the dependency rule.
- Policy is enforced before every call regardless of the server's own behavior.

---

## 11. Data and Knowledge

### 11.1 PostgreSQL

- `pgx` driver; `sqlc` for type-safe queries; `goose` migrations run at startup under an advisory lock.
- No triggers or stored procedures (D5). Optional row-level security only as an additional layer.
- **Entity groups**: principals, sessions, tokens · workspaces, memberships, service accounts · catalog items, versions, reviews · harnesses, versions, lifecycle, audiences · schedules · runs, run steps · approvals · connections, credentials · policy bundles · knowledge sources, documents, chunks · audit events, per-person keys · usage records, budgets.
- **Blob storage** (`Blob` interface in `server/internal/blob`): local volume or S3-compatible; uploads, skill resources, run artifacts. Keys are portable paths (ASCII letters, digits, `.`, `_`, `-` in `/`-separated segments, no segment starting with `.`), so no key can leave a local root. `Put` takes the declared size and replaces an object atomically; content of another size fails and leaves the previous object. The local volume stores each object under the SHA-256 of its key and writes it to a temporary file that is synced and renamed into place. One contract test suite runs against both implementations; S3 is tested against gofakes3, an in-process S3-compatible server (MinIO is AGPL-licensed and ruled out by §1).

### 11.2 Knowledge and retrieval

- **Confluence, SharePoint** (Later): live search through their search APIs with the caller's delegated credential; no copies.
- **Uploads, S3**: parsed (one-shot sandbox jobs), chunked, embedded (embedding model from a model configuration), indexed in pgvector; hybrid search (vector + Postgres full-text, reciprocal rank fusion). Access follows the knowledge source's audience.
- Retrieved passages carry source references for citations and taint the run.
- **Later**: indexed connectors with ACL sync.

---

## 12. Audit, Observability, Cost

### 12.1 Audit

- `audit_events` append-only (`INSERT`-only database role), hash-chained; latest hash optionally exported periodically.
- Events record who, what, when, under which policy versions, referencing run steps.
- **Crypto-shredding**: personal data in step payloads is encrypted per person (caller or approver); erasure deletes the key.
- **Retention**: third-party personal data in payloads cannot be attributed reliably; a configurable payload retention period deletes payloads after N days, keeping metadata. This limitation is documented for operators.

### 12.2 Observability

- **Logging**: `log/slog` API, `charmbracelet/log` handler (text in development, JSON or logfmt in production), redacting wrapper handler.
- **Tracing**: OpenTelemetry spans for runs, model calls, tool calls, policy decisions, sandbox executions (OTel GenAI semantic conventions); OTLP export only when configured.
- **Trace view** in the UI is rendered from the step log; no external stack required.

### 12.3 Cost and budgets

- Model steps record input/output tokens and, for commercial models, cost at the price valid at that time. Local models: tokens only.
- Budgets (run, harness, workspace) are checked before every model call; exhaustion fails the run with `budget_exceeded`.
- Aggregates per run, step, harness, workspace for FinOps views.

---

## 13. Public API, Definition Format, Frontend

### 13.1 API

- REST/JSON under `/v1`, served with Gin; OpenAPI 3.1 in `api/` is the source of truth; `oapi-codegen` (Go, Gin server interfaces), `openapi-typescript` + `openapi-fetch` (TS); errors as RFC 9457 problem details.
- **Start a run**: `POST /v1/harnesses/{id}/entrypoints/{name}/runs?wait=30s` → 200 with structured result if completed in time, else 202 with run reference. `Idempotency-Key` supported.
- **Observe**: `GET /v1/runs/{id}`; `GET /v1/runs/{id}/events` (server-sent events).
- **Chat**: one run per conversation alternating `running` / `waiting_input`.
- **Other resources**: workspaces, service accounts, harnesses (versions, lifecycle, import/export), catalog, schedules, approvals, connections and credentials (connect flows), policies and bundles, knowledge sources, usage, audit (read-only).

### 13.2 Harness definition

YAML, `apiVersion: agenty.dev/v1`, `kind: Harness`, validated against the JSON Schema in `schemas/` (authoritative once it exists). The visual builder edits a form that maps one-to-one to the schema. Skills are `SKILL.md` directories, referenced from the catalog or inline.

Illustrative only — field names will be fixed by the schema:

```yaml
apiVersion: agenty.dev/v1
kind: Harness
metadata:
  name: service-request-triage
  workspace: workplace-it
  owner: jane.doe@example.com
spec:
  audience: { type: workspace }
  model: { ref: catalog/models/default-chat }
  instructions: |
    You triage incoming service requests for Workplace IT …
  entryPoints:
    - name: triage
      type: api
      parameters:
        requestId: { type: string, required: true }
      prompt: |
        Triage service request {{ .requestId }}.
  tools:
    - ref: catalog/tools/servicenow.get_request
    - ref: catalog/tools/servicenow.close_request
    - ref: catalog/tools/directory.lookup_user
  skills:
    - ref: catalog/skills/workplace-taxonomy
    - ref: catalog/skills/diagnose-vpn
  policy:
    rules:
      - tool: servicenow.close_request
        when: { arg: priority, in: [1, 2] }
        action: deny
      - tool: servicenow.close_request
        action: require_approval
    rego: []            # optional custom Rego files (restrict-only)
  output:
    schema:
      type: object
      required: [category, action]
      properties:
        category: { type: string }
        action: { enum: [closed, assigned, escalated] }
```

### 13.3 Frontend

React, Vite, TypeScript (linted and formatted with Biome); TanStack Router; TanStack Query with the generated client; shadcn/ui (Radix, Tailwind). API base URL from runtime `config.json`; one build for all hosting modes (embedded via `go:embed`, static host such as S3 + CloudFront, Vite dev server). Live views via server-sent events.

---

## 14. Deployment

- **Images**: `agenty` (server, UI embedded and optionally served), `agenty-sandbox`, the sandbox runtime image; published to GitHub Container Registry by the release workflow.
- **Image build**: `deploy/agenty.Dockerfile` and `deploy/agenty-sandbox.Dockerfile`, built from the repository root (`.dockerignore` sends only `server/` and `sandbox/`). Multi-stage: each module is compiled on its own (`GOWORK=off`, `CGO_ENABLED=0`) in `golang` at the `go.work` Go version, with dependencies downloaded in their own cached layer from `go.mod` and `go.sum` (when present) and the version injected via `-ldflags` into `buildinfo.Version`; the runtime stage is `gcr.io/distroless/static-debian13:nonroot` (no shell or package manager; CA certificates, tzdata; user 65532). Base images are pinned by digest, and the images carry OCI labels including `org.opencontainers.image.version`. A unit test per module keeps the Dockerfile in line with `go.work` and these rules. The Dockerfiles work with the classic builder and BuildKit. `task build:images` builds `agenty:<version>` and `agenty-sandbox:<version>` locally; `task test:images` builds them and checks `--version`, the version label, and the non-root user. The sandbox runner needs access to the container runtime socket, granted at deployment (e.g., a supplementary group), not by running as root.
- **Single VM**: Docker Compose with `agenty` (all roles), `agenty-sandbox` (with `runsc` on the host), PostgreSQL with pgvector, a volume for blobs.
- **Scaled**: separate `api`, `worker`, `scheduler` processes against the same database; sandbox runners per host.
- **Configuration**: a YAML file (`agenty --config <file>`) with `AGENTY_*` environment variables overriding its values; invalid configuration aborts startup naming the field. `--roles` selects any combination of `api`, `worker`, `scheduler` (default: all). Only the `api` role listens for HTTP and serves `/healthz` (reporting the active roles) and `/readyz`. Master key for envelope encryption from file or environment in v1.
- **Later**: Helm chart, Kubernetes sandbox backend.

---

## 15. Testing Strategy

Most code is AI-written; tests are the primary correctness guarantee. Development is test-driven: write the failing test first.

### 15.1 Tiers

| Tier | Scope | Tools |
|---|---|---|
| Unit | Functions and types | Go `testing` with testify; Vitest |
| Module | One package/component in isolation with hand-written fakes or `testify/mock` at boundaries; `server` against real Postgres with fake sandbox and fake model; `agenty-sandbox` via its protocol against real Docker + `runsc` | testcontainers-go, testify |
| Integration | All components in Docker Compose: server, sandbox, Postgres, fake OpenAI-compatible model server, mock MCP servers (remote and local), Dex as test IdP | Go test harness |
| E2E | Both reference scenarios (VISION §9) end to end via the public API | Same stack, scripted model |
| UI | Components (Vitest + Testing Library); browser flows against the integration stack (build harness, chat, approve, inspect trace); accessibility | Playwright, axe-core |

### 15.2 Deterministic models

A **scripted model** returns predefined responses (including tool calls) step by step: as a Go `Model` implementation (module tests) and as a fake OpenAI-compatible HTTP server (integration, E2E). An optional nightly run against a real model checks quality without gating.

### 15.3 Quality gates

- **Coverage**: ≥ 90 % statement coverage per package; 100 % statement coverage for `toolgateway`, `policy`, `credentials`, `identity`, `runs` (including their subpackages). (Go tooling measures statements, not branches; mutation testing covers branch logic.) `task test:unit` and `task test:module` write coverage profiles for all Go modules to `coverage/`; `task check:coverage` (part of `task check`) merges them (a statement counts as covered if either tier covers it) and fails naming each package and its coverage below its threshold. The check is a small in-repository Go program, `server/internal/testinfra/covergate` (standard library plus the existing `goccy/go-yaml`; no new tool). Thresholds live in `.covergate.yaml`: a default plus import-path patterns (`/...` includes subpackages; the longest match wins; a nested pattern may only raise the threshold of the pattern containing it, so no override can lower a subpackage of a critical package), so the 100 % thresholds apply as soon as those packages exist. Packages with statements but no tests count as 0 %. There are no exclusions: `func main()` stays measured and command packages reach the threshold by testing `run()`. Never lower a threshold to make the gate pass.
- **Mutation testing** on those packages: Gremlins (Go), Stryker (TypeScript); initial efficacy threshold 80 %. `task test:mutation` runs Gremlins (pinned exactly by `task setup`) on each package in `MUTATION_PACKAGES` (Taskfile.yml; empty until the critical packages exist, which the task reports and succeeds) with the settings in `.gremlins.yaml`, and fails naming each package below the threshold. The threshold lives in `.gremlins.yaml` because Gremlins 0.6.0 ignores its `--threshold-*` flags; each package runs from its own module with `GOWORK=off`.
- **Gate self-tests**: `task test:gates` proves every gate fails when its rule is violated.
- **Architecture rules** (golangci-lint `depguard`, in `.golangci.yaml`): only `toolgateway` imports `server/internal/executor/...` (executor packages may import each other); `server` and `sandbox` do not import each other. Access to another package's `internal/` is already rejected by the Go compiler. That `web/` only uses the generated client is enforced separately for the web app (E03).
- **Invariant suites**: one named suite per invariant in §3.
- **Crash tests**: fault injection between steps and mid-tool-call; runs resume or land in `needs_attention`.
- **Policy tests**: reference Rego ships with `opa test` suites.
- **Contract tests**: server vs. OpenAPI; server and runner vs. sandbox protocol.
- **License check**: `task check:licenses` (§16).

### 15.4 Local and CI execution

**Local**

- **Task** targets: `test:unit`, `test:module`, `test:integration`, `test:e2e`, `test:ui`, `test:mutation`, `test:gates`, `test:milestone:Mx` (automated milestone demos), `test:nightly` (real model, not gating), `check:licenses`, `check:coverage`, and `check` (all gating tiers plus gates). `task check` must pass before merging to `main`. Tiers without tests report "no tests yet" and succeed. `test:images` (container image smoke test, §14) is not part of `check`, because an image rebuild takes about a minute; a unit test checks the Dockerfiles statically instead. `test:images` is run manually today and will run in the release workflow (E21).
- **Go test tiers** are selected by build tag: untagged tests are unit tests; `module`, `integration`, `e2e`, and `milestone` tag the other tiers. A milestone demo is the test `TestMilestone<Mx>`.
- **PostgreSQL in tests**: `server/internal/testinfra/pgtest` starts one PostgreSQL-with-pgvector container per test package (testcontainers-go, from `TestMain`) and gives each test its own database. `deploy/compose.yaml` runs the same pinned image for local development; a unit test keeps the two in sync. On macOS, the Task test targets point testcontainers-go at the active Docker context.
- **Tools** (Go, Task, Lefthook, golangci-lint, go-licenses, Gremlins, Docker) are installed by the developer on the `PATH`. `task setup` verifies their versions (golangci-lint, go-licenses, and Gremlins exactly, because lint results, detected licenses, and mutation results depend on them; the others as minimums) and installs the Lefthook hooks.
- **Web tools**: Node.js and pnpm are `PATH` tools verified by `task setup` (minimums, matching `engines` and `packageManager` in `web/package.json`). The repository uses pnpm, never npm. Biome (the single source of its version; `biome.json` references the same schema version), TypeScript, Vite, and Vitest are dev dependencies pinned by `web/pnpm-lock.yaml`; the Task targets install them with `pnpm install --frozen-lockfile` when the lockfile changes. pnpm blocks dependency build scripts by default and none is allowed: no installed package needs one (Vite uses prebuilt native binaries). `task lint` runs Biome and the type check (`tsc --noEmit`), `task build:web` builds `web/dist` with Vite (also part of `task check`), and `task test:unit` runs Vitest.
- **Lefthook**: pre-commit → lint, architecture rules, unit tests; pre-push → module tests.
- **macOS**: gVisor requires Linux; `runsc`-based sandbox tests run in a Colima or Lima VM, set up as described in the [macOS sandbox guide](guides/macos-sandbox.md). Without it, sandbox tests fall back to `insecure_dev_mode` and report that clearly.

**CI (GitHub Actions)**

- CI jobs call the same Task targets; there is no CI-only test logic.
- **On every pull request and push to `main`**: lint, architecture rules, gate self-tests, license check, unit, module (including sandbox tests with real `runsc` on Linux runners), integration, E2E, UI, and the milestone demos that exist. Mutation tests run for pull requests that touch the critical packages.
- **Nightly (scheduled)**: full mutation testing and `test:nightly` against a real model. Model credentials are repository secrets and are never exposed to pull-request workflows.
- **Security scanning**: CodeQL for Go and TypeScript; Dependabot for Go modules, npm, GitHub Actions, and Docker images.
- **Workflow hardening**: third-party actions pinned by commit SHA; least-privilege `permissions` per job; no `pull_request_target` for untrusted code.
- **Branch protection on `main`**: pull requests required; required checks must pass; squash merge only (linear history); no force pushes.
- **Release** (E21): a version tag builds and publishes images to GitHub Container Registry with a changelog.

---

## 16. Dependencies and Licenses

Verified 2026-10-04 against each project's repository. **Before adding any dependency, check it against the dependency rule (§1) and record it here.**

**Shipped (runtime)** — all linked and shipped code is permissive; the runtime base image additionally carries Debian OS data packages, some GPL-licensed, that are aggregated in the image, not linked:

| Project | License |
|---|---|
| PostgreSQL, pgvector | PostgreSQL License |
| pgx, sqlc, goose, gronx | MIT |
| Open Policy Agent, gVisor, connect-go, oapi-codegen | Apache-2.0 |
| MCP Go SDK | Apache-2.0 (new contributions) / MIT (not-yet-relicensed parts) |
| openai-go, go-genai, aws-sdk-go-v2 (incl. smithy-go; checked 2026-10-05), go-oidc, opentelemetry-go | Apache-2.0 |
| anthropic-sdk-go, charmbracelet/log, Gin, goccy/go-yaml (configuration file; already required by Gin) | MIT |
| crewjam/saml | BSD-2-Clause |
| distroless `static-debian13` (runtime base image; checked 2026-10-05) | Apache-2.0 (project); bundled Debian data packages (no executables) under their own licenses: ca-certificates MPL-2.0 (certificates) and GPL-2.0+ (packaging), tzdata public domain, media-types ad-hoc permissive, base-files GPL-2.0+, netbase GPL-2.0 (data and configuration files only; aggregated in the image, nothing linked into Agenty) |
| openapi-typescript, openapi-fetch, React, Vite, TanStack Router/Query, shadcn/ui, Radix, Tailwind | MIT |

**Development and test only** (not distributed):

| Project | License |
|---|---|
| testcontainers-go (incl. its `postgres` module), Vitest, Testing Library, Task, Lefthook, Colima, actionlint | MIT |
| gofakes3 (in-process S3-compatible test server; checked 2026-10-05) | MIT |
| ryszard/goskiplist (dependency of gofakes3) | Apache-2.0 |
| testify (`assert`, `require`, `mock`, `suite`; checked 2026-10-05), stretchr/objx (dependency of `testify/mock`) | MIT |
| go-spew, vendored in testify's `internal/` (testify v1.12 no longer depends on the module) | ISC |
| go-difflib, vendored in testify's `internal/` (likewise) | BSD-3-Clause |
| go.yaml.in/yaml/v3 (testify dependency; files ported from libyaml MIT, the rest Apache-2.0) | MIT and Apache-2.0 |
| Playwright, Gremlins, Stryker, Dex, Lima, go-licenses, Docker Engine and CLI | Apache-2.0 |
| `golang` Docker image (build stage only; its contents are not shipped; checked 2026-10-05) | BSD-3-Clause (Go); Debian packages under their own licenses |
| Biome (@biomejs/biome npm package) | MIT OR Apache-2.0 |
| axe-core | MPL-2.0 |
| golangci-lint | GPL-3.0 (standalone tool, not linked) |
| Node.js, pnpm, @vitejs/plugin-react, jsdom, @types/react, @types/react-dom | MIT |
| TypeScript | Apache-2.0 |

**Services used for development** (not dependencies of Agenty): GitHub Actions, CodeQL, Dependabot, GitHub Container Registry.

**Transitive dependencies**: `task check:licenses` runs `task check:licenses:go` and `task check:licenses:web`. `check:licenses:go` runs `go-licenses check` (pinned in `task setup`, because detected licenses depend on its version) in every Go module (`server`, `sandbox`) against the allowlist versioned in `go-licenses-allowlist.txt`, with the same two scopes and licenses as the web check: the packages linked into the binaries (`./cmd/...`) against `shipped`, and all packages including test-only imports (`./... --include_tests`, with the test-tier build tags `module`, `integration`, `e2e`, and `milestone` set through `GOFLAGS` so tagged test files count too) against `shipped` plus `dev`. go-licenses walks the full import graph, so transitive dependencies are covered; a dependency whose license is not allowed or cannot be detected fails the task, which names the library and license. go-licenses resolves build-constrained files for one platform, so each scope runs for `linux/amd64` and `linux/arm64` (the container images run on Linux; both architectures are checked because no image architecture is fixed yet) and, on another host, also for the host platform. Agenty's own modules are skipped with `--ignore github.com/jangraefen/agenty/server/` and `.../sandbox/` (go-licenses matches by string prefix, so the trailing slash keeps the ignore from also matching another module such as `github.com/jangraefen/agenty-sdk`) while they are unlicensed (VISION §13); their dependencies are still checked. A missing allowlist or one without `shipped` licenses fails the task with a message, because go-licenses would otherwise silently fall back to its default forbidden license types. For `web/`, `task check:licenses:web` runs `web/scripts/check-licenses.mjs` (plain Node, no dependency), which reads pnpm's built-in `pnpm licenses list --json` (every installed package in the lockfile, including transitive, private, and platform-specific optional ones; only the root package is not listed), evaluates SPDX expressions (`OR`: one side allowed; `AND`: both), and fails naming each package and license outside the allowlists defined in the script. It checks twice: production dependencies, which are shipped in the built assets, must be permissive (MIT, ISC, Apache-2.0, BSD-2-Clause, BSD-3-Clause, 0BSD); all dependencies, including build and test tools, may additionally be MIT-0, BlueOak-1.0.0, CC0-1.0, or MPL-2.0 (e.g., lightningcss inside Vite). A dedicated npm license checker (license-checker-rseidelsohn) was dropped on 2026-10-05 because it missed the optional platform binaries in pnpm's `node_modules` layout.

---

## 17. Working Conventions for Agents

- **Read first**: VISION.md (concepts, trust model), CAPABILITIES.md (scope), this document (structure, decisions, invariants).
- **Scope discipline**: build only v1 capabilities unless a task says otherwise.
- **Place code by responsibility** (§4, §5.1). Do not create new top-level directories or Go modules without recording a decision.
- **Contracts first**: API and protocol changes start in `api/` (OpenAPI, protobuf), then regenerate. Never edit generated code.
- **TDD**: failing test first; keep §15.3 gates green; `task check` before merging.
- **Security-relevant changes** (gateway, policy, credentials, identity, runs, sandbox) must extend the matching invariant suites.
- **Dependencies**: license check (§16) and record before use; prefer the standard library and existing dependencies.
- **Decisions**: if a task requires deviating from this document, update §2 and the affected sections in the same change.
- **Pull requests**: one story per pull request where practical, on a branch named `<type>/<issue>-<slug>` (e.g., `feat/42-entrypoint-api`). The PR title uses conventional-commit format, because it becomes the squash commit message. The description references the story (`Closes #42`) and the acceptance criteria it addresses. Merge only when all required checks pass.

---

## 18. Deliberately Deferred

Kubernetes sandbox backend and Helm chart · HA beyond role separation · KMS/Vault for the master key · token exchange · indexed connectors with ACL sync · long-term memory · workflow canvas and any generic workflow engine (reassess then) · sandbox warm pools and local MCP instance reuse · external notification transport (NATS).

## 19. Risks

| Risk | Mitigation |
|---|---|
| v1 scope for a single maintainer | Delivery slices and roadmap planned separately; components testable in isolation. |
| Own durable execution has subtle failure modes | Small explicit state machine; crash-injection tests; 100 % statement coverage and mutation testing on `runs`. |
| Per-run tainting causes approval fatigue | Reference policies target writes and external communication; approval evidence (Later). |
| gVisor setup burden (operators, macOS development) | Documented host setup; Colima/Lima; explicit `insecure_dev_mode`. |
| Third-party MCP server quality and licensing | Curated starter kit; policy enforced before every call. |
| Live search quality for Confluence/SharePoint | Indexed connectors as a Later option per source. |
| AI-written tests that pass without verifying behavior | Mutation testing, invariant suites, architecture rules, coverage gates. |
