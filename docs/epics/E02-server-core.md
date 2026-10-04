# E02 — Server core

> **GitHub issue**: [#2](https://github.com/jangraefen/agenty/issues/2) — status, progress, and stories are tracked there
> **Milestone**: [M1](../ROADMAP.md#m1--foundations)
> **Depends on**: [E01](E01-project-foundation.md)

## Goal

The shared runtime of the `agenty` server that every domain package builds on.

## Capabilities covered

- Enabler for all server capabilities

## In scope

- Configuration from file and environment with validation; `--roles` selection (api, worker, scheduler).
- Logging via `log/slog` with `charmbracelet/log` as handler (text in development, JSON or logfmt in production) and a redacting wrapper handler.
- PostgreSQL access: `pgx` pool, `goose` migrations at startup under an advisory lock, `sqlc` setup, repository conventions that require `workspace_id` for workspace-scoped queries.
- `Notifier` interface with Postgres `LISTEN/NOTIFY` and in-process implementations, plus the polling-fallback helper.
- `Blob` interface with local-volume and S3-compatible implementations.
- Minimal append-only audit writer (table and `audit` interface) so later epics can emit events; hardening follows in E18.
- OpenAPI 3.1 skeleton in `api/` with `oapi-codegen` (Go) and `openapi-typescript`/`openapi-fetch` (TypeScript) generation tasks.
- HTTP server skeleton on Gin: health and readiness endpoints, RFC 9457 errors, request logging, graceful shutdown.

## Out of scope

- Identity and any domain logic.
- Tracing (E19).

## References

- ARCHITECTURE D4, D5, D18, D19, §5, §6.4, §11.1, §12.2, §13.1

## Acceptance criteria

The [common acceptance criteria](README.md#common-acceptance-criteria) apply in addition to:

- **AC-E02-1** — The server starts from a configuration file plus environment overrides; invalid configuration aborts startup with a message naming the offending field. *Verified by:* Unit and module tests.
- **AC-E02-2** — `--roles` starts only the selected roles (any combination of api, worker, scheduler); the health endpoint reports the active roles. *Verified by:* Module tests.
- **AC-E02-3** — Log output is text in development and JSON or logfmt in production; values registered as secrets are redacted in every format. *Verified by:* Unit tests.
- **AC-E02-4** — Migrations run at startup; two instances starting concurrently apply each migration exactly once. *Verified by:* Module test against PostgreSQL.
- **AC-E02-5** — Workspace-scoped repository functions cannot be called without a `workspace_id`, and a query for one workspace never returns another workspace's rows. *Verified by:* Invariant 13 suite.
- **AC-E02-6** — A notification published after commit wakes subscribers on another server instance; with notifications deliberately dropped, waiters still observe the new state within the polling interval. *Verified by:* Module tests.
- **AC-E02-7** — The `Blob` interface passes one shared contract test suite for the local-volume and S3-compatible implementations (S3 tested against a license-checked S3-compatible test server). *Verified by:* Module tests.
- **AC-E02-8** — The minimal audit writer appends events; the application's database role cannot update or delete them. *Verified by:* Module test with role permissions.
- **AC-E02-9** — `task generate` regenerates Go and TypeScript code from `api/`; `task check` fails if generated code is out of date. *Verified by:* `task test:gates`.
- **AC-E02-10** — `/healthz` and `/readyz` exist; readiness requires database connectivity and applied migrations; errors use RFC 9457 problem details; shutdown completes in-flight requests. *Verified by:* Module tests.

## Stories

Stories are tracked as sub-issues of [#2](https://github.com/jangraefen/agenty/issues/2).
