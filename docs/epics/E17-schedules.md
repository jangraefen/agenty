# E17 — Schedules

> **GitHub issue**: [#17](https://github.com/jangraefen/agenty/issues/17) — status, progress, and stories are tracked there
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

## Acceptance criteria

The [common acceptance criteria](README.md#common-acceptance-criteria) apply in addition to:

- **AC-E17-1** — Personal and workspace schedules can be created with saved parameters; invalid cron expressions are rejected. *Verified by:* Module tests.
- **AC-E17-2** — Exactly one scheduler is leader; when it dies, another takes over within the lock timeout. *Verified by:* Module tests.
- **AC-E17-3** — Personal schedules fire as their owner, workspace schedules as the selected service account, with the saved parameters. *Verified by:* Integration tests.
- **AC-E17-4** — Fires missed during downtime are skipped and recorded, not replayed. *Verified by:* Module tests.
- **AC-E17-5** — Fire-time checks fail closed; departure, credential invalidation, lost harness access, and harness retirement disable the affected schedules with a reason and notify the harness owner. *Verified by:* Module tests for each condition.
- **AC-E17-6** — Schedules can be managed in the UI, which shows disabled schedules and their reason. *Verified by:* Playwright with axe-core.

## Stories

Stories are tracked as sub-issues of [#17](https://github.com/jangraefen/agenty/issues/17).
