# E03 — Web foundation

> **Status**: Proposed
> **Milestone**: [M1](../ROADMAP.md#m1--foundations)
> **Depends on**: [E01](E01-project-foundation.md), [E02](E02-server-core.md)

## Goal

The single-page application shell that all UI work builds on, hostable embedded or separately.

## Capabilities covered

- Enabler for all UI capabilities

## In scope

- Vite + React + TypeScript app in `web/` with TanStack Router and TanStack Query on the generated API client.
- Runtime `config.json` for the API base URL.
- shadcn/ui and Tailwind setup; application layout, navigation, error and loading patterns; auth-aware routing hooks (stubbed until E04).
- Embedding of the production build into `agenty` via `go:embed`, with a setting to disable serving the UI.
- Biome for linting and formatting; Vitest and Testing Library; Playwright with axe-core against the Compose stack; Stryker for mutation testing.
- Select, verify, and record an npm license checker; add it to `task check:licenses`.

## Out of scope

- Feature screens (delivered by the capability epics).

## References

- ARCHITECTURE D17, §13.3, §15, §16

## Definition of done

In addition to the [common definition of done](README.md#common-definition-of-done):

- Playwright smoke test passes both against the embedded UI and against a separately served build configured via `config.json`.

## Stories

_To be defined._
