# AGENTS.md

Guidance for AI coding agents working in this repository.

## Project

Agenty is a fully open-source, self-hostable enterprise platform for building governed AI agent harnesses. The repository currently contains the product and architecture documents only; no code has been written yet.

## Read before working

1. [docs/VISION.md](docs/VISION.md) — why Agenty exists, principles (ranked), core concepts, trust model, non-goals.
2. [docs/CAPABILITIES.md](docs/CAPABILITIES.md) — what ships in v1 versus Later / Explore.
3. [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — how it is built: decision record (§2), invariants (§3), layout, components, testing, dependencies, conventions (§17).

Planned work is organized as epics in [docs/epics/](docs/epics/README.md).

These documents are the source of truth. Do not reopen decisions recorded in ARCHITECTURE.md §2 without recording a new decision there.

## Non-negotiables

- **Scope**: build only v1 capabilities unless the task explicitly says otherwise.
- **Languages**: Go (`server/`, `sandbox/`) and TypeScript (`web/`). No Python in platform code; Python runs only inside sandboxes.
- **Invariants**: every change preserves ARCHITECTURE.md §3 — above all, every side effect goes through `toolgateway`, ungranted tools are denied, and credentials never reach model context, logs, or skill scripts.
- **Contracts first**: API and protocol changes start in `api/` (OpenAPI, protobuf); regenerate, never hand-edit generated code.
- **Tests first**: write the failing test before the implementation. Security-relevant changes extend the matching invariant suites. Coverage and mutation gates are defined in ARCHITECTURE.md §15.3.
- **Dependencies**: check every new dependency against the dependency rule and record it in ARCHITECTURE.md §16 before use.
- **No logic in the database**: no triggers or stored procedures.
- **Logging**: use the `log/slog` API (handler: `charmbracelet/log`).

## Commands

All entry points live in `Taskfile.yml` (to be created with the first code). Planned targets: `test:unit`, `test:module`, `test:integration`, `test:e2e`, `test:ui`, `test:mutation`, `check:licenses`, and `check`. `task check` must pass before merging to `main`. There is no CI; everything runs locally.

## Git

- Conventional commit prefixes (`feat:`, `fix:`, `docs:`, `test:`, `refactor:`, `chore:`).
- Commit author email is the GitHub no-reply address configured in the repository; do not change git identity settings.
- Do not push or rewrite history unless asked.

## Keeping documents current

When a task changes a decision, a capability's phase, or an invariant, update VISION.md, CAPABILITIES.md, or ARCHITECTURE.md in the same change.
