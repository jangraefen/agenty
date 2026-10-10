# Agenty

This is the only agent instruction file in the repo; `CLAUDE.md` just imports it (`@AGENTS.md`)
for Claude Code. Keep project knowledge here. The block between the `nextjs-agent-rules` markers
is managed by `next dev` (it re-adds it when missing): leave it in place and don't edit it.

<!-- BEGIN:nextjs-agent-rules -->

## This is NOT the Next.js you know

This version has breaking changes — APIs, conventions, and file structure may all differ from your training data. Read the relevant guide in `node_modules/next/dist/docs/` (resolved from this file's directory; in monorepos the `next` package may not be visible from the repo root) before writing any code. Heed deprecation notices.

This block is written and re-added by `next dev` — verify at `node_modules/next/dist/server/lib/generate-agent-files.js`. Removing it from a diff only re-creates the uncommitted change; committing it with your work keeps the tree clean.

<!-- END:nextjs-agent-rules -->

Self-hosted AI agent builder with chat. Users sign in through one OIDC identity provider
(single-tenant for now), collaborate in workspaces, which will own agents, tools, provider keys and policies, and chat
with agents (chats stay private to their user). Every
tool call is checked by an OPA policy; agent runs are durable (Workflow SDK, Postgres world).

Runtime components: the Next.js app (standalone Node server), Postgres, and the operator's OIDC
identity provider (development and CI: a mock IdP container).

## Commands

All commands go through `Taskfile.yml` (go-task), identically locally and in CI.

| Command | Does |
|---|---|
| `task setup` | Check tools, `pnpm install`, Playwright Chromium, `.env` from `.env.example` |
| `task tools:check` | Check node, pnpm, docker and opa are installed; warn if the opa version differs from `OPA_VERSION` |
| `task playwright:install` | Install Playwright's Chromium (with OS dependencies when `CI=true`) |
| `task dev` | Postgres and mock IdP up, policy build, `next dev` (the server applies migrations on start) |
| `task db:up` / `db:down` / `db:reset` | Local Postgres and the mock IdP (compose; the IdP listens on port 8080); `db:reset` deletes data (needed after editing `docker/postgres/init.sql`) |
| `task db:check` | Fail fast if `DATABASE_URL` is unreachable (prints the error code, never the URL) |
| `task auth:generate` | Regenerate `src/server/db/auth-schema.ts` from the Better Auth config (never edit it by hand) |
| `task db:generate` | New migration from `src/server/db/schema.ts`; `-- --custom --name x` for hand-written SQL |
| `task db:migrate` | Apply migrations without starting the app, for tests and CI (`scripts/migrate.ts`) |
| `task db:studio` | Drizzle Studio |
| `task policy:test` / `policy:build` | `opa test`; compile `policies/` to `build/policy/policy.wasm` |
| `task lint` / `task format` | Biome check; apply Biome fixes incl. Tailwind class sorting |
| `task typecheck` | `next typegen` + `tsc --noEmit` |
| `task test` | Vitest: `unit` (`*.test.ts`) and `integration` (`*.int.test.ts`, needs Postgres) |
| `task test:e2e` | Playwright against the standalone build on port 3100 |
| `task build` | `scripts/build.sh`: self-contained `.next/standalone` |
| `task docker:build` | Production image `agenty:local` |
| `task ci` | Everything the `ci` job checks (the `docker` job additionally builds the image and starts it against an empty database, which it migrates); locally run `task db:up` first |

Run tests through `task` (it loads `.env`); plain `pnpm exec vitest` lacks `DATABASE_URL`.

## Architecture

```
src/app/            routes, pages, route handlers (thin: parse, call src/server, respond)
  layout.tsx        root layout: header (user menu when signed in) and the shared `<main>`
  sign-in/          sign-in page, sign-in server action, fixed error messages
  api/workspaces/   invite user search route (the only client-side read)
  workspaces/       workspace list, create form, invitations; shared action runner and error messages
  w/[workspaceId]/  workspace home and settings (every page checks membership itself)
src/components/ui/  shadcn/ui components (Base UI preset `base-nova`)
src/lib/            shared client/server utilities (utils.ts re-exports `cn` from the `cn` package)
src/instrumentation.ts  server start: validates env, applies migrations
src/server/         server-only code (every module imports "server-only")
  env.ts            lazily Zod-parsed configuration (getEnv)
  auth/             Better Auth instance (auth.ts) and the server API for the rest of the app (session.ts)
  db/               Drizzle schema, generated auth-schema.ts, client (getDb), migrations
  health.ts         health check
  policy/           OPA WASM loading and evaluation
  workspaces/       workspace rules (workspaces.ts, invitations.ts), access helpers (access.ts)
policies/           Rego sources and tests
scripts/            build, policy build, migration, DB check (shared by Taskfile and Dockerfile)
docker/postgres/    init.sql: database roles
tests/e2e/          Playwright specs
tests/support/      test helpers (oidc.ts drives an OIDC sign-in against the mock IdP over HTTP;
                    users.ts creates users directly for workspace tests)
docs/specs, docs/plans  per-milestone design and implementation plan
```

Folders `src/server/{agents,tools,workflows,crypto}` are created by the milestones that need them.

### Database

- Two roles, created by `docker/postgres/init.sql` (compose runs it on an empty volume; CI via psql):
  - `agenty_owner`: owns schema `app` and all its tables, runs migrations (`DATABASE_MIGRATION_URL`).
  - `agenty_app`: runtime role (`DATABASE_URL`); no superuser, no `BYPASSRLS`, owns nothing, cannot
    create objects. Gets table/sequence access through default privileges. It cannot bypass RLS,
    so any RLS added later (e.g. with tenancy) applies to it; there is none today.
- Schema `app` and grants come from migrations (0000, 0001), not from init.sql. Prefer migrations
  for future role/grant changes; init.sql only runs on an empty data directory.
- Drizzle's bookkeeping lives in schema `drizzle` (owner only).
- Reserved for the workflow world (M3), names hardcoded by `@workflow/world-postgres`: `workflow`,
  `workflow_drizzle`, `graphile_worker`. Its bootstrap falls back to `DATABASE_URL`, i.e. the app
  role, so M3 must set `WORKFLOW_POSTGRES_URL` and design roles/grants for it.
- Better Auth tables (`user`, `session`, `account`, `verification`) live in `app` like everything
  else; there is no RLS (see rule 1). Timestamps are `timestamp` without time zone (the generator's
  default).
- Integration tests run against the dev database for now. The E2E sign-ins create users there that
  are never removed; `task db:reset` clears them.

### Auth

Better Auth 1.7.7 with its built-in `genericOAuth` plugin: one OIDC connector, provider id `oidc`,
no organization plugin, email/password or rate limiting (workspaces and their roles are our own,
see Workspaces). Defined in `src/server/auth/auth.ts`.

- **Env:** `BETTER_AUTH_SECRET` (at least 32 chars), `BETTER_AUTH_URL`, `OIDC_DISCOVERY_URL`,
  `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET`; all via `getEnv()`. Scopes `openid email profile`; the ID
  token is verified (`requireIdTokenVerification`). Callback: `<BETTER_AUTH_URL>/api/auth/callback/oidc`.
- **Discovery** happens when the auth instance is created, at the first request (`getAuth()`, so
  `next build` needs no env). Better Auth's discovery fetch has no timeout and every `getSession()`
  waits for it, so `getAuth()` first fetches the discovery document itself with a 5 s timeout and
  leaves the provider out if that fails. Better Auth also skips a provider whose own discovery
  failed. `getAuth()` keeps an instance without the provider for 30 seconds and then recreates it
  (`memoizeUntil`): an IdP outage heals itself, and a hanging IdP delays pages by at most 5 s per
  attempt. Remaining race: an IdP that answers the check and then hangs before Better Auth's own
  fetch still stalls requests. Meanwhile sign-in answers `PROVIDER_NOT_FOUND`, which the sign-in
  page shows as "unavailable".
- **Sessions:** database sessions, 12 hours absolute, no refresh, so a user removed at the IdP
  loses access within 12 hours. Sign-out is local only (`disableProviderLogout`): it ends the
  Agenty session, not the IdP session. Default account linking is kept.
- **ID-token hook:** `hooks.before` rejects `/sign-in/social` and `/link-social` requests carrying
  `idToken`. Better Auth would accept such a bare ID token (with a client-chosen nonce) as a
  sign-in, so a leaked or replayed token could start a session. Only the code flow signs in.
- **Tokens:** `account.encryptOAuthTokens` encrypts access and refresh tokens; the ID token stays
  plain text (Better Auth limitation; replay is blocked by the hook). `BETTER_AUTH_SECRET` is the
  encryption key and signs sessions: rotating it signs everyone out and makes stored tokens
  unreadable.
- **Sign-in:** the sign-in page is a plain `<form>` whose server action (`src/app/sign-in/actions.ts`)
  calls `auth.api.signInSocial` and redirects to the IdP. The `nextCookies()` plugin (last in
  `plugins`, as Better Auth requires) writes the cookies Better Auth sets in `auth.api` calls, here
  the signed state cookie, through Next's `cookies()`; without it the callback fails with
  `state_mismatch`. A missing provider (`PROVIDER_NOT_FOUND`) redirects to
  `/sign-in?error=sign_in_unavailable`, any other failure to `?error=sign_in_failed` (only the error
  class is logged).
- **Errors:** `onAPIError.errorURL` is `/sign-in`, so state errors land on the sign-in page. The page
  maps `?error=` to fixed messages and never shows IdP text. It logs `?error=` and
  `error_description` server-side (`signInErrorLogLine`: control characters removed, 200 characters
  each, after `connection()` because Partial Prefetching renders the page twice per request);
  `error_description` is never rendered.
- **Use in code:** `getCurrentUser()` (`{ id, name, email }` or null, once per request) and
  `requireUser()` (redirects to `/sign-in`) from `src/server/auth/session.ts`. Code that can run
  during prerender must read `headers()` before `getAuth()`; `getCurrentUser()` does. Workspace
  pages use `src/server/workspaces/access.ts` on top of these.
- **Personal workspace hook:** `databaseHooks.session.create.before` ensures the personal workspace
  at every sign-in, before the session row exists; a failure is rethrown as `APIError` code
  `sign_in_failed`, so the callback redirects to `/sign-in?error=sign_in_failed` without a session
  (`createAuth`'s `ensureWorkspace` option replaces it in tests).
- **Tables:** `src/server/db/auth-schema.ts` is generated by `task auth:generate` (config in
  `scripts/auth-generate.config.ts`); never edit it. After a Better Auth upgrade, rerun it, then
  `task db:generate`.
- **Dev/CI IdP:** `mock-oauth2-server` on `http://localhost:8080`, issuer `/agenty`. Always use
  `localhost` (its issuer follows the Host header). In docker-compose, `JSON_CONFIG` turns a
  username that is an email address into the `email`/`name` claims; such mapped claims override the
  login form's, so tests use usernames without `@` and pass their claims in the form. The CI service
  runs without it.

### Workspaces

Own tables (`workspace`, `workspace_member`, `workspace_invitation`), not Better Auth's
organization plugin. Rules (spec: `docs/specs/2026-10-10-m2-workspaces-design.md`):

- Every user has one personal workspace (`personal_user_id`), ensured at every sign-in in
  `databaseHooks.session.create.before` and by `/`. It can't be renamed, deleted or shared; its
  Settings link is disabled (tooltip) and its settings page redirects to the workspace home.
- Roles `admin` and `member`. Admins rename, delete, invite, cancel invitations, remove members
  and change roles (later: manage agents, tools, keys). Members use the workspace and can leave.
- At least one admin at all times (the last admin can't leave, be removed or be demoted).
- Invitations only for existing users (admins search all users by name/email; members and invitees
  are listed as such); the invitee accepts or declines. No email.
- Users are never deleted for now (Better Auth's `deleteUser` is off).

Code: service functions in `src/server/workspaces/` take the acting user's id explicitly and throw
`WorkspaceError` with a fixed code. Every mutating function (except create, ensure and decline, which need no lock) runs in one transaction that locks the
workspace row first, then reads the actor's role. Pages use `requireWorkspaceMember` /
`requireWorkspaceAdmin` (`access.ts`); non-members get the not-found page (a soft 404, since pages
stream). Server actions go through `runWorkspaceAction` (`src/app/workspaces/run-action.ts`):
errors redirect back with `?error=<code>`, shown as fixed text (`error-messages.ts`). Server actions
that change data shown in layouts (workspace name, header invitation count) call `refresh()` from
`next/cache` before `redirect()`, as `runWorkspaceAction` does; otherwise the client reuses the old
layouts after the redirect.
The invite search on the settings page is the one client-side read: a client component fetches
`GET /api/workspaces/[workspaceId]/users?q=` as the admin types (route handler, not a server
action: actions are for mutations and run one at a time per client). The handler takes the user
from the session, validates query and response with Zod (`src/lib/user-search.ts`) and answers
fixed error codes (401, 404 for non-members, 403 for non-admins and the personal workspace).

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

## Non-negotiable rules

1. **Single-tenant for now (maintainer decision):** no `tenant_id`/RLS. Workspace isolation is
   enforced in app code: every workspace-owned table has `workspace_id` and is only read or written
   after `requireWorkspaceMember`/`requireWorkspaceAdmin` or a service check against the session
   user. Chats are private to their user: they also need an owner check. Ids in URLs and forms are
   lookup keys only. Adding tenancy later means `tenant_id` + RLS on every domain table.
2. **Policy before every tool call:** tools are never registered directly; always
   `withPolicy(tool, ctx)`. Every decision is logged to `policy_decisions`. Creating/modifying
   tools is policy-checked too.
3. **Idempotent steps:** policy check and tool execution run in one workflow step; every tool call
   carries the idempotency key `runId:toolCallId` to external calls.
4. **Secrets:** provider keys (per workspace) and tool credentials are stored AES-256-GCM encrypted
   (master key from env). Secrets never appear in prompts, logs, error messages or API responses.
5. **Untrusted input:** tool descriptions and results are untrusted (prompt injection). HTTP tools
   have SSRF protection (no private/internal IPs, domain allowlist configured by the instance admin), timeouts and
   response size limits.

## Conventions

- TypeScript strict + `noUncheckedIndexedAccess`; Biome for lint/format (no ESLint/Prettier).
- Zod at every boundary: env, request bodies, route responses, policy results, external data.
- Configuration is read only via `getEnv()` (lazy, so `next build` needs no runtime env); the
  owner URL only via `takeMigrationUrl()`. `src/instrumentation.ts` calls `process.exit(1)` when
  startup fails, because Next only logs `register()` failures. This also stops `next dev`.
- Cache Components requires request-time data (session, `params`, `searchParams`) inside a
  `<Suspense>`, and a client navigation only re-renders below the layout both routes share, so a
  boundary in a parent layout doesn't count there. Every page that reads request-time data has a
  `loading.tsx` next to it (fallback `null`), which wraps that page in its own boundary (or the
  page wraps the reading part in `<Suspense>` to keep a static part instant, like sign-in). A
  layout that reads request-time data wraps that part in `<Suspense>` itself (the workspace nav).
  `next build` only checks page loads; `next dev` logs `Route "...": ... uncached data` for
  navigations.
- Route handlers that touch the database without reading the request (e.g. `/api/health`) call
  `await connection()` first (Cache Components is on; otherwise Next may prerender them at build
  time). Handlers that read the request, like the auth route, are dynamic anyway.
- Error bodies never contain internal details; log them server-side. Never echo connection strings
  (`scripts/check-db.mjs` prints only the error code, e.g. `ECONNREFUSED`).
- Shell scripts stay POSIX `sh` and must work with BSD/macOS tools (e.g. `scripts/build-policy.sh`
  handles both GNU tar and bsdtar).
- pnpm `allowBuilds` decisions for dependency build scripts live in `pnpm-workspace.yaml`.
- Tests are named after the guarantee they protect. Unit: `*.test.ts`; integration: `*.int.test.ts`.
  Vitest config is `vitest.config.mts` (native `resolve.tsconfigPaths`). Playwright queries use
  `exact: true` where a role/name could match more than one element.
- UI: server components and server actions with plain `<form>`s; no client data-fetching library.
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
- `better-auth` is pinned to exactly 1.7.7 (also the `auth` CLI version in `task auth:generate`).
- Mock IdP: `ghcr.io/navikt/mock-oauth2-server:6.0.5` (docker-compose and the `ci` job).
- shadcn/ui uses the Base UI preset (`base-nova`); `buttonVariants` is exported from
  `src/components/ui/button.tsx`.
