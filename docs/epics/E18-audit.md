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

## Definition of done

In addition to the [common definition of done](README.md#common-definition-of-done):

- Invariant 11 suite; erasure test proving payloads become unreadable while the chain verifies.

## Stories

_To be defined._
