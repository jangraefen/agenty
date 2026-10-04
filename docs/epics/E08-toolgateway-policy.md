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

## Acceptance criteria

The [common acceptance criteria](README.md#common-acceptance-criteria) apply in addition to:

- [ ] **AC-E08-1** — Executors are reachable only through `toolgateway`. *Verified by:* `depguard` rule and invariant 1 suite.
- [ ] **AC-E08-2** — Calls to tools not granted to the harness's agent principal are denied. *Verified by:* Invariant 2 suite.
- [ ] **AC-E08-3** — For every combination of layer results, the strictest wins; `allow` or other outputs from harness layers have no effect. *Verified by:* Invariant 3 suite (table-driven).
- [ ] **AC-E08-4** — Effective permissions are the intersection of agent grants and caller permissions. *Verified by:* Invariant 4 suite.
- [ ] **AC-E08-5** — Policy evaluation errors and timeouts deny the call (fail closed). *Verified by:* Unit tests.
- [ ] **AC-E08-6** — Structured rules are stored as data and evaluated by the fixed Rego policy, which ships with an `opa test` suite. *Verified by:* `opa test` and module tests.
- [ ] **AC-E08-7** — Custom harness Rego with compile errors, disallowed rule names, or forbidden built-ins (such as `http.send`) is rejected on save with a clear message; attached `opa test` suites run on save. *Verified by:* Module tests.
- [ ] **AC-E08-8** — Central bundles load by upload, HTTP, OCI registry, and directory; bundles with invalid signatures are rejected when signing is required; every `policy_decision` step records the bundle versions used. *Verified by:* Module tests.
- [ ] **AC-E08-9** — Untrusted parameters and tool outputs mark the run as tainted and add their source; policy input contains both. *Verified by:* Module tests.
- [ ] **AC-E08-10** — Reference policies require approval for writes in tainted runs and deny `external_communication` in tainted runs; they ship with `opa test` suites. *Verified by:* `opa test`.
- [ ] **AC-E08-11** — The rule builder, custom Rego tab, and compliance policy administration work in the UI. *Verified by:* Playwright with axe-core.
- [ ] **AC-E08-12** — `toolgateway` and `policy` have 100 % statement coverage and meet the mutation threshold. *Verified by:* `task check`.

## Stories

_To be defined._
