# Agenty — Capabilities

> **Status**: Draft
> **Companions**: [VISION.md](VISION.md) defines why Agenty exists, its principles, and its non-goals. [ARCHITECTURE.md](ARCHITECTURE.md) defines how it is built. Every capability here must be consistent with both.

Phases:

- **v1**: required for the first release that an enterprise can run in production.
- **Later**: committed direction, not part of v1.
- **Explore**: an idea worth evaluating; not committed.

---

## 1. Building Agents

| Capability | Phase | Notes |
|---|---|---|
| Visual harness builder | v1 | Configure entry points, instructions, model, tools, skills, knowledge, rules, approvals, and output contract. |
| Entry points: chat, form, API | v1 | Every entry point requires an authenticated caller (person or service account); the run acts on their behalf. |
| Schedules | v1 | Recurring invocations of an entry point with saved parameters. Personal schedules run as their owner; workspace schedules run as a workspace service account. |
| Schedule disabling | v1 | Personal schedules are disabled when the owner leaves, loses access, or their delegated credentials expire; the harness owner is notified. All schedules of a retired harness are disabled. Without SCIM (Later), departure is detected when the person's login or credential refresh fails. |
| Typed parameters and prompt templates | v1 | One parameter definition drives API validation and the portal form; values are rendered as untrusted data. |
| Synchronous and asynchronous runs | v1 | Wait for the structured result, or receive a run reference. |
| Connector event entry points | Later | First-class integrations from external systems (e.g., "GitLab issue labeled `change`"), including payload-to-parameter mapping. |
| Skill editor | v1 | Instructions, scripts, and resources; attachable to harnesses and publishable to the catalog. |
| Skill scripts (Bash, Python, Node.js) | v1 | Run in the custom-tool sandbox; no network or credentials unless granted; policy-checked like tool calls. |
| Agent Skills format compatibility | v1 | Import and export skills in the open `SKILL.md` format. |
| Structured rule builder | v1 | Form-based conditions on tool and arguments (deny / require approval), compiled to OPA. |
| Output contracts | v1 | Required structure of an agent's result. |
| Declarative harness definition | v1 | Human-readable file (e.g., YAML) as source of truth; import and export. |
| Git-managed harnesses | Later | Harness managed in Git: read-only in Agenty, changed via commits and merge requests. No bidirectional merging. |
| Example runs and replay | v1 | Save inputs (e.g., historic tickets), replay after changes, compare outputs. Not a gate. |
| Agent-as-a-tool | Later | Published harnesses callable by other harnesses; the entry point's parameters become the tool's schema. Requires delegation chains in the identity model. |
| Workflow canvas | Later | Composition of harnesses with control flow, fan-out, and approval gates for mandated determinism. |
| Supervisor-worker and evaluator-optimizer patterns | Later | Provided as harness and workflow templates. |
| Conversational builder | Explore | Describe an agent in natural language; Agenty drafts the harness for refinement. |

## 2. Running Agents

| Capability | Phase | Notes |
|---|---|---|
| Copilots in a web portal | v1 | |
| Copilots in Slack and Microsoft Teams | Later | |
| Embeddable copilot widget | Later | For internal portals and CRM sidebars. |
| Background workers | v1 | Started via API or schedule; connector events later. |
| Durable execution | v1 | Runs survive restarts; can pause for approvals and resume. |
| Interactive and headless autonomy modes | v1 | |
| Conversation memory | v1 | Memory within a run or chat conversation only. |
| Long-term memory | Later | Explicit scopes (e.g., per user, per agent), retention rules, inspection, and erasure. |
| Bounded auto-mode | Later | Autonomous within policy and budget; falls back to interactive on high-risk actions. |
| Run versions pinned at start | v1 | In-flight runs finish on the version they started with; new runs, including those from schedules and API callers, use the harness's active version. Rollback affects new runs only. |

## 3. Catalog & Integrations

| Capability | Phase | Notes |
|---|---|---|
| Starter kit of curated connectors | v1 | Tested catalog entries and reference deployments for selected open-source MCP servers of common enterprise systems, so a fresh install is usable on day one. Initial set to be decided, e.g., GitLab, GitHub, Jira, Confluence, ServiceNow, Microsoft 365. Connector code lives outside the Agenty repository; servers must meet the dependency constraints in §10. |
| Catalog with visibility scopes | v1 | Workspace and enterprise. Department scope comes with hierarchical workspaces. |
| Review and publishing flow | v1 | Enablement team approves components before wider publication. |
| MCP client: remote servers | v1 | Streamable HTTP. Per-user OAuth (MCP authorization) enables on-behalf-of calls; servers with a static credential are bound to service accounts only. |
| MCP client: local servers | v1 | stdio MCP servers run as sandbox sessions from digest-pinned images, one instance per run and identity, with an egress allowlist. |
| MCP server | Later | Expose Agenty agents and tools to other MCP clients. |
| OpenAPI 3.x import | v1 | Turns internal REST APIs into typed tools. |
| Credential brokering | v1 | OAuth2 (incl. PKCE), on-behalf-of delegation, service account credentials. Write-only secrets, never exposed to models, scripts, or builders. mTLS later. |
| Service account credentials | v1 | Agenty-issued API tokens or OAuth client credentials for calling entry points. Per-connector downstream credentials: username/password (rotation reminders, can be disallowed by policy), OAuth client credentials (preferred), OAuth authorization-code grants (records who consented). Token exchange and external secret stores (e.g., Vault) later. |
| Sandboxed custom tools (Python, Node.js) | v1 | Ephemeral, resource-limited, egress allowlist. Published by the enablement team or by technical builders. |
| Scoped tool publishing | v1 | Technical builders publish to their team scope; wider scopes require review. Business builders use tools but do not create them. The same review applies to skills that contain scripts. |
| Knowledge connectors | v1 | File upload and S3-compatible storage, indexed in pgvector with hybrid search. Jira, ServiceNow, SQL later. |
| Live search for Confluence and SharePoint | Later | Searched through their own search APIs with the caller's delegated credential (no copies, exact permissions). |
| Indexed connectors with ACL sync | Later | Semantic indexing of Confluence, SharePoint, and others where live search quality is insufficient. |
| Permission-aware retrieval | v1 | Enforce source-system ACLs for the acting identity. |
| Citations | v1 | Link answers back to source documents and passages. |

## 4. Models

| Capability | Phase | Notes |
|---|---|---|
| Major commercial providers | v1 | Via their APIs or the customer's private endpoints. |
| OpenAI-compatible endpoints | v1 | Covers enterprise AI gateways and most self-hosted runtimes. |
| Per-harness model configuration | v1 | Model configurations published centrally in the catalog. |
| Failover chains | Later | |
| Reference setups for local runtimes | Later | Documentation only (e.g., vLLM, Ollama) via OpenAI-compatible endpoints; no dedicated product integration. |

## 5. Governance

| Capability | Phase | Notes |
|---|---|---|
| Policy evaluation (OPA) before every tool call | v1 | Strictest result wins (deny > require approval > allow); ungranted tools are denied. |
| Central policy bundles | v1 | Compliance-authored Rego, uploaded or loaded as OPA bundles (HTTP, OCI, directory); signed bundles supported; every decision records the policy version. |
| Custom harness Rego | v1 | Technical builders add Rego to a harness; restrict-only (deny / require approval), restricted built-ins, validated on save, optional `opa test` suites. |
| Untrusted-content tracking | v1 | Policies can restrict actions influenced by untrusted input. |
| Centrally mandated approval gates | v1 | Builders can add gates, not remove mandated ones. |
| Approvals inbox (web) | v1 | Personal context: the run's user approves. Workspace context: any workspace member approves. |
| Designated approvers and four-eyes | Later | Named approvers or roles, and approvals that must come from someone other than the caller. |
| Approvals in Slack and Microsoft Teams | Later | |
| Approval evidence | Later | Approval and rejection rates and approver edits per action, to support deliberate relaxation of approval requirements. Never automatic. |
| Approval SLAs and escalation | Later | Timeouts route to a secondary approver or a safe fallback. |
| PII detection and masking | Later | Before prompts reach model providers. |
| Evaluator agents | Later | Quality control only, never a security control. |

## 6. Identity & Access

| Capability | Phase | Notes |
|---|---|---|
| OIDC and SAML single sign-on | v1 | |
| SCIM provisioning | Later | |
| Workspaces | v1 | Team collaboration under common ownership; isolation of harnesses, schedules, service accounts, data, and budgets. No credentials of their own. |
| Workspace service accounts | v1 | Non-human identities for API calls and workspace schedules; usable by every member of the workspace; every run records the configuring person. |
| RBAC | v1 | |
| Harness audience | v1 | Who may call a harness: workspace members only, specific IdP groups, or all authenticated users. Groups come from OIDC/SAML claims until SCIM is available. |
| Attribute-based access via policy | Later | |
| Agent identities and on-behalf-of execution | v1 | Intersection of agent and caller permissions. |
| Hierarchical workspaces (department → team) | Later | |

## 7. Observability, Audit & Cost

| Capability | Phase | Notes |
|---|---|---|
| Run traces | v1 | Prompts, tool calls, policy decisions, approvals, outputs. |
| OpenTelemetry export | v1 | |
| Append-only audit log with crypto-shredding | v1 | |
| Token usage per run, step, harness, workspace | v1 | All models. |
| Currency cost for commercial models | v1 | Based on operator-configured prices. |
| Budget caps that stop runs | v1 | Per harness and per workspace. |
| Cost-center attribution | Later | |
| Budget alerts | Later | |
| Usage export API | Later | |

## 8. Lifecycle

| Capability | Phase | Notes |
|---|---|---|
| Immutable versioning and rollback | v1 | For harnesses, workflows, and catalog components. |
| Playground | v1 | |
| Workspace ownership and owner reassignment | v1 | Harnesses belong to the workspace; a departed owner is replaced by the workspace administrator. |
| Lifecycle states | v1 | Draft, active, deprecated, retired. |
| Central inventory | v1 | Every harness across workspaces with owner, tools, data access, cost, last run, and its schedules (including disabled ones). |
| Inactivity detection | Later | Flag harnesses without runs for a configurable period. |
| Environments (development, staging, production) | Later | |
| Promotion approvals | Later | |
| Dry-run of side effects | Explore | |

## 9. Deployment

| Capability | Phase | Notes |
|---|---|---|
| Self-hosted container deployment | v1 | Concrete topology is an architecture decision, not a vision decision. |
| No phone-home | v1 | |
| High availability | Later | |
| Offline / air-gapped installation | Explore | Possible, not guaranteed (see VISION non-goals). |

---

## 10. Inputs for Architecture

> The system architecture, including how these constraints are met, is specified in [ARCHITECTURE.md](ARCHITECTURE.md).

### Constraints derived from the vision

- **Open, self-hostable dependencies.** Every v1 capability must work with required dependencies that are open source and free to self-host, without needing a commercial tier of any of them (VISION principle 3). Commercial products may be supported as optional integrations. Dependency licenses must also permit offering Agenty as a hosted service (VISION, Sustainability).
- **Operability.** A regular enterprise IT team must be able to run Agenty (principle 6); minimize the number of distinct stateful components.
- **Framework-agnostic definition.** The harness definition must not leak the agent framework or runtime chosen internally (principle 5). The runtime may use an existing framework.
- **Deterministic enforcement outside the model.** Identity, policy (OPA), approvals, and untrusted-content tracking sit in the execution path of every tool call and script execution, not in prompts (VISION trust model).
- **The reference scenarios** in VISION are the end-to-end acceptance cases for v1.

### Decisions deliberately left to the architecture

- Implementation language(s) and agent framework or runtime.
- Durable execution approach (runs that pause for days and resume).
- Deployment topology and minimum footprint.
- Storage: relational state, audit log, documents and embeddings for retrieval.
- Sandbox technology for custom tools and skill scripts.
- Mechanism for untrusted-content tracking and how policies consume it.
- Key management for crypto-shredding.
- Credential broker design: storage, encryption, delegated token refresh.
- How starter-kit connectors are packaged and run (likely MCP servers).
- The declarative harness definition schema.
- API design for entry points, synchronous and asynchronous runs.
