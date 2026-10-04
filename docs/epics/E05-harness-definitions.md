# E05 — Harness definitions

> **GitHub issue**: [#5](https://github.com/jangraefen/agenty/issues/5) — status, progress, and stories are tracked there
> **Milestone**: [M2](../ROADMAP.md#m2--first-governed-run)
> **Depends on**: [E04](E04-identity-workspaces.md)

## Goal

Harnesses can be defined visually or as YAML, versioned immutably, and moved through their lifecycle.

## Capabilities covered

- Visual harness builder
- Declarative harness definition
- Immutable versioning and rollback
- Lifecycle states
- Entry point and parameter definitions
- Output contract definitions

## In scope

- JSON Schema in `schemas/` covering all harness sections (entry points, instructions, model, tools, skills, knowledge, policy, approvals, output, audience).
- YAML import and export with clear validation errors.
- Immutable versions, active-version pointer, rollback.
- Lifecycle states (draft, active, deprecated, retired) and owner field.
- Agent principal per harness with grants.
- Visual builder UI mapping one-to-one to the schema; sections for tools, skills, knowledge, and policy gain pickers as their epics land.

## Out of scope

- Git-managed harnesses (Later).
- Running harnesses (E06).

## References

- VISION §6, principle 5; ARCHITECTURE §13.2

## Acceptance criteria

The [common acceptance criteria](README.md#common-acceptance-criteria) apply in addition to:

- **AC-E05-1** — The JSON Schema in `schemas/` validates every harness section; invalid definitions are rejected with the path of each error. *Verified by:* Unit tests with valid and invalid fixtures.
- **AC-E05-2** — Importing a YAML definition and exporting it again is lossless. *Verified by:* Round-trip tests over fixtures.
- **AC-E05-3** — Every save creates an immutable version; attempts to modify a version are rejected. *Verified by:* Module tests.
- **AC-E05-4** — Activating a version and rolling back change only the active pointer; all versions remain available. *Verified by:* Module tests.
- **AC-E05-5** — Lifecycle transitions follow a defined table; disallowed transitions are rejected; a retired harness cannot be activated. *Verified by:* Unit tests.
- **AC-E05-6** — Each harness has an agent principal whose grants can be changed only by authorized roles. *Verified by:* Module tests.
- **AC-E05-7** — A complete harness can be created in the visual builder, and its exported YAML matches the expected fixture. *Verified by:* Playwright.
- **AC-E05-8** — The example in ARCHITECTURE §13.2 is updated to the final schema and imports successfully as a test fixture. *Verified by:* Unit test; documentation updated.

## Stories

Stories are tracked as sub-issues of [#5](https://github.com/jangraefen/agenty/issues/5).
