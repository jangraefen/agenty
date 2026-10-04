# E02 — Server core

> **Status**: Proposed
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
- HTTP server skeleton: health and readiness endpoints, RFC 9457 errors, request logging, graceful shutdown.

## Out of scope

- Identity and any domain logic.
- Tracing (E19).

## References

- ARCHITECTURE D4, D5, D18, D19, §5, §6.4, §11.1, §12.2, §13.1

## Definition of done

In addition to the [common definition of done](README.md#common-definition-of-done):

- Module tests against real PostgreSQL for migrations, notifier (including lost-notification fallback), and blob backends.
- Invariant 13 (workspace isolation) helper covered by tests.

## Stories

_To be defined._
