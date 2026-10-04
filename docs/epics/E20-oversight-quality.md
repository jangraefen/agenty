# E20 — Oversight & quality

> **Status**: Proposed
> **Milestone**: [M6](../ROADMAP.md#m6--governance--release)
> **Depends on**: [E06](E06-run-engine.md), [E16](E16-chat-copilots.md), [E19](E19-observability-cost.md)

## Goal

Central teams keep oversight, and builders gain confidence in changes.

## Capabilities covered

- Central inventory
- Workspace ownership and owner reassignment
- Playground
- Example runs and replay

## In scope

- Inventory across workspaces: owner, tools, data access, cost, last run, schedules (including disabled).
- Owner reassignment by workspace administrators.
- Playground for trying draft harness versions.
- Example runs: save inputs (and optional reviewed outputs), replay against a version, compare results.

## Out of scope

- Inactivity detection, environments, promotion (Later).

## References

- VISION §6, §8

## Acceptance criteria

The [common acceptance criteria](README.md#common-acceptance-criteria) apply in addition to:

- [ ] **AC-E20-1** — The inventory lists every harness across workspaces with owner, tools, data access, cost, last run, and schedules (including disabled ones); only central roles can see it. *Verified by:* Module tests and Playwright.
- [ ] **AC-E20-2** — Workspace administrators can reassign a harness owner; the change is audited. *Verified by:* Module tests.
- [ ] **AC-E20-3** — The playground runs a draft version without activating it. *Verified by:* Integration tests.
- [ ] **AC-E20-4** — Example runs can be saved, replayed against a chosen version, and compared side by side. *Verified by:* Integration tests and Playwright.
- [ ] **AC-E20-5** — The replay side-effect strategy is decided, recorded in ARCHITECTURE §2, and enforced: replay never repeats real side effects. *Verified by:* Integration tests.

## Notes

- Open design question: replaying examples must not cause real side effects (e.g., closing tickets again). Options to decide when stories are written: force approval or deny for write effects during replay, or replay with recorded tool results.

## Stories

_To be defined._
