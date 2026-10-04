# Agenty — Capabilities

> **Status**: Draft
> **Companion**: [VISION.md](VISION.md) defines why Agenty exists, its principles, and its non-goals. Every capability here must be consistent with it.

Phases:

- **v1**: required for the first release that an enterprise can run in production.
- **Later**: committed direction, not part of v1.
- **Explore**: an idea worth evaluating; not committed.

---

## 1. Building Agents

| Capability | Phase | Notes |
|---|---|---|
| Visual harness builder | v1 | Configure entry points, instructions, model, tools, skills, knowledge, rules, approvals, and output contract. |
| Entry points: chat, form, API, schedule | v1 | Every entry point requires an authenticated caller (person or service account); the run acts on their behalf. |
| Typed parameters and prompt templates | v1 | One parameter definition drives API validation and the portal form; values are rendered as untrusted data. |
| Synchronous and asynchronous runs | v1 | Wait for the structured result, or receive a run reference. |
| Connector event entry points | Later | First-class integrations from external systems (e.g., "GitLab issue labeled `change`"), including payload-to-parameter mapping. |
| Skill editor | v1 | Instructions, scripts, and resources; attachable to harnesses and publishable to the catalog. |
| Skill scripts (Bash, Python, Node.js) | v1 | Run in the custom-tool sandbox; no network or credentials unless granted; policy-checked like tool calls. |
| Agent Skills format compatibility | v1 | Import and export skills in the open `SKILL.md` format. |
| Structured rule builder | v1 | Form-based conditions on tool and arguments (deny / require approval), compiled to OPA. |
| Output contracts | v1 | Required structure of an agent's result. |
| Declarative harness definition | v1 | Human-readable file (e.g., YAML) as source of truth; import and export. |
| Git sync of harness definitions | Later | See VISION open question on source of truth. |
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
| Bounded auto-mode | Later | Autonomous within policy and budget; falls back to interactive on high-risk actions. |
| Run versions pinned at start | v1 | In-flight runs finish on the version they started with; rollback affects new runs only. |

## 3. Catalog & Integrations

| Capability | Phase | Notes |
|---|---|---|
| Starter kit of maintained connectors | v1 | Common enterprise systems, so a fresh install is usable on day one. Initial set to be decided, e.g., GitLab, GitHub, Jira, Confluence, ServiceNow, Microsoft 365. Likely delivered as MCP servers. |
| Catalog with visibility scopes | v1 | Team, department, enterprise. |
| Review and publishing flow | v1 | Enablement team approves components before wider publication. |
| MCP client | v1 | |
| MCP server | Later | Expose Agenty agents and tools to other MCP clients. |
| OpenAPI 3.x import | v1 | Turns internal REST APIs into typed tools. |
| Credential brokering | v1 | OAuth2 (incl. PKCE), service accounts, on-behalf-of delegation. mTLS later. |
| Sandboxed custom tools (Python, Node.js) | v1 | Ephemeral, resource-limited, egress allowlist. Published by the enablement team or by technical builders. |
| Scoped tool publishing | v1 | Technical builders publish to their team scope; wider scopes require review. Business builders use tools but do not create them. The same review applies to skills that contain scripts. |
| Knowledge connectors | v1 | Start with file upload, S3-compatible storage, Confluence, SharePoint. Jira, ServiceNow, SQL later. |
| Permission-aware retrieval | v1 | Enforce source-system ACLs for the acting identity. |
| Citations | v1 | Link answers back to source documents and passages. |

## 4. Models

| Capability | Phase | Notes |
|---|---|---|
| Major commercial providers | v1 | Via their APIs or the customer's private endpoints. |
| OpenAI-compatible endpoints | v1 | Covers enterprise AI gateways and most self-hosted runtimes. |
| Per-harness model configuration | v1 | Model configurations published centrally in the catalog. |
| Failover chains | Later | |
| Dedicated local runtime support | Explore | Only if OpenAI-compatible endpoints prove insufficient. |

## 5. Governance

| Capability | Phase | Notes |
|---|---|---|
| Policy evaluation (OPA) before every tool call | v1 | |
| Untrusted-content tracking | v1 | Policies can restrict actions influenced by untrusted input. |
| Centrally mandated approval gates | v1 | Builders can add gates, not remove mandated ones. |
| Approvals inbox (web) | v1 | |
| Approvals in Slack and Microsoft Teams | Later | |
| Gradual trust | Explore | Approval requirements that relax as an agent proves itself (see VISION open questions). |
| Approval SLAs and escalation | Later | Timeouts route to a secondary approver or a safe fallback. |
| PII detection and masking | Later | Before prompts reach model providers. |
| Evaluator agents | Later | Quality control only, never a security control. |

## 6. Identity & Access

| Capability | Phase | Notes |
|---|---|---|
| OIDC and SAML single sign-on | v1 | |
| SCIM provisioning | Later | |
| Workspaces | v1 | Isolation of agents, credentials, data, budgets. |
| RBAC | v1 | |
| Attribute-based access via policy | Later | |
| Agent identities and on-behalf-of execution | v1 | Intersection of agent and user permissions. |
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
| Agent ownership and transfer | v1 | Every deployed agent has an owner (see VISION, Agent Owner). |
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
