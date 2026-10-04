# Agenty — Product Vision

> **Status**: Draft
> **Companion**: [CAPABILITIES.md](CAPABILITIES.md) lists concrete capabilities and their phasing.

---

## 1. Why Agenty Exists

Enterprises want their teams to build AI agents. Today they have three options, and none of them fits well:

- **Open-source agent builders** are easy to start with, but the features an enterprise needs before it can let teams use them in production (SSO, RBAC, workspace isolation, audit trails, central policy) are often reserved for a paid or cloud edition.
- **Hyperscaler and SaaS agent platforms** ship with governance, but tie the enterprise to one vendor's cloud, models, and pricing.
- **Building in-house** on agent frameworks turns engineering teams into the bottleneck for every agent a business team wants.

The result: either agents stay locked inside IT, or they spread across teams without central visibility into what they do, what they access, and what they cost.

## 2. Vision Statement

**Agenty is a fully open-source, self-hostable platform where anyone in an enterprise — technical or not — can define their own agent harnesses from building blocks the enterprise provides and approves, while central teams keep visibility and control over what agents do, what data they touch, and what they cost.**

No enterprise edition. No feature paywall. Everything an enterprise needs to run Agenty in production is part of the open-source product, free to self-host.

## 3. What Makes Agenty Different

1. **No enterprise tax.** SSO, RBAC, workspaces, audit trails, policy enforcement, and cost attribution are core features, not upsells.
2. **Harnesses, not flowcharts.** Agents are model-driven: builders describe the situation, the available tools, and the limits, and the agent decides the steps. Builders define the whole harness around a model — entry points, instructions, tools, skills, knowledge, rules, approvals, and expected output. What a developer achieves with an agent framework in code, a business builder achieves by composing catalog tools and writing skills in plain language.
3. **Central enablement, decentralized building.** Central teams publish the building blocks — connectors, tools, skills, templates, model configurations, policies — and set standards once. Teams compose agents from what is approved instead of waiting for IT to build them. Agenty ships with a curated starter kit of tested catalog entries for open-source MCP servers of common enterprise systems, so builders can be productive from day one rather than waiting for a catalog to be filled.
4. **Governed by default.** Every agent action passes through identity, policy, and audit, whether or not the builder knows these mechanisms exist.

## 4. Principles

Principles are ranked. When two conflict, the higher one wins.

1. **Governance over accessibility.** A capability that cannot be governed — attributed to an identity, checked against policy, recorded in the audit trail — does not ship, however convenient it would be.
2. **Safe by default.** Builders should not need to understand policy to be safe. Safe defaults come from the catalog and the workspace, not from the builder's diligence.
3. **Nothing withheld.** No capability is reserved for a paid edition.
4. **Open standards over bespoke integrations.** MCP, OpenAPI, OpenTelemetry, OIDC/SAML, SCIM, OPA. Prefer the standard even when a custom integration would be faster.
5. **One declarative definition.** Every harness has a declarative, human-readable representation (e.g., YAML) that is its source of truth. The definition does not expose or depend on any particular agent framework. Each harness is managed in exactly one place: either in Agenty (edited in the visual builder, exportable at any time) or in Git (read-only in Agenty, changed through commits and merge requests). There is no bidirectional merging.
6. **Operable by a regular enterprise IT team.** Running Agenty must not require a dedicated AI platform team.
7. **Model agnostic.** No dependency on a single model vendor. Any model reachable through a supported provider API or enterprise AI gateway can be used.

## 5. Personas

| Group | Persona | Goal |
|---|---|---|
| **Builders** | **Business Builder** | Domain expert who builds agents (and later workflows) visually from catalog components, without writing code. |
| | **Technical Builder** | Developer who version-controls harness definitions, writes custom tools, and publishes them to their team's scope; wider publication goes through review. |
| | **Enablement Team** | Central team (IT, AI center of excellence) that publishes connectors, tools, templates, model configurations, and policies to the catalog and sets standards. |
| **Agent users** | **End User** | Employee who talks to copilots or receives the output of background workers. |
| | **Approver** | Signs off on actions that require human approval. Needs enough context to decide quickly and is accountable for the decision. |
| | **Agent Owner** | Accountable business owner of a deployed agent: its purpose, its access, its lifecycle, and its retirement. |
| **Central oversight** | **Platform Operator** | Installs, upgrades, and scales Agenty; connects identity providers and model providers. |
| | **Compliance & Security** | Defines policies, reviews audit trails, investigates incidents. |
| | **FinOps / Cost Owner** | Tracks consumption, sets budgets, allocates cost to departments. |

## 6. Core Concepts

Shared vocabulary for all future specifications.

- **Agent Harness** — The versioned definition of an agent and Agenty's primary building unit. With typed entry points and an output contract, a harness behaves like a typed function — inputs in, structured result out — which makes it easy to use from other automations and by other agents. It consists of:
  - **Entry points** — how a run is started: chat, a form in the portal, or an authenticated API call. Each entry point defines typed parameters and a prompt template that turns them into the run's first prompt. The same parameter definition drives API validation and the portal form.
  - **Instructions** — the agent's role, goal, and behavior, in plain language.
  - **Model configuration** — selected from centrally approved configurations.
  - **Tools** — capabilities from the catalog the agent may use, each with a permission scope (e.g., read-only vs. write).
  - **Skills** — reusable know-how (runbooks, taxonomies, procedures, and supporting scripts) the agent loads when relevant.
  - **Knowledge** — document sources for retrieval.
  - **Rules** — deterministic limits on tool use (e.g., "never close priority 1–2 tickets"), built with a structured rule builder and enforced as policy.
  - **Approvals** — which actions require human sign-off, and by whom.
  - **Output contract** — the structure the agent's result must have.
  - **Identity and grants** — what the agent itself is allowed to access.
  - **Audience** — who may call the harness: workspace members only, specific groups, or everyone in the enterprise (e.g., an HR copilot for all employees).
- **Tool** — A typed, permission-scoped capability (e.g., "ServiceNow: close request"), backed by an MCP server, an imported OpenAPI operation, or sandboxed custom code. Tools are published by the enablement team or by technical builders; business builders use them but do not create them.
- **Skill** — A package of domain expertise: plain-language instructions, optionally bundled with scripts (Bash, Python, Node.js) and resources (templates, reference data) the agent can use. Skills follow the open Agent Skills format where possible. They are the primary way builders give agents expertise: business builders write the instructions, and scripts let technical builders — or business builders who are comfortable with a little code — encode logic that is better expressed deterministically.
- **Workflow** — A composition of harnesses with control flow and approval gates, for processes that require mandated determinism (fixed step order, fan-out, hand-offs between teams). Most agents need no workflow. Workflows are graphs, not DAGs: loops and retries are allowed.
- **Catalog** — The governed registry of reusable building blocks (connectors, tools, skills, harness templates, model configurations, policies), each published with a visibility scope (workspace or enterprise; department scopes follow with hierarchical workspaces).
- **Workspace** — The space where a team collaborates under common ownership, and the unit of isolation between teams. Harnesses, workspace schedules, service accounts, and budgets belong to a workspace; members hold roles in it. A workspace holds no credentials of its own: credentials always belong to an identity — a person or a service account.
- **Service Account** — A non-human identity belonging to a workspace, for work that must not depend on any individual. It can call entry points (e.g., from other automations) and run workspace schedules. Every member of the workspace can use its service accounts, so workspace membership is a security boundary: joining a workspace grants access to what its service accounts can reach. Service accounts are configured in Agenty and hold credentials for downstream systems (e.g., technical-user credentials or OAuth grants). These credentials are stored in Agenty's credential broker, never exposed to models, scripts, or builders, and must represent non-human accounts in the target systems.
- **Schedule** — A recurring invocation of a harness entry point with saved parameter values ("run this harness every Monday with these parameters, as me"). Schedules are separate from the harness, so several people or teams can schedule the same harness with different parameters. A **personal schedule** belongs to a person and runs as that person; a **workspace schedule** belongs to a workspace and runs as one of its service accounts.
- **Run** — A single execution of a harness or workflow; the unit of tracing, auditing, and cost attribution.
- **Example Run** — A saved input (and optionally a reviewed output) attached to a harness. Builders replay their examples after changing a prompt, tool, or model to see what changed. Examples are how builders gain confidence in a change; they are not a formal test suite and do not gate deployment.

## 7. How Agents Run

- **Interactive copilots** serve users on demand in a web portal and in collaboration tools such as Slack or Microsoft Teams.
- **Background workers** are started by other automations through the API, or by schedules, and execute long-running processes without a user watching. First-class event integrations with external systems (e.g., "GitLab issue labeled") come in a later phase.
- **Runs can be synchronous or asynchronous.** A caller can wait for the structured result, or receive a run reference and collect the result later — required whenever a run may pause for approval.
- **Durable execution.** Runs survive restarts and can pause — for an approval, for days if needed — and resume where they left off.
- **Autonomy is configured per harness**: from confirming every side effect, to bounded autonomy within policy, to fully headless execution governed only by policy and approval gates.
- **Memory is run-scoped at first.** Initially, agents remember only within a run or conversation; lasting knowledge comes from knowledge sources. Long-term memory follows later, with explicit scopes, retention rules, and the ability to inspect and erase it.

## 8. Ownership & Lifecycle

As the number of builders grows, agents must not outlive their purpose or their accountability.

- **Workspaces own, people are accountable.** Every harness belongs to a workspace and has a named Agent Owner. When an owner leaves, the harness keeps running; the workspace administrator assigns a new owner.
- **Personal schedules end with the person.** A personal schedule is disabled when its owner leaves, loses access to the harness, or their delegated credentials expire or are revoked; the harness owner is notified. Processes that must survive personnel changes use workspace schedules. Any schedule of a retired harness is disabled.
- **Explicit lifecycle states.** Harnesses move through draft, active, deprecated, and retired. Callers and schedule owners of deprecated harnesses are warned; retired harnesses cannot run.
- **Inactivity is surfaced.** Harnesses without runs for a configurable period are flagged for their owner's review.
- **Central inventory.** Central teams see every harness across workspaces: owner, tools, data access, cost, and last run.

## 9. Reference Scenarios

Two agents originally built in code with an agent framework (Strands). Agenty must let a business builder build both without writing code; they serve as acceptance scenarios for the architecture.

**Issue-to-change bot.** A harness reads GitLab issues and opens change requests in ServiceNow.
- Entry point: API, called by an existing automation with the issue reference as a typed parameter.
- Tools: GitLab (read issue, comment), ServiceNow (create change request).
- Skill: how to fill a change request (template, risk classification).
- Rules and approvals: creating a change request requires approval until the owner relaxes it.
- Output contract: change request number and link, also posted back to the issue.

**Service request triage.** A harness categorizes and analyzes incoming service requests, and diagnoses and closes some of them.
- Entry point: workspace schedule (service account), polling new requests in an assignment group.
- Tools: ServiceNow (read, categorize, comment, close), directory lookup and device status (read-only).
- Skills: categorization taxonomy; diagnosis runbooks per category, some with scripts.
- Rules: close only when the category is on an allowed list; never touch priority 1–2 requests.
- Approvals: closing requires approval initially.
- Example runs: historic requests with known outcomes, replayed after every change.

## 10. Trust Model

What Agenty assumes, and what it guarantees.

- **The model is not trusted.** Any agent that processes untrusted content (emails, documents, tickets, web pages) is assumed to be steerable by that content. Safety comes from deterministic controls outside the model, not from detecting prompt injections.
- **Every run has an authenticated caller.** Every entry point requires a valid login — a person, or a service account for automations. Personal scheduled runs act on behalf of their owner, using credentials that person has delegated; workspace scheduled runs act as a workspace service account. No one is present during scheduled runs, so anything requiring confirmation goes through approvals.
- **Service accounts do not launder permissions.** Only members of its workspace can use a service account, and every run records both the service account and the person who configured the call or schedule.
- **Every action has an identity.** Agents have their own identity with explicit grants. A run acts on behalf of its caller, limited to the intersection of the agent's and the caller's permissions. Delegation chains (an agent calling an agent) are recorded and can never widen permissions.
- **Parameters are data, not instructions.** Values passed into a prompt template are rendered as clearly delimited data and treated as untrusted content, so they cannot silently rewrite the agent's instructions.
- **Policy before every side effect.** Every tool call is evaluated against policy (OPA) before it executes. Content from untrusted sources is tracked through the run, and policies can restrict which actions such content may influence.
- **Code is governed wherever it lives.** Scripts in skills and custom tools run in isolated sandboxes with no network access or credentials unless explicitly granted, and their execution is subject to policy like any other tool call. Skills containing scripts follow the same publishing review as tools beyond team scope.
- **Humans approve what is high-risk.** Approval requirements can be mandated centrally by policy and added per harness. Builders can add approval gates but cannot remove centrally mandated ones. Approval requirements never relax automatically: Agenty shows the evidence (approval and rejection rates, edits made by approvers), and an owner or compliance officer relaxes them deliberately.
- **Approvals go to whoever the run belongs to.** In a personal context (a user's session, API call, or personal schedule), that user approves. In a workspace context (e.g., a workspace schedule), any workspace member can approve. Every decision is recorded with the approver's identity. Designated approvers and separation of duties (four-eyes) come later.
- **Retrieval respects source permissions.** Agents only retrieve what the identity they act for is allowed to access in the source system.
- **LLM-based review is quality control, not a security control.** Evaluator agents may check accuracy or tone; they never replace deterministic enforcement.
- **Audit without hoarding.** Audit events are append-only. Personal data within them is encrypted per data subject, so erasure requests are honored by destroying the key (crypto-shredding) while the event trail itself remains intact.
- **No phone-home.** Agenty itself makes no outbound calls beyond those the operator configures.

## 11. Non-Goals

- **Not a flow builder first.** Agenty does not ask builders to draw every step; the canvas exists for the minority of processes that need mandated determinism.
- **Not an iPaaS.** Deterministic integration steps exist to serve agents. Agenty does not compete with general-purpose integration and automation platforms.
- **Not a general-purpose chat assistant.** Copilots are purpose-built agents, not a replacement for a general AI chat product.
- **No model training, fine-tuning, or serving.** Agenty consumes models; it does not train or host them.
- **No paid edition.** No features are withheld from the open-source product.
- **No air-gap guarantee.** Agenty is self-hostable and does not depend on external services of its own. Deployments that use commercial model APIs or SaaS collaboration tools send data to those services by the operator's choice.

## 12. Sustainability

Agenty is fully open source and free to self-host. Long-term development is funded through support and services — support contracts, SLAs, and consulting — and possibly through a paid hosted offering later. Neither ever gates features: a hosted Agenty is the same product, operated for the customer.

## 13. Open Questions

None at the moment. New questions are recorded here as they arise.
