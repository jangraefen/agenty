# E01 — Project foundation

> **Status**: Proposed
> **Milestone**: [M1](../ROADMAP.md#m1--foundations)
> **Depends on**: None

## Goal

A repository in which every later epic can add code with tests, quality gates, and local tooling already working.

## Capabilities covered

- Enabler for all capabilities

## In scope

- Monorepo skeleton per ARCHITECTURE §4: `go.work`, `server/` and `sandbox/` Go modules with minimal binaries, `api/`, `schemas/`, `web/` placeholder, `deploy/`.
- `Taskfile.yml` with all targets from ARCHITECTURE §15.4, wired to what exists, plus `task setup` for local prerequisites.
- Lefthook hooks: pre-commit runs lint, architecture rules, unit tests; pre-push runs module tests.
- Adopt the existing `.editorconfig`, `.golangci.yaml`, and `biome.json`, and align them: point Biome's `files.includes` at `web/`; enable `errcheck`, `govet`, `ineffassign`, and `unused` in golangci-lint (with `default: none` they are currently off, so the `govet` settings have no effect); add `depguard` rules for architecture boundaries.
- Coverage gate tooling with per-package thresholds; Gremlins mutation testing wired for Go; `task check:licenses` with `go-licenses` and an allowlist.
- testcontainers-go helper for PostgreSQL with pgvector; Docker Compose skeleton for integration tests.
- Dockerfiles for `agenty` and `agenty-sandbox` (multi-stage builds).
- Guide for running sandbox tests on macOS in a Colima or Lima VM.

## Out of scope

- Product features.
- Web tooling beyond Biome (Vitest, Playwright, Stryker): E03.

## References

- ARCHITECTURE §4, §15, §16, §17

## Acceptance criteria

The [common acceptance criteria](README.md#common-acceptance-criteria) apply in addition to:

- [ ] **AC-E01-1** — On a clean checkout, `task setup` installs all tooling prerequisites and the Lefthook git hooks on macOS and Linux. *Verified by:* Following the setup procedure on a clean checkout.
- [ ] **AC-E01-2** — `task check` runs every tier target from ARCHITECTURE §15.4 and passes on the skeleton; tiers without tests report that explicitly instead of failing. *Verified by:* `task check`.
- [ ] **AC-E01-3** — golangci-lint runs on all Go modules with the configured linters plus `errcheck`, `govet`, `ineffassign`, and `unused`; Biome runs on `web/`. *Verified by:* `task test:gates`.
- [ ] **AC-E01-4** — `depguard` architecture rules are configured; an import that violates them (e.g., an executor package imported outside `toolgateway`) fails the lint step. *Verified by:* `task test:gates`.
- [ ] **AC-E01-5** — Coverage gates are configured per package (≥ 90 % statements by default, 100 % statements for `toolgateway`, `policy`, `credentials`, `identity`, `runs`); a package below its threshold fails `task check`. *Verified by:* `task test:gates`.
- [ ] **AC-E01-6** — Gremlins mutation testing runs on the critical packages with an efficacy threshold (initially 80 %); falling below it fails `task test:mutation`. *Verified by:* `task test:gates`.
- [ ] **AC-E01-7** — `task check:licenses` runs `go-licenses` against an allowlist; a module with a disallowed license fails the check. *Verified by:* `task test:gates`.
- [ ] **AC-E01-8** — `task test:gates` exists and proves each gate above fails when violated, using temporary fixtures that do not touch the real tree. *Verified by:* `task test:gates`.
- [ ] **AC-E01-9** — A testcontainers-go helper starts an isolated PostgreSQL with pgvector per test package; a Compose file starts the same for local use. *Verified by:* Module test of the helper.
- [ ] **AC-E01-10** — `task build:images` builds the `agenty` and `agenty-sandbox` images; both binaries respond to `--version`. *Verified by:* `task build:images` and a smoke test.
- [ ] **AC-E01-11** — The macOS guide for Colima or Lima exists, and following it runs a container with `runsc`. *Verified by:* Manual verification, recorded in the guide.

## Stories

_To be defined._
