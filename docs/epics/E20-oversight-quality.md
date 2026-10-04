# E20 — Oversight & quality

> **Status**: Proposed
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

## Definition of done

In addition to the [common definition of done](README.md#common-definition-of-done):

- UI tests for inventory, reassignment, and replay comparison.

## Notes

- Open design question: replaying examples must not cause real side effects (e.g., closing tickets again). Options to decide when stories are written: force approval or deny for write effects during replay, or replay with recorded tool results.

## Stories

_To be defined._
