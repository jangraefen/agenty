# AGENTS.md

Guidance for AI coding agents working in this repository.

## Project

Agenty is an open-source, self-hostable platform for building governed AI agent harnesses. The proof of concept, the API server, the web frontend and durable runs are done and the next stage is the web portal: [docs/IDEA.md](docs/IDEA.md) describes the idea, the settled technology choices, the PoC, and the decisions for the next stages. Read it before working.

## Non-negotiables

- **Scope**: build the stage IDEA.md describes as next. Add a component only when that stage needs it.
- **Language**: Go for the backend; the web frontend alone is TypeScript (React, TanStack), in its own deployable. No agent framework: Agenty owns its agent loop on the official model provider SDKs.
- **Server stack**: gin for the API, spec-first: `schema/openapi.yaml` defines it, and oapi-codegen generates the API types into `internal/api` and the gin server interface into `internal/server`. Change the spec first, never the generated code. PostgreSQL only through `internal/store`, with sqlc-generated queries on pgx and goose migrations. After changing the spec, a query or a migration, run `task generate` and commit the result. Tests that need PostgreSQL use `storetest`, which reads `AGENTY_TEST_DATABASE_URL`.
- **Web frontend**: in `web/`, managed with pnpm: React, TanStack Router, Query and Form, Tailwind with shadcn/ui, API calls only through the client typed from the spec, whose types `task generate` regenerates too. Biome and `tsc` must pass with no warnings; never render model or tool output as HTML (`dangerouslySetInnerHTML` is a lint error), and never log or put the token in a URL. Unit tests use Vitest and Testing Library, query by role and label, and mock the API with MSW from responses shaped by the spec. The Playwright smoke test (`task web:e2e`, its own CI job) drives the built frontend in Google Chrome against a real `agenty serve` on a database of its own; its runs fail at once, as its model provider is a closed local port.
- **Policy**: OPA, embedded, is the only policy engine.
- **Trust model**: every change preserves the guarantees in IDEA.md, above all: every side effect goes through the Tool Gateway, ungranted tools are denied, and credentials never reach model context or logs.
- **Tests first**: write the failing test before the implementation. Each trust-model guarantee has a named test; extend it when touching security-relevant code. The gateway, policy and secret redaction code get the most thorough tests.
- **Go tests**: use testify; `require` for preconditions where the test cannot continue, `assert` for independent checks; prefer table-driven tests. Use the scripted model (`internal/model/modeltest`), never a real one, in gating tests.
- **Local checks**: `task check` runs what CI runs (generated code, lint, `go mod tidy`, tests) against a Docker Compose database of its own, on a free port, removed afterwards; `task --list` shows the rest.
- **Lint**: `golangci-lint run ./...` must pass. Its `forbidigo` rules encode trust-model guarantees; never exclude or `//nolint` them. Every `//nolint` names its linter and says why.
- **Errors**: never discard one, not even with `_`. Return it, or panic if it truly cannot happen; the linter enforces this.
- **Logging**: use the `log/slog` API (handler: `charmbracelet/log`).
- **Dependencies**: keep them few; every new dependency must be open source and free to self-host.

## Git

- Never commit directly to `main`. Work on a branch named `<type>/<slug>` and open a pull request.
- PR titles use conventional-commit format (`feat:`, `fix:`, `docs:`, `test:`, `refactor:`, `chore:`); pull requests are squash-merged.
- Do not merge, push, or rewrite history unless asked.
- The repository is public but not yet licensed; do not add a license or accept external contributions without the maintainer's decision.
- Never put secrets in the repository or logs.
- Local credentials live in a git-ignored `.env` at the module root, loaded with `internal/dotenv`. Agents must never read, print or commit it.

## Keeping documents current

When a task changes a technology choice, a trust-model guarantee, or the scope of the current stage, update IDEA.md in the same change.
