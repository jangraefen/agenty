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

## Acceptance criteria

The [common acceptance criteria](README.md#common-acceptance-criteria) apply in addition to:

- [ ] **AC-E03-1** — `web/` builds static assets with Vite; `task check` runs Biome, the TypeScript type check, and Vitest. *Verified by:* `task check`.
- [ ] **AC-E03-2** — The same build works against different API base URLs by changing only `config.json`. *Verified by:* Playwright test with two configurations.
- [ ] **AC-E03-3** — `agenty` serves the embedded UI with client-side routing fallback; serving can be disabled by configuration. *Verified by:* Module test and Playwright.
- [ ] **AC-E03-4** — The build served by a separate static server on another origin talks to the API when that origin is configured. *Verified by:* Playwright.
- [ ] **AC-E03-5** — The shell provides layout, navigation, error boundary, loading states, and a not-found page; axe-core reports no violations on shell pages. *Verified by:* Playwright with axe-core.
- [ ] **AC-E03-6** — UI code accesses the API only through the generated client. *Verified by:* Biome or lint rule, covered by `task test:gates`.
- [ ] **AC-E03-7** — Stryker runs with a configured threshold; an npm license checker is selected, recorded in ARCHITECTURE §16, and fails on disallowed licenses. *Verified by:* `task test:gates`.

## Stories

_To be defined._
