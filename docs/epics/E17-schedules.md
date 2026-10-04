# E17 — Schedules

> **Status**: Proposed
> **Milestone**: [M4](../ROADMAP.md#m4--sandbox-skills--schedules)
> **Depends on**: [E06](E06-run-engine.md), [E10](E10-connections-credentials.md)

## Goal

Harnesses run on schedules, personally or on behalf of a workspace.

## Capabilities covered

- Schedules
- Schedule disabling
- Background workers (scheduled)

## In scope

- Personal and workspace schedules with saved parameters; cron parsing with `gronx`.
- Scheduler role with advisory-lock leader election.
- Identity checks at fire time (owner active, harness access, valid credentials).
- Disabling on departure, credential failure, or harness retirement; in-app notification to the harness owner.
- Schedules UI.

## Out of scope

- None

## References

- ARCHITECTURE D14, §6.3

## Definition of done

In addition to the [common definition of done](README.md#common-definition-of-done):

- Tests for leader failover, missed-fire handling, and every disabling condition.

## Stories

_To be defined._
