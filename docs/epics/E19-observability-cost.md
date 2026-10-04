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

## Definition of done

In addition to the [common definition of done](README.md#common-definition-of-done):

- Tests that a budget stops a run mid-loop and that historical cost is unaffected by price changes.

## Stories

_To be defined._
