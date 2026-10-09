# Agenty

This is the only agent instruction file in the repo; `CLAUDE.md` just imports it (`@AGENTS.md`)
for Claude Code. Keep project knowledge here. The block between the `nextjs-agent-rules` markers
is managed by `next dev` (it re-adds it when missing): leave it in place and don't edit it.

<!-- BEGIN:nextjs-agent-rules -->

## This is NOT the Next.js you know

This version has breaking changes — APIs, conventions, and file structure may all differ from your training data. Read the relevant guide in `node_modules/next/dist/docs/` (resolved from this file's directory; in monorepos the `next` package may not be visible from the repo root) before writing any code. Heed deprecation notices.

This block is written and re-added by `next dev` — verify at `node_modules/next/dist/server/lib/generate-agent-files.js`. Removing it from a diff only re-creates the uncommitted change; committing it with your work keeps the tree clean.

<!-- END:nextjs-agent-rules -->

Self-hosted, multi-tenant AI agent builder with chat. Tenants configure agents (model, system
prompt, tools) and chat with them. Every tool call is checked by an OPA policy; agent runs are
durable (Workflow SDK, Postgres world).

Sign-in is SSO only (OIDC, Better Auth). The **organization** is the tenant: its slug comes from a
claim in the user's ID token, it is created on the first sign-in that names it and belongs to the
provider that created it. Workspaces (collaboration spaces inside an organization) come later.

Runtime components: the Next.js app (standalone Node server) and Postgres. Nothing else. (Development
and tests also use a mock IdP, `ghcr.io/navikt/mock-oauth2-server`, never in production.)

## Commands

All commands go through `Taskfile.yml` (go-task), identically locally and in CI.

| Command | Does |
|---|---|
| `task setup` | Check tools, `pnpm install`, Playwright Chromium, `.env` from `.env.example` |
| `task tools:check` | Check node, pnpm, docker and opa are installed; warn if the opa version differs from `OPA_VERSION` |
| `task playwright:install` | Install Playwright's Chromium (with OS dependencies when `CI=true`) |
| `task dev` | Postgres and mock IdP up, migrate, seed, policy build, `next dev` |
| `task db:up` / `db:down` / `db:reset` | Local Postgres and the mock IdP on `http://localhost:8080` (compose); `db:reset` deletes data (needed after editing `docker/postgres/init.sql`) |
| `task db:check` | Fail fast if `DATABASE_URL` is unreachable (prints the error code, never the URL) |
| `task db:generate` | New migration from `src/server/db/schema.ts`; `-- --custom --name x` for hand-written SQL |
| `task db:migrate` | Apply migrations without starting the app, for tests and CI (`scripts/migrate.ts`) |
| `task db:seed` | Register the mock IdP's providers `corp` and `partner` (insert-if-absent, never modifies a row) |
| `task auth:generate` | Write Better Auth's reference schema to `build/auth-schema.generated.ts` for diffing after upgrades |
| `task db:studio` | Drizzle Studio |
| `task policy:test` / `policy:build` | `opa test`; compile `policies/` to `build/policy/policy.wasm` |
| `task lint` / `task format` | Biome check; apply Biome fixes incl. Tailwind class sorting |
| `task typecheck` | `next typegen` + `tsc --noEmit` |
| `task test` | Vitest: `unit` (`*.test.ts`) and `integration` (`*.int.test.ts`, needs Postgres) |
| `task test:e2e` | Playwright against the standalone build on port 3100 |
| `task build` | `scripts/build.sh`: self-contained `.next/standalone` |
| `task docker:build` | Production image `agenty:local` |
| `task ci` | Everything the `ci` job checks (incl. migrate and seed) (the `docker` job additionally builds the image and starts it against an empty database, which it migrates); locally run `task db:up` first |

Run tests through `task` (it loads `.env`); plain `pnpm exec vitest` lacks `DATABASE_URL`.

## Architecture

```
src/app/            routes, pages, route handlers (thin: parse, call src/server, respond)
  api/auth/[...all] Better Auth handler; sign-in/ SSO sign-in page; settings/members/ admin-only member list
src/components/ui/  shadcn/ui components (Base UI preset `base-nova`)
src/lib/            shared client/server utilities (utils.ts re-exports `cn` from the `cn` package;
                    auth-client.ts is the Better Auth client)
src/proxy.ts        optimistic redirect to /sign-in without a session cookie (pages still validate)
src/instrumentation.ts  server start: validates env, applies migrations
src/server/         server-only code (every module imports "server-only")
  env.ts            lazily Zod-parsed configuration (getEnv)
  auth/             Better Auth instance (auth.ts, getAuth()), provider row validation and
                    selection (providers.ts), ID-token claim decisions (claims.ts), JIT
                    provisioning (provisioning.ts), getTenantContext (tenant.ts), TenantContext type
                    and constructor (tenant-context.ts), member list (members.ts)
  db/               Drizzle schema (schema.ts, auth-schema.ts, tenant-table.ts), client (getDb),
                    withTenant (tenant.ts), dev seed (seed.ts), migrations
  health.ts         health check
  policy/           OPA WASM loading and evaluation
policies/           Rego sources and tests
scripts/            build, policy build, migration, seed, DB check (shared by Taskfile and Dockerfile)
docker/postgres/    init.sql: database roles
tests/e2e/          Playwright specs (against the mock IdP)
tests/support/sso.ts  drives a real SSO sign-in over HTTP (mock IdP login form + callback) for integration tests
docs/specs, docs/plans  per-milestone design and implementation plan
```

Folders `src/server/{agents,tools,workflows,crypto}` are created by the milestones that need them.

Every server module imports `"server-only"` except the ones plain Node or drizzle-kit load:
`src/server/db/{migrate,schema,auth-schema,tenant-table,seed}.ts`. These use relative imports with
`.ts` extensions (no `@/` alias) so `node scripts/*.ts` and drizzle-kit can load them.

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
- Schema `auth` (owned by `agenty_owner`, same default privileges for `agenty_app`) holds
  Better Auth's `user`, `session`, `account`, `verification`, `sso_provider` and our `organization`
  and `member`. **No RLS** on these: they are read before a tenant is known and across
  organizations. Only `src/server/auth`, `src/server/db`, `scripts` and tests may import
  `auth-schema` (Biome `noRestrictedImports`; the import name `createTenantContext` is restricted
  the same way, `import type { TenantContext }` is allowed everywhere). Accepted risks: session
  tokens and provider client secrets (plain text, see Auth) are readable by `agenty_app`.
  Hand-maintained `auth-schema.ts`: diff it against `task auth:generate` after Better Auth upgrades.
- Domain tables (schema `app`) use `tenantId()` and `tenantIsolation()` from
  `src/server/db/tenant-table.ts`; the catalog guard test fails any table without them.
- Integration tests run against the dev database. Provider-dependent files seed in `beforeAll`
  and never modify `corp`/`partner` (see provider fingerprint); tests needing other providers
  create random ones. E2E users and organizations (per-run unique names) accumulate in the dev
  database; `task db:reset` clears them.

### Policy

`policies/agenty/main.rego` (package `agenty.authz`) defines `decision`:
`{ outcome: "allow" | "require_approval" | "deny", reason }`, default deny. Built to WASM with
entrypoint `agenty/authz/decision`, traced into the standalone output via
`outputFileTracingIncludes`, and evaluated in-process by `evaluatePolicy()`, which returns that
decision as a `PolicyDecision`. Callers `switch` over `outcome` and handle every case. Any result
other than exactly one well-formed decision (e.g. an unknown outcome) throws (fail closed); callers
treat a thrown error as a denial. A Rego test asserts the policy only produces known outcomes.

### Build

`scripts/build.sh` (used by `task build` and the Dockerfile) builds the policy, runs `next build`,
copies static assets and the SQL migrations into `.next/standalone`, and removes any `.env` files
Next copied there (configuration must come from the real environment). The runtime image only
contains `.next/standalone`.

### Migrations on start

`register()` in `src/instrumentation.ts` runs once at server start: it validates the env, takes
`DATABASE_MIGRATION_URL` out of `process.env` (`takeMigrationUrl()`), and applies the migrations as
`agenty_owner` via `migrateDatabase()` (`src/server/db/migrate.ts`), under a Postgres advisory lock
so concurrent instances migrate one after another. The owner connection is closed afterwards; any
failure stops the server. Accepted trade-off: the app receives owner credentials at start, and the
container configuration and initial process environment still hold them. `src/server/db/migrate.ts`
has no `server-only` import because `scripts/migrate.ts` (`task db:migrate`) runs it in plain Node
(type stripping; `package.json` is `"type": "module"`).

## Auth

- **Providers** are rows in `auth.sso_provider`, written by an operator with SQL (README) or by
  `task db:seed`, never through the plugin's registration endpoints (disabled). Our columns:
  `organization_claim`, `role_claim`, `admin_values` (admin when the role claim contains one of
  them, else member). The plugin parses `oidc_config` with plain `JSON.parse` and applies no
  defaults to SQL-written rows, so the sign-in hook validates the selected row strictly (Zod):
  `clientId`, `clientSecret`, `pkce: true`, `scopes` exactly `["openid","email","profile"]`,
  optional `discoveryEndpoint` and the three discovered endpoints; no `userInfoEndpoint`, SAML
  config or plugin organization. An invalid row makes only that provider fail (`idp_unavailable`,
  field names logged, never values). The provider lookup reads all rows ordered by `provider_id`.
- **Discovery is one-off**: the first sign-in through a provider discovers the OIDC endpoints and
  writes `authorizationEndpoint`/`tokenEndpoint`/`jwksEndpoint` into the row. Public IdP hosts are
  allowed; private or loopback hosts only when their origin is in `BETTER_AUTH_TRUSTED_ORIGINS`.
  A DNS check rejects public names that resolve to private addresses before discovery. The plugin
  binds a fingerprint of the provider row into each sign-in, so **never modify a provider row
  while sign-ins may be in flight** (the seed is insert-if-absent for this reason).
- **Endpoints**: a `hooks.before` allowlist permits only `get-session`, `sign-out`, `sign-in/sso`
  and `sso/callback/:providerId`; everything else is 404. On `sign-in/sso` only the body keys
  `email`, `callbackURL`, `errorCallbackURL` are accepted, and both URLs must be relative. The
  provider is chosen by the email's domain (case-insensitive) in our hook.
- **Provisioning**: `resolveUser` (verified ID-token claims, not userinfo) returns `reject` or a
  decision, handed over via a WeakMap to `session.create.before`, which writes organization and
  member through the transaction adapter (same transaction as user, account and session). No
  fallback: any failure rolls the whole sign-in back and redirects to `/sign-in?error=<code>`.
  First sign-ins through the same provider are serialised by the plugin's provider row lock.
- **Organization rules**: slug `^[a-z0-9][a-z0-9-]{1,62}$` from the organization claim; an
  organization owned by another provider is rejected; a user with a membership elsewhere is
  rejected (one organization per user in M1); the email domain must belong to the provider and a
  user bound to another provider is rejected. Better Auth's organization plugin is not used.
- **Sessions**: database sessions, absolute 12 h (`expiresIn: 43_200`), no refresh, cookie cache
  off. Roles and memberships change only at sign-in, so deprovisioning lags up to 12 h.
- **`getTenantContext()`** (`src/server/auth/tenant.ts`) returns the branded `TenantContext`
  (`userId`, `organizationId`, `role`) from the session and a fresh `member` read; the
  organization's provider must still exist. The organization never comes from the request.

## Non-negotiable rules

1. **Tenant isolation:** every domain table has `tenant_id` and an RLS policy: declare it with
   `tenantId()` + `tenantIsolation()` and access it only through
   `withTenant(await getTenantContext(), …)`, which sets `app.tenant_id` per transaction with
   `set_config(…, true)`. The tenant always comes from the session, never from the request.
   The catalog guard test enforces this; cross-tenant access is ruled out by explicit tests.
2. **Policy before every tool call:** tools are never registered directly; always
   `withPolicy(tool, ctx)`. Every decision is logged to `policy_decisions`. Creating/modifying
   tools is policy-checked too.
3. **Idempotent steps:** policy check and tool execution run in one workflow step; every tool call
   carries the idempotency key `runId:toolCallId` to external calls.
4. **Secrets:** provider keys and tool credentials are stored per tenant, AES-256-GCM encrypted
   (master key from env). Secrets never appear in prompts, logs, error messages or API responses.
   Recorded exception (maintainer decision): SSO provider client secrets in `auth.sso_provider`
   are stored in plain text.
5. **Untrusted input:** tool descriptions and results are untrusted (prompt injection). HTTP tools
   have SSRF protection (no private/internal IPs, per-tenant domain allowlist), timeouts and
   response size limits.

## Conventions

- TypeScript strict + `noUncheckedIndexedAccess`; Biome for lint/format (no ESLint/Prettier).
- Zod at every boundary: env, request bodies, route responses, policy results, external data.
- Configuration is read only via `getEnv()` (lazy, so `next build` needs no runtime env); the
  owner URL only via `takeMigrationUrl()`. `src/instrumentation.ts` calls `process.exit(1)` when
  startup fails, because Next only logs `register()` failures. This also stops `next dev`.
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
- Better Auth: `better-auth`, `@better-auth/sso` and `@better-auth/core` are pinned to the same
  exact version (1.7.7); `task auth:generate` uses the same version. Mock IdP image
  `ghcr.io/navikt/mock-oauth2-server:6.0.5`, always addressed as `http://localhost:8080` (its
  issuer follows the Host header; never use `127.0.0.1`).
- opa v1.21.1: installed by the developer (`brew install opa`); there is no download task.
  `OPA_VERSION` in `Taskfile.yml` is checked by `task tools:check`, passed to the Docker build
  (which downloads the binary with a checksum check), and mirrored in `.github/workflows/ci.yml`
  (`open-policy-agent/setup-opa`).
- shadcn/ui uses the Base UI preset (`base-nova`); `buttonVariants` is exported from
  `src/components/ui/button.tsx`.
