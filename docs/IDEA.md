# Agenty — Idea

> **Status**: Proof of concept. This document is the starting point; everything else is built incrementally from it.

## The problem

Enterprises want their teams to build AI agents, but each option on the market falls short:

- Open-source agent builders put governance (SSO, RBAC, audit, policy) behind a paid edition.
- Hyperscaler and SaaS platforms tie the enterprise to one vendor's cloud, models, and pricing.
- Building in-house on agent frameworks makes engineering the bottleneck for every agent.

Agents either stay locked inside IT, or spread without central visibility into what they do, what they access, and what they cost.

## The idea

**Agenty is an open-source, self-hostable platform where teams define their own agents from building blocks the enterprise approves, while every action an agent takes is checked against policy and recorded, without the builder having to think about it.**

The bet is that governance can live in one deterministic place — the path every tool call takes — rather than in the model, the builder's diligence, or a paid edition.

## Core concepts

- **Harness** — a declarative definition (YAML) of an agent: instructions, model, granted tools, limits, and expected output. Model-driven, not a flowchart: the builder describes the situation and the limits; the agent decides the steps. The definition does not depend on any agent framework.
- **Tool** — a typed capability the agent may call, backed by an MCP server.
- **Tool Gateway** — the single path for every side effect. For each call it checks that the tool is granted, evaluates policy, executes, and records the outcome. It also owns the tool servers: it starts the MCP servers that serve a granted tool and stops them when done, so no other code holds a connection to one.
- **Policy** — rules evaluated before every tool call, producing `allow`, `deny`, or `require_approval`. Builders can tighten central policy, never loosen it. Policy only has `deny` and `require_approval` rules, and central and harness policy are evaluated as separate layers, so no layer can change another's rules.
- **Run** — one execution of a harness: the unit of tracing and audit.

## Trust model (the parts that matter from day one)

1. **The model is not trusted.** Safety comes from deterministic controls outside it, not from detecting prompt injection.
2. **Every side effect goes through the gateway.** Nothing else executes tools.
3. **Default deny.** A tool not granted to the harness is denied.
4. **Strictest wins.** Policy results combine as deny > require_approval > allow.
5. **Credentials never reach the model.** They are injected only at call time, never into prompts, logs, or tool output.
6. **Every tool call is recorded** with its policy decision and result.

## Technology

These choices carry over from earlier exploration and are settled:

- **Go** for the platform. One language, one binary, small operational surface.
- **Own agent loop** on the official model provider SDKs; no agent framework. Governance has to sit inside the tool-call path, and the harness definition must stay framework-free.
- **OPA (embedded)** as the single policy engine.
- **MCP** as the way tools are reached.
- **`log/slog`** for logging, with `charmbracelet/log` as handler.
- **testify** for Go test assertions.
- **golangci-lint** for static checks, including trust-model rules (e.g. only the Tool Gateway may execute a tool).

## Testing

Most code is AI-written, so tests are the primary correctness guarantee:

- **Test-driven**: write the failing test first.
- **Scripted model**: a deterministic `Model` implementation returns predefined responses and tool calls, so the agent loop is tested without a real model. A real-model run is optional and never gating.
- **Invariant tests**: each trust-model guarantee above has a named test that fails if it is violated.
- **High bar for the gateway, policy and secret redaction code**: these are where security lives, and they get the most thorough tests (full coverage, mutation testing when it earns its keep).
- **Table-driven tests**, `require` for preconditions, `assert` for independent checks.

Gates and tooling are added when the code they protect exists, not before.

## Proof of concept

The first goal is to prove the core loop end to end, in a single Go binary:

1. Load a harness from a YAML file.
2. Run an agent loop against one model provider.
3. Call tools on an MCP server, every call passing through the Tool Gateway.
4. Enforce grants and an OPA policy (including `require_approval`, answered on the command line).
5. Write an audit log of every decision and call.

All five are built. `agenty run` ties them together: an operator config (`agenty.yaml`: model provider, MCP servers, central policy) is kept apart from the harness, so builders never touch credentials. Config values come from the environment (`{env: NAME}`, then treated as secrets and redacted everywhere) or are plain text (`{value: TEXT}`). Approvals are asked at the terminal and rejected when stdin is not one. The audit log is a JSON-lines file, one record per decision, approval and result. The gateway starts only the MCP servers whose tools the harness grants, and refuses to start when a granted tool is not served by any of them, so a misconfigured harness fails before it runs.

To try it (needs Node.js for the example's filesystem MCP server, and `ANTHROPIC_API_KEY` in the environment or a `.env` file):

```sh
go build -o agenty ./cmd/agenty
cd examples/notes
../../agenty run --harness notes.yaml "tidy my notes"
```

The example's central policy asks before every file change and denies dotfiles; its harness allows one rewrite per run. Each run appends to `audit.jsonl`.

**Not in the PoC**: web UI, database, multi-tenancy, identity providers, schedules, knowledge retrieval, sandboxed scripts. Each comes back when the PoC shows it is needed.

## Later, if the PoC holds

The longer-term direction stays the same: a web portal for business builders, a governed catalog of tools and skills, workspaces and service accounts, approvals, durable runs, cost attribution, and a curated starter kit of MCP servers for common enterprise systems — all open source, with no paid edition.
