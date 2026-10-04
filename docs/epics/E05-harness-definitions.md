# E05 — Harness definitions

> **Status**: Proposed
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

## Definition of done

In addition to the [common definition of done](README.md#common-definition-of-done):

- YAML round-trip tests (import → store → export is lossless).
- Schema validation tests, including invalid definitions.
- UI tests for the builder.

## Stories

_To be defined._
