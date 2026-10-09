# Agenty

Self-hosted, multi-tenant AI agent builder with chat. Tenants (Better Auth organizations)
configure agents (model, system prompt, tools) and chat with them. Every tool call is checked by
an OPA policy; agent runs are durable (Workflow SDK, Postgres world).

Runtime components: the Next.js app (standalone Node server) and Postgres. Nothing else.

## Commands

All commands go through `Taskfile.yml` (go-task), identically locally and in CI.

| Command | Does |
|---|---|
| `task setup` | Check tools, `pnpm install`, Playwright Chromium, `.env` from `.env.example` |
| `task tools:check` | Check node, pnpm, docker and opa are installed; warn if the opa version differs from `OPA_VERSION` |
| `task playwright:install` | Install Playwright's Chromium (with OS dependencies when `CI=true`) |
| `task dev` | Postgres up, migrations, policy build, `next dev` |
| `task db:up` / `db:down` / `db:reset` | Local Postgres (compose); `db:reset` deletes data (needed after editing `docker/postgres/init.sql`) |
| `task db:check` | Fail fast if `DATABASE_URL` is unreachable (prints the error code, never the URL) |
| `task db:generate` | New migration from `src/server/db/schema.ts`; `-- --custom --name x` for hand-written SQL |
| `task db:migrate` | Apply migrations as `agenty_owner` (`scripts/migrate.mjs`) |
| `task db:studio` | Drizzle Studio |
| `task policy:test` / `policy:build` | `opa test`; compile `policies/` to `build/policy/policy.wasm` |
| `task lint` / `task format` | Biome check; apply Biome fixes incl. Tailwind class sorting |
| `task typecheck` | `next typegen` + `tsc --noEmit` |
| `task test` | Vitest: `unit` (`*.test.ts`) and `integration` (`*.int.test.ts`, needs Postgres) |
| `task test:e2e` | Playwright against the standalone build on port 3100 |
| `task build` | `scripts/build.sh`: self-contained `.next/standalone` |
| `task docker:build` | Production image `agenty:local` |
| `task ci` | Everything the `ci` job checks (the `docker` job additionally builds the image, migrates from it and smoke-tests it); locally run `task db:up` first |

Run tests through `task` (it loads `.env`); plain `pnpm exec vitest` lacks `DATABASE_URL`.

## Architecture

```
src/app/            routes, pages, route handlers (thin: parse, call src/server, respond)
src/components/ui/  shadcn/ui components (Base UI preset `base-nova`)
src/lib/            shared client/server utilities (utils.ts re-exports `cn` from the `cn` package)
src/instrumentation.ts  validates env at server start
src/server/         server-only code (every module imports "server-only")
  env.ts            lazily Zod-parsed configuration (getEnv)
  db/               Drizzle schema, client (getDb), migrations
  health.ts         health check
  policy/           OPA WASM loading and evaluation
policies/           Rego sources and tests
scripts/            build, policy build, migration, DB check (shared by Taskfile and Dockerfile)
docker/postgres/    init.sql: database roles
tests/e2e/          Playwright specs
docs/specs, docs/plans  per-milestone design and implementation plan
```

Folders `src/server/{auth,agents,tools,workflows,crypto}` are created by the milestones that need them.

### Database

- Two roles, created by `docker/postgres/init.sql` (compose runs it on an empty volume; CI via psql):
  - `agenty_owner`: owns schema `app` and all its tables, runs migrations (`DATABASE_MIGRATION_URL`).
  - `agenty_app`: runtime role (`DATABASE_URL`); no superuser, no `BYPASSRLS`, owns nothing, cannot
    create objects. Gets table/sequence access through default privileges. RLS therefore always
    applies to it.
- Schema `app` and grants come from migrations (0000, 0001), not from init.sql. Prefer migrations
  for future role/grant changes; init.sql only runs on an empty data directory.
- Drizzle's bookkeeping lives in schema `drizzle` (owner only).
- Reserved for the workflow world (M3), names hardcoded by `@workflow/world-postgres`: `workflow`,
  `workflow_drizzle`, `graphile_worker`. Its bootstrap falls back to `DATABASE_URL`, i.e. the app
  role, so M3 must set `WORKFLOW_POSTGRES_URL` and design roles/grants for it.
- Better Auth tables (M1: user, session, account, verification, organization, member, invitation)
  will live in `app` but are read before a tenant context exists and across organizations; M1 must
  decide their RLS treatment explicitly.
- Integration tests run against the dev database for now; M1's isolation tests may need a
  separate test database.

### Policy

`policies/agenty/main.rego` (package `agenty.authz`) defines `decision`:
`{ allow, reason, require_approval }`, default deny. Built to WASM with entrypoint
`agenty/authz/decision`, traced into the standalone output via `outputFileTracingIncludes`, and
evaluated in-process by `evaluatePolicy()`, which returns a `PolicyOutcome`:
`{ kind: "allow" | "require_approval" | "deny", reason }`. Callers `switch` over `kind` and handle
every case. Any result other than exactly one well-formed decision throws (fail closed), and so does
the contradictory `allow: false, require_approval: true`; callers treat a thrown error as a denial.
A Rego test asserts the policy never produces that contradiction.

### Build

`scripts/build.sh` (used by `task build` and the Dockerfile) builds the policy, runs `next build`,
copies static assets into `.next/standalone`, and bundles `scripts/migrate.mjs` with esbuild
(file tracing does not include drizzle's migrator) next to the SQL migrations. The runtime image
only contains `.next/standalone`; run migrations with `node scripts/migrate.mjs`.

## Non-negotiable rules

1. **Tenant isolation:** every domain table has `tenant_id` and an RLS policy. The tenant is set
   per transaction with `set_config('app.tenant_id', …, true)` and always comes from the session,
   never from the request body. Cross-tenant access is ruled out by explicit tests.
2. **Policy before every tool call:** tools are never registered directly; always
   `withPolicy(tool, ctx)`. Every decision is logged to `policy_decisions`. Creating/modifying
   tools is policy-checked too.
3. **Idempotent steps:** policy check and tool execution run in one workflow step; every tool call
   carries the idempotency key `runId:toolCallId` to external calls.
4. **Secrets:** provider keys and tool credentials are stored per tenant, AES-256-GCM encrypted
   (master key from env). Secrets never appear in prompts, logs, error messages or API responses.
5. **Untrusted input:** tool descriptions and results are untrusted (prompt injection). HTTP tools
   have SSRF protection (no private/internal IPs, per-tenant domain allowlist), timeouts and
   response size limits.

## Conventions

- TypeScript strict + `noUncheckedIndexedAccess`; Biome for lint/format (no ESLint/Prettier).
- Zod at every boundary: env, request bodies, route responses, policy results, external data.
- Configuration is read only via `getEnv()` (lazy, so `next build` needs no runtime env).
  `src/instrumentation.ts` validates it at server start and calls `process.exit(1)` when invalid,
  because Next only logs `register()` failures. This also stops `next dev`.
- Route handlers that touch the database call `await connection()` first (Cache Components is on;
  without it Next may try to prerender them at build time).
- Error bodies never contain internal details; log them server-side. Never echo connection strings
  (`scripts/check-db.mjs` prints only the error code, e.g. `ECONNREFUSED`).
- Shell scripts stay POSIX `sh` and must work with BSD/macOS tools (e.g. `scripts/build-policy.sh`
  handles both GNU tar and bsdtar).
- pnpm `allowBuilds` decisions for dependency build scripts live in `pnpm-workspace.yaml`.
- Tests are named after the guarantee they protect. Unit: `*.test.ts`; integration: `*.int.test.ts`.
  Vitest config is `vitest.config.mts` (native `resolve.tsconfigPaths`). Playwright queries use
  `exact: true` where a role/name could match more than one element.
- English only, no i18n.
- One branch and PR per milestone; `task ci` must pass locally before committing.
- Before using a library API, check its current docs (context7 / official site).

## Version notes

- Node 24 LTS (`.nvmrc`).
- TypeScript 7: Next 16.4 type-checks with the project-local `tsc` CLI
  (`experimental.useTypeScriptCli`, on by default). Don't turn it off; TS 7 has no JS compiler API.
- opa v1.21.1: installed by the developer (`brew install opa`); there is no download task.
  `OPA_VERSION` in `Taskfile.yml` is checked by `task tools:check`, passed to the Docker build
  (which downloads the binary with a checksum check), and mirrored in `.github/workflows/ci.yml`
  (`open-policy-agent/setup-opa`).
- shadcn/ui uses the Base UI preset (`base-nova`); `buttonVariants` is exported from
  `src/components/ui/button.tsx`.
