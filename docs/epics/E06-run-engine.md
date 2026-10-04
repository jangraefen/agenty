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

## Acceptance criteria

The [common acceptance criteria](README.md#common-acceptance-criteria) apply in addition to:

- [ ] **AC-E06-1** — Every run state transition in ARCHITECTURE §6.1 is implemented; any other transition is rejected. *Verified by:* Unit tests.
- [ ] **AC-E06-2** — Valid entry-point parameters create a run; invalid parameters return 422 problem details naming each parameter; callers outside the harness audience receive 403. *Verified by:* Module tests.
- [ ] **AC-E06-3** — With `wait`, a run finishing in time returns 200 with the result, otherwise 202 with a run reference; repeating a request with the same `Idempotency-Key` returns the same run without creating another. *Verified by:* Module tests.
- [ ] **AC-E06-4** — Runs started by service accounts record both the service account and the configuring person. *Verified by:* Invariant 5 suite.
- [ ] **AC-E06-5** — Each step is committed before the next begins; killing a worker between any two steps never loses or duplicates a step. *Verified by:* Crash tests (invariant 8 suite).
- [ ] **AC-E06-6** — When a worker dies mid-run, another worker resumes the run from the step log after the lease expires. *Verified by:* Crash tests.
- [ ] **AC-E06-7** — A non-idempotent tool call with unknown outcome moves the run to `needs_attention`, where it can be resumed or cancelled; idempotent calls are retried. *Verified by:* Invariant 9 suite.
- [ ] **AC-E06-8** — Activating a new harness version does not affect runs already in progress. *Verified by:* Invariant 10 suite.
- [ ] **AC-E06-9** — Server-sent events deliver step updates; reconnecting with `Last-Event-ID` resumes without gaps. *Verified by:* Module tests.
- [ ] **AC-E06-10** — The portal form is generated from the entry point's parameters and validates exactly like the API. *Verified by:* Shared-schema tests and Playwright.
- [ ] **AC-E06-11** — Run list, run detail, and the trace view render the step log. *Verified by:* Playwright.
- [ ] **AC-E06-12** — `runs` has 100 % statement coverage and meets the mutation threshold. *Verified by:* `task check`.

## Stories

_To be defined._
