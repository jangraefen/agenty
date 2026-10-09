# M0 – Scaffolding: design

Status: draft · Date: 2026-10-09 · Branch: `chore/m0-scaffolding`

## Goal

An empty but complete skeleton of Agenty, a self-hosted multi-tenant AI agent
builder. Every tool the later milestones rely on is installed, wired up and
exercised by at least one check, so M1–M5 add features, not plumbing.

**Done when:** `task ci` passes locally and in GitHub Actions, the home page
renders, the PR has been reviewed by a reviewer subagent, and the maintainer
has approved the merge.

## Non-goals

No auth, tenants, RLS policies, domain tables, LLM calls, workflows or tool
execution. Those belong to M1–M5. M0 only prepares the ground they stand on
(database roles, policy toolchain, test harnesses).

## Decisions

| Topic | Decision |
|---|---|
| Runtime | Node 24 LTS, pinned in `.nvmrc`, `package.json#engines`, Dockerfile and CI. pnpm pinned via `packageManager`. |
| Framework | Next.js 16 (App Router), React 19, TypeScript strict plus `noUncheckedIndexedAccess`, `output: "standalone"`, long-lived Node server. |
| UI | Tailwind CSS 4, shadcn/ui (`components.json`, `src/components/ui`). TanStack Query is installed with a `QueryClientProvider` in the root layout, though nothing queries yet. English only, no i18n. |
| Lint/format | Biome only: linter, formatter, import sorting, Tailwind class sorting (`useSortedClasses`) and Tailwind CSS directives. No ESLint, no Prettier. `task lint` runs `biome ci`. |
| Validation | Zod 4 at every boundary. Environment variables are parsed once by a Zod schema in `src/server/env.ts`; the app fails fast at startup on invalid configuration. |
| Database | Postgres (pinned major) via `docker-compose.yml`. Drizzle ORM with the `postgres` (postgres.js) driver; drizzle-kit migrations in `src/server/db/migrations`. |
| Policy | Rego sources in `policies/`, `opa test` for Rego tests, `opa build -t wasm` to `policies/build/policy.wasm` (git-ignored), evaluated in-process with `@open-policy-agent/opa-wasm`. |
| Tests | Vitest with two projects, `unit` (no I/O) and `integration` (real Postgres). Playwright E2E against the production standalone build. |
| Tooling install | `task setup` downloads a pinned `opa` binary for the current OS/arch into `.tools/bin` (git-ignored); Taskfile puts it on `PATH`. |

Exact library versions are taken from npm at implementation time (current
stable), and APIs are checked against the official docs.

## Database roles and schemas

Tenant isolation in M1 depends on RLS, and RLS is silently bypassed by
superusers, table owners and roles with `BYPASSRLS`. M0 therefore separates
the roles from the start:

- `agenty_owner` — owns schema `app` and all its tables; runs migrations
  (`DATABASE_MIGRATION_URL`). Not a superuser.
- `agenty_app` — the runtime role (`DATABASE_URL`). `NOSUPERUSER NOBYPASSRLS`,
  owns nothing, has `USAGE` on schema `app` and receives table/sequence
  privileges through `ALTER DEFAULT PRIVILEGES FOR ROLE agenty_owner`.

An idempotent SQL init script (`docker/postgres/init.sql`) creates both roles
and the `app` schema. docker-compose mounts it into
`/docker-entrypoint-initdb.d`; CI runs the same script with `psql` against its
Postgres service container, so both environments are set up identically.
Passwords in compose and CI are local-development values, not secrets.

Both roles set `search_path = app`. drizzle-kit is configured with
`schemaFilter: ["app"]`, and its migrations bookkeeping table lives in `app`
too (`migrations.schema = "app"`).

**Workflow schema:** `@workflow/world-postgres` hardcodes its tables in a
schema named `workflow` and creates them with its own bootstrap. Creating
that schema, its role and grants is deferred to M3, where the world is
actually wired in; M0 only reserves the name and documents it in
`CLAUDE.md`. This replaces the earlier idea of creating an empty `workflow`
schema in M0, which would add grants we cannot yet test.

M0 adds no domain tables, so the first migration is empty apart from what
drizzle-kit needs. A check that the app role really cannot bypass RLS is an
M1 deliverable, but M0's integration test asserts the role attributes
(`rolsuper = false`, `rolbypassrls = false`) of the role behind
`DATABASE_URL`.

## Application skeleton

```
src/
  app/
    layout.tsx          # html shell, fonts, Providers
    page.tsx            # home page (shadcn Card + Button)
    providers.tsx       # "use client"; QueryClientProvider
    api/health/route.ts # GET → { status: "ok", db: "ok" } or 503
  components/ui/        # shadcn components
  server/
    env.ts              # Zod-parsed process.env (server-only)
    db/
      client.ts         # drizzle(postgres(DATABASE_URL)), lazy singleton
      schema.ts         # empty for now, exports pgSchema("app")
      migrations/
    policy/
      evaluate.ts       # load policy.wasm, evaluate(input) → Zod-parsed result
  lib/
    utils.ts            # shadcn cn()
policies/
  agenty/main.rego      # package agenty.authz; default decision = deny
  agenty/main_test.rego
docker/postgres/init.sql
tests/e2e/              # Playwright specs
```

The empty `auth/`, `agents/`, `tools/`, `workflows/` and `crypto/`
directories from the suggested layout are created by the milestones that fill
them, not as empty placeholders.

`/api/health` runs `select 1` through the app role and returns a body
validated by a Zod schema. It is the only route handler in M0 and
demonstrates the route handler conventions: Zod response schema,
`server-only` imports, no secrets in the error body (a DB failure returns
`{ status: "error", db: "unavailable" }` with HTTP 503 and logs the cause
server-side).

## Policy toolchain

`policies/agenty/main.rego` defines `decision` with the shape agreed for M4,
`{ allow, reason, require_approval }`, and denies everything by default
(`allow: false, reason: "no policy matches", require_approval: false`). The
entrypoint is `agenty/authz/decision`.

`src/server/policy/evaluate.ts` loads `policies/build/policy.wasm` once,
evaluates an input and parses the output with a Zod schema. It is not wired to
any tool yet; a unit test asserts that the default-deny decision comes back.
`task build` depends on `policy:build`, and the wasm file is copied into the
standalone output and the Docker image.

## Tests

- **Unit (Vitest, `unit` project):** `cn()`, env parsing (valid/invalid),
  policy evaluation of the built wasm.
- **Integration (Vitest, `integration` project):** connects as the app role,
  runs `select 1`, asserts the role is neither superuser nor `BYPASSRLS`, and
  calls the health route handler directly.
- **E2E (Playwright):** builds nothing itself; `task test:e2e` depends on
  `build` and starts `node .next/standalone/server.js` through Playwright's
  `webServer`. Checks that the home page renders its heading and that
  `/api/health` returns `200 { status: "ok" }`. Chromium only.

## Taskfile

All commands run through `Taskfile.yml`, identically locally and in CI.
`dotenv: [".env"]` loads local configuration.

| Task | Does |
|---|---|
| `setup` | Check Node/pnpm/Docker, `pnpm install --frozen-lockfile`, download pinned `opa` to `.tools/bin`, install Playwright's Chromium (skipped where a browser is preinstalled), copy `.env.example` to `.env` if missing. |
| `dev` | `db:up`, `db:migrate`, `policy:build`, `next dev`. |
| `db:up` / `db:down` | `docker compose up -d --wait` / `docker compose down`. |
| `db:generate` | `drizzle-kit generate`. |
| `db:migrate` | `drizzle-kit migrate` as `agenty_owner`. |
| `db:studio` | `drizzle-kit studio`. |
| `policy:build` / `policy:test` | `opa build -t wasm -e agenty/authz/decision` / `opa test policies -v`. |
| `lint` | `biome ci .` |
| `typecheck` | `tsc --noEmit` (after `next typegen` for route types). |
| `test` | `vitest run` (both projects). |
| `test:e2e` | `playwright test` against the standalone build. |
| `build` | `policy:build`, then `next build`, then copy `public/`, `.next/static` and the wasm into the standalone output. |
| `ci` | `policy:test → lint → typecheck → db:migrate → test → build → test:e2e`. Fails early with a hint ("run `task db:up`") when `DATABASE_URL` is unreachable. |

## Infrastructure

- **`docker-compose.yml`:** Postgres only, port configurable via
  `POSTGRES_PORT` (default 5432), named volume, healthcheck, init script.
- **`Dockerfile`:** multi-stage on `node:24-*-slim`: deps (pnpm via
  corepack) → build (needs `opa` for `policy:build`, so the build stage
  downloads the same pinned version) → runtime with only the standalone
  output, non-root user, `HOSTNAME=0.0.0.0`, `PORT=3000`. Migrations are not
  run on container start; they are an explicit operator step for now.
- **`.env.example`:** `DATABASE_URL`, `DATABASE_MIGRATION_URL`,
  `POSTGRES_PORT`, and the later-needed `BETTER_AUTH_SECRET`,
  `BETTER_AUTH_URL` and `ENCRYPTION_MASTER_KEY` (documented, unused in M0,
  optional in the M0 env schema).
- **GitHub Actions (`.github/workflows/ci.yml`):** on push and pull request;
  checkout, Node 24, pnpm, go-task (`arduino/setup-task`), cache pnpm store
  and `.tools`; Postgres service container; run `docker/postgres/init.sql`;
  `task setup`; `task ci`. Uploads the Playwright report on failure.

## CLAUDE.md

Describes the architecture (layers, folder layout, the two DB roles,
`workflow` schema reserved for M3), the non-negotiable rules from the brief
(tenant only from the session, RLS per transaction, `withPolicy` around every
tool, idempotency keys, encrypted secrets that never reach prompts/logs/
responses, tool descriptions and results are untrusted), conventions (Biome,
Zod at boundaries, `server-only`, tests per guarantee, English) and the key
`task` commands. It is updated by every milestone.

## Risks and open points

- **Vitest 5 / Next 16 / Tailwind 4 / Biome 2 compatibility:** verified while
  scaffolding; any incompatibility is resolved by following the official docs
  rather than pinning old versions, and noted in `CLAUDE.md` if a workaround
  is needed.
- **`@open-policy-agent/opa-wasm`** was last published in 2024; it is the
  mandated library and the WASM ABI is stable, but M0's unit test is what
  proves it works with the current `opa` release.
- **Standalone output and the wasm file:** Next's file tracing does not see a
  file read at runtime by path, so the build task copies it explicitly; the
  E2E run against the standalone build is what guards this. The health route
  does not touch policy, so a dedicated assertion that the wasm loads inside
  the standalone server is added in M4 when the first route uses it.
