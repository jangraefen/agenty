# E19 — Observability & cost

> **Status**: Proposed
> **Milestone**: [M5](../ROADMAP.md#m5--copilots--knowledge)
> **Depends on**: [E07](E07-agent-loop-models.md)

## Goal

Operators and FinOps see what runs do and what they cost, and budgets stop runaway runs.

## Capabilities covered

- OpenTelemetry export
- Token usage per run, step, harness, workspace
- Currency cost for commercial models
- Budget caps that stop runs

## In scope

- OpenTelemetry spans following GenAI semantic conventions; OTLP export only when configured.
- Usage records per model step; prices on model configurations with validity periods.
- Budgets per run, harness, workspace; check before each model call; `budget_exceeded`.
- FinOps views: usage and cost per workspace and harness.

## Out of scope

- Cost-center attribution, alerts, export API (Later).

## References

- ARCHITECTURE §12.2, §12.3

## Acceptance criteria

The [common acceptance criteria](README.md#common-acceptance-criteria) apply in addition to:

- [ ] **AC-E19-1** — Spans exist for runs, model calls, tool calls, policy decisions, and sandbox executions with GenAI attributes; they are exported via OTLP only when an endpoint is configured. *Verified by:* Module tests with a test collector.
- [ ] **AC-E19-2** — Each model step records tokens and, for commercial models, cost at the price valid at that time; changing a price does not change historical cost; local models record tokens only. *Verified by:* Module tests.
- [ ] **AC-E19-3** — Budgets per run, harness, and workspace are checked before every model call; exceeding one fails the run with `budget_exceeded`, also mid-loop. *Verified by:* Integration tests.
- [ ] **AC-E19-4** — FinOps views show usage and cost per workspace and harness over time. *Verified by:* Playwright with axe-core.

## Stories

_To be defined._
