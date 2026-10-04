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

## Definition of done

In addition to the [common definition of done](README.md#common-definition-of-done):

- `task check` passes on the skeleton.
- Each gate demonstrably fails when violated (coverage below threshold, forbidden import, disallowed license).
- Hooks install via `task setup`.

## Stories

_To be defined._
