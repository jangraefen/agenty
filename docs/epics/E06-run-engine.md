# E06 — Run engine & entry points

> **Status**: Proposed
> **Milestone**: [M2](../ROADMAP.md#m2--first-governed-run)
> **Depends on**: [E05](E05-harness-definitions.md)

## Goal

Harnesses can be started through the API and portal forms, run durably, and be observed.

## Capabilities covered

- Entry points: form, API
- Synchronous and asynchronous runs
- Background workers (API-started)
- Durable execution
- Run versions pinned at start
- Run traces
- Headless autonomy mode

## In scope

- Run state machine with all transitions from ARCHITECTURE §6.1, including `needs_attention` with resume and cancel.
- Append-only step log.
- Claiming (`FOR UPDATE SKIP LOCKED`), leases with heartbeats, retries with backoff and jitter.
- Entry point API: parameter validation, audience check, `wait` with 200/202, `Idempotency-Key`, recording of service account and configuring person.
- Server-sent events for run updates.
- Portal form entry point generated from parameter definitions.
- Run list, run detail, and trace view rendered from the step log.
- Fault-injection framework for crash tests; a stub loop executor until E07 provides the agent loop.

## Out of scope

- Agent loop and models (E07).
- Chat (E16).
- Schedules (E17).

## References

- ARCHITECTURE D3, D4, §6, §13.1; invariants 8, 9, 10

## Definition of done

In addition to the [common definition of done](README.md#common-definition-of-done):

- Crash tests: runs resume after worker death; non-idempotent unknown outcomes land in `needs_attention`.
- 100 % branch coverage for `runs`.
- Invariant suites 8, 9, 10.

## Stories

_To be defined._
