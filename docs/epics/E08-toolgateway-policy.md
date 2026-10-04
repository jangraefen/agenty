# E08 — Tool Gateway & policy

> **Status**: Proposed
> **Milestone**: [M2](../ROADMAP.md#m2--first-governed-run)
> **Depends on**: [E07](E07-agent-loop-models.md)

## Goal

Every side effect flows through one governed path with layered, non-bypassable policy.

## Capabilities covered

- Policy evaluation (OPA) before every tool call
- Untrusted-content tracking
- Central policy bundles
- Custom harness Rego
- Structured rule builder
- Centrally mandated approval gates (policy side)
- Agent identities and on-behalf-of execution (enforcement)

## In scope

- `toolgateway` pipeline (ARCHITECTURE §7.3) with an executor registry for later epics; built-in tool registration.
- Embedded OPA: decision input and output, strictest-wins combination, default deny for ungranted tools.
- Fixed, tested Rego policy evaluating structured rules stored as data.
- Custom harness Rego: restricted built-ins, validation on save, optional `opa test` suites run on save, evaluation timeouts.
- Central bundles: upload, HTTP, OCI, directory; signature verification; version recording on every decision.
- Per-run taint tracking and tool metadata fields (effect, trust, idempotency).
- Reference policies with `opa test` suites.
- UI: rule builder, custom Rego tab, policy administration for compliance.
- `depguard` rule: only `toolgateway` imports executors.

## Out of scope

- Approval UX (E09).
- Concrete executors (E11–E15).

## References

- ARCHITECTURE D8, D9, D10, §7.3–§7.5; invariants 1–4, 7

## Definition of done

In addition to the [common definition of done](README.md#common-definition-of-done):

- Invariant suites 1–4.
- 100 % branch coverage for `toolgateway` and `policy`; mutation gate passing.

## Stories

_To be defined._
