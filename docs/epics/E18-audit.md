# E18 — Audit

> **Status**: Proposed
> **Milestone**: [M6](../ROADMAP.md#m6--governance--release)
> **Depends on**: [E06](E06-run-engine.md)

## Goal

Compliance gets a tamper-evident, privacy-aware audit trail.

## Capabilities covered

- Append-only audit log with crypto-shredding

## In scope

- Audit events for runs, policy decisions, approvals, and administrative actions.
- INSERT-only database role; hash chain with a verification command; optional periodic hash export.
- Per-person keys and encryption of personal data in step payloads; erasure procedure.
- Payload retention job.
- Compliance read API and UI.

## Out of scope

- None

## References

- ARCHITECTURE §12.1; invariant 11

## Acceptance criteria

The [common acceptance criteria](README.md#common-acceptance-criteria) apply in addition to:

- [ ] **AC-E18-1** — Audit events are emitted for every action type in a documented event catalog (runs, policy decisions, approvals, administrative changes). *Verified by:* Tests per event type.
- [ ] **AC-E18-2** — `agenty audit verify` validates the hash chain and detects any modified or deleted event. *Verified by:* Module test that tampers via a privileged role.
- [ ] **AC-E18-3** — The application's database role can only insert audit events. *Verified by:* Invariant 11 suite.
- [ ] **AC-E18-4** — Personal data in step payloads is encrypted per person; deleting a person's key makes their payloads unreadable while the chain still verifies. *Verified by:* Module tests.
- [ ] **AC-E18-5** — The retention job deletes payloads older than the configured period and keeps metadata. *Verified by:* Module tests.
- [ ] **AC-E18-6** — When configured, the latest chain hash is exported periodically to a file or HTTP endpoint. *Verified by:* Module tests.
- [ ] **AC-E18-7** — Compliance and administrators can query audit events with filters in the API and UI; other roles cannot. *Verified by:* Module tests and Playwright.

## Stories

_To be defined._
