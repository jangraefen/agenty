# M1 – SSO and organizations: design

Status: draft · Date: 2026-10-09 · Branch: `feat/m1-sso-organizations`

## Goal

Users sign in only through their organization's identity provider (OIDC). The
**organization** is the tenant: it is declared by the instance operator together
with its identity provider, users are provisioned into it on sign-in, and their
role comes from an IdP group claim. All later domain data is isolated per
organization by Postgres row-level security (RLS), and that isolation is proven
by tests.

**Done when:** `task ci` passes locally and in GitHub Actions, the isolation,
auth and E2E tests below exist and pass, the PR has been reviewed by a reviewer
subagent and the maintainer has approved the merge.

## Terminology

- **Organization** = tenant, the unit of data isolation. Every domain table has
  `tenant_id` referencing an organization, and RLS keys on it (`tenant_id` and
  `app.tenant_id` are the brief's names; our code APIs say `organizationId`).
- **Provider** = the organization's OIDC identity provider (IdP).
- **Workspaces** (collaboration spaces inside an organization) are a later
  feature; M1 builds nothing for them.

## Non-goals

- Email/password or any other non-SSO login, also in development (development
  uses a mock IdP). Email of any kind.
- SAML (planned for a later milestone; the config format reserves a place for
  it), SCIM, IdP-initiated login, single logout.
- Admin UI for organizations or providers; changes go through the config file
  and a restart.
- Multiple organizations per user, organization switching, invitations,
  workspaces.
- Rate limiting (Better Auth's built-in limiter is switched off explicitly; it
  would otherwise be on in production with in-memory storage).
- Domain tables (agents arrive in M2). M1 delivers the RLS machinery and proves
  it on a test fixture table.

## Decisions

| Topic | Decision |
|---|---|
| Libraries | Better Auth `>=1.7.7` (1.7.7 fixes critical OAuth advisories) with the Drizzle adapter (`better-auth/adapters/drizzle`, `provider: "pg"`, `transaction: true`), the SSO plugin `@better-auth/sso` (same version as `better-auth`) and `nextCookies()` (last plugin). No email/password (`emailAndPassword` not enabled), no social providers, **no organization plugin** (see Organizations). |
| Organizations | Our own tables `auth.organization` (`id uuid`, `slug` unique, `name`, timestamps) and `auth.member` (`user_id`, `organization_id`, `role` `admin`/`member`, timestamps; unique `user_id`, i.e. one organization per user in M1). Better Auth's organization plugin is not used: without invitations, organization creation or switching it would only add endpoints to block, and the SSO plugin's own organization provisioning cannot re-derive roles on every sign-in. This deviates from the brief's "organization plugin" deliberately; workspaces may bring it (or our own model) back later. |
| Configuration file | `AUTH_CONFIG_FILE` (env) points to a JSON file, Zod-validated at start (strict: unknown keys rejected). Shape below. Secrets are never in the file: each provider names the env var holding its client secret (`clientSecretEnv`), which must be set and non-empty. |
| Organization sync | At server start, after migrations (`register()` in `src/instrumentation.ts`), organizations are upserted by `slug` (insert or rename). Organizations missing from the config are not deleted (their data stays); their users can no longer sign in, and `getTenantContext` rejects their sessions (the organization must be present in the loaded config). Invalid config stops the server, like invalid env. |
| Providers | Built from the config and passed to the SSO plugin as `defaultSSO` (in memory), so client secrets never reach the database; the `sso_provider` table exists (plugin schema) but stays empty and provider registration is switched off (`providersLimit: 0`). Each provider: `providerId` = organization slug, `issuer`, `discoveryUrl` (default `<issuer>/.well-known/openid-configuration`), `clientId`, secret from env, `scopes` (default `openid email profile`), PKCE on, claim mapping (`email`, `name`, optional role claim via `mapping.extraFields`). |
| Provider selection | The sign-in page asks for the email address; `signIn.sso({ email, callbackURL })` resolves the provider by the email's domain against the configured `domains`. Domains are unique across organizations (config validation). |
| Account binding | A user is bound to the provider that created them (Better Auth `account` row). Sign-in through another provider with the same email is rejected (Better Auth 1.7 `resolveUser` hook, decision `reject`), as is an asserted email whose domain is not in the provider's organization's `domains`. Together these stop one organization's IdP from signing in as another organization's user. |
| Provisioning | On every SSO sign-in (`provisionUser` with `provisionUserOnEveryLogin: true`): upsert the user's `member` row for the provider's organization and set `role` from the role rule: `admin` if the configured claim (string or string array) contains one of the configured admin values, else `member`. Removing someone from the IdP group demotes them at their next sign-in. The SSO plugin's own `organizationProvisioning` is disabled. |
| Sessions | Database sessions, cookie cache off (Better Auth default, pinned explicitly), secure cookies in production. No active-organization field: in M1 the tenant is the user's single membership. |
| Trusted origins | `BETTER_AUTH_URL` plus the origins of all configured issuers. Better Auth only accepts discovery/token endpoints on loopback or private hosts (like the dev mock IdP) when their origin is trusted. The first plan task checks that this has no wider effect (redirect targets, CSRF origin checks) and documents the outcome. |
| IDs | `advanced.database.generateId: "uuid"`; id and foreign key columns are `uuid`. |
| Auth tables | Schema `auth` (new, owned by `agenty_owner`; `agenty_app` gets `USAGE` and table privileges via default privileges, same pattern as `app`): `user`, `session`, `account`, `verification`, `sso_provider`, `organization`, `member`. **No RLS**: they are read before a tenant is known. Only `src/server/auth/**` may import the auth schema or instance (Biome `noRestrictedImports`). Accepted risk: session tokens are readable by `agenty_app`; raw SQL against `auth` outside `src/server/auth/` is not allowed (review rule). |
| Schema source | `src/server/db/auth-schema.ts` is hand-maintained: written once from the Better Auth CLI output (`auth generate`, which emits plain `pgTable`), moved to `pgSchema("auth")`, snake_case columns, plus our `organization`/`member` tables. `task auth:generate` writes the CLI output to `build/auth-schema.generated.ts` (git-ignored) for diffing after upgrades; it never overwrites the committed file. |
| Configuration (env) | New, Zod-validated in `getEnv()`: `BETTER_AUTH_SECRET` (≥ 32 chars), `BETTER_AUTH_URL` (http(s) URL), `AUTH_CONFIG_FILE` (path). Plus the secret env vars the config names. `.env.example` gets development values; Playwright and the Docker CI job pass their own. The Better Auth instance is created lazily (`getAuth()`), so `next build` still needs no runtime env. |

### Configuration file

```json
{
  "organizations": [
    {
      "slug": "acme",
      "name": "Acme",
      "domains": ["acme.test"],
      "oidc": {
        "issuer": "http://localhost:8080/acme",
        "clientId": "agenty",
        "clientSecretEnv": "ACME_OIDC_CLIENT_SECRET",
        "scopes": ["openid", "email", "profile"],
        "roles": { "claim": "groups", "admin": ["agenty-admins"] }
      }
    }
  ]
}
```

Validation: `slug` matches `^[a-z0-9][a-z0-9-]{1,62}$` and is unique; `domains`
non-empty, lower-case, unique across organizations; `issuer` and optional
`discoveryUrl` are http(s) URLs (plain http is accepted because the operator
is trusted; README recommends https outside development); `roles`
optional (everyone `member` without it). The OIDC block is named `oidc` so a
`saml` block can be added later. Error messages name the JSON path, never
secret values.

## Tenant context

`src/server/auth/tenant.ts` (server-only):

```ts
declare const brand: unique symbol;
export type TenantContext = {
  readonly userId: string;
  readonly organizationId: string;
  readonly role: "admin" | "member";
  readonly [brand]: true;
};
export async function getTenantContext(): Promise<TenantContext>;
```

- Loads the session from the request headers; none → `UnauthorizedError`.
- Re-reads the user's `member` row on every call and requires its
  organization to be in the loaded config; otherwise `ForbiddenError`.
- Our code never takes the organization id from a request body, query string
  or header. `TenantContext` can only be constructed in this module.

`src/server/db/tenant.ts` (server-only):

```ts
export async function withTenant<T>(ctx: TenantContext, fn: (tx: Tx) => Promise<T>): Promise<T>;
```

Opens a transaction on the `agenty_app` pool, runs
`select set_config('app.tenant_id', ${ctx.organizationId}, true)` (bind
parameter) and then `fn(tx)`. Domain code reads and writes domain tables only
through `withTenant`.

## RLS for domain tables (`app` schema)

`src/server/db/tenant-table.ts` provides the building blocks M2+ use for every
domain table:

- `tenantId()` column: `tenant_id uuid not null references auth.organization(id) on delete cascade`.
- `tenantIsolation(table)` policy (Drizzle `pgPolicy`, permissive, `for: "all"`,
  `to` the existing role `agenty_app`): `USING` and `WITH CHECK` both
  `tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid`.
  Without a tenant set the expression is `NULL`: nothing is visible and every
  write fails. A malformed value raises an error (fails closed).
- RLS enabled on the table. No `FORCE ROW LEVEL SECURITY`: only the owner
  bypasses RLS, owner credentials are removed at server start, migrations may
  need cross-tenant access, and an existing test asserts that `agenty_app`
  owns nothing and is a member of no role.

**Catalog guard** (integration test): every table in schema `app` has
`tenant_id uuid not null`, `relrowsecurity = true` and a policy for
`agenty_app` whose `USING` and `WITH CHECK` reference `app.tenant_id`. It passes
vacuously in M1; a test proves it fails for a fixture table without RLS.
Exceptions must be listed in the test with a reason.

## Tests

**Isolation** (integration, as `agenty_app`, against a fixture table built
with the same helpers in a test-only schema `rls_fixture` that the setup
creates as `agenty_owner` and drops):

1. In organization A's context only A's rows are visible.
2. With no tenant set nothing is visible and inserts fail.
3. As A, inserting a row with B's `tenant_id` fails.
4. As A, updating a row's `tenant_id` to B fails; updating or deleting B's
   rows affects zero rows.
5. On a `max: 1` pool, after A's transaction commits or rolls back, the next
   transaction sees nothing.
6. A malformed `app.tenant_id` raises an error.
7. `withTenant` passes the id as a bind parameter.

**Configuration and provisioning** (unit and integration):

- Config parsing: valid file; each validation rule rejected with its JSON path;
  missing secret env var; secrets never in error messages.
- Sync: inserts, renames, leaves removed organizations in place; idempotent.
- Role rule: claim as string and as array, no match, claim missing, no rule.
- Provisioning: first sign-in creates the member row with the derived role; a
  later sign-in with changed groups updates the role.
- Binding: same email through another provider is rejected; an email outside
  the provider's domains is rejected; nothing is created in either case.
- `getTenantContext`: no session, no membership, organization removed from
  config, valid member.
- Wiring: the configured Better Auth plugin list is exactly the expected one;
  email/password endpoints do not exist; provider registration is rejected.

These run against the dev database and the mock IdP (compose service), with
unique users (`<test>-<random>@acme.test`) that are deleted afterwards.

**E2E** (Playwright, production build, mock IdP, dev config):

- Signed-out access to an app page redirects to `/sign-in`.
- An Acme user in group `agenty-admins` signs in, sees "Acme" and the members
  page; an Acme user without the group sees no members link and gets 404 on
  the page.
- A Globex user sees only Globex and only Globex members.
- Signing in with an `@acme.test` email at the Globex IdP is rejected with a
  readable error.
- Sign-out ends the session.

## Development, CI and test IdP

- `ghcr.io/navikt/mock-oauth2-server` (pinned tag) in `docker-compose.yml` on
  port 8080 and as a service in the CI `ci` job. It serves one issuer per path
  (`/acme`, `/globex`) and shows a login form where any username and claims
  (JSON) can be entered; Playwright fills that form.
- `config/agenty.dev.json` (committed) declares Acme (`acme.test`) and Globex
  (`globex.test`) against the mock; `.env.example` points `AUTH_CONFIG_FILE` to
  it and sets the two (dummy) client secrets.
- The Docker CI job mounts the dev config and sets its env, so the image starts
  with a valid configuration (its smoke test still only checks health).
- Production runtime stays the app plus Postgres; the mock IdP is dev/test only.

## UI and routes

- `src/app/api/auth/[...all]/route.ts`: `toNextJsHandler(getAuth())`.
- `/sign-in`: email field, "Continue with SSO"; errors from the callback
  (unknown domain, rejected account) shown as readable messages, never raw.
- `/`: signed out → landing page (M0 heading stays) with a sign-in button;
  signed in → home placeholder ("Agents arrive in M2").
- Signed-in shell with a header: organization name, user menu (name, email,
  role, sign out).
- `/settings/members`: read-only member list (name, email, role, last sign-in)
  of the user's organization; admins only (others get 404).
- `src/proxy.ts`: optimistic redirect to `/sign-in` when no session cookie is
  present (`getSessionCookie`); pages and route handlers always validate the
  session themselves.
- Client calls go through `src/lib/auth-client.ts` (`createAuthClient` +
  `ssoClient()`).

## Database and migrations

- Migration: `CREATE SCHEMA auth`, `GRANT USAGE` to `agenty_app`, default
  privileges for `agenty_owner` in `auth` (tables: select, insert, update,
  delete; sequences: usage, select), then the tables.
- `roles.int.test.ts` widened to schema `auth` (owner, table ownership, app role
  cannot create objects) and pins schema `USAGE` and sequence privileges for
  both schemas.

## Docs

AGENTS.md: terminology, SSO-only, config file and sync, schema `auth` and the
import restriction, `getTenantContext`/`withTenant`, the
`tenantId()`/`tenantIsolation()` rule for every new domain table, the catalog
guard, new env vars, the mock IdP, `task auth:generate`. README: production
configuration (config file, secrets env vars, `BETTER_AUTH_*`), registering
Agenty at an IdP (redirect URI `<BETTER_AUTH_URL>/api/auth/sso/callback/<slug>`),
and that removing an organization from the config disables it without deleting
data.

## Risks (verified first in the plan)

- **`defaultSSO` in production:** its type comment says "for testing". The
  first plan task verifies precedence over the database, domain matching, that
  it needs no domain verification, and that it works with `providersLimit: 0`.
  If `defaultSSO` is unfit, there is no clean fallback (the plugin stores
  `sso_provider` secrets in plain text), so the work stops and the maintainer
  decides.
- **Hook order and transactions:** `resolveUser`, `provisionUser` and session
  creation order, and whether provisioning runs in the sign-in transaction,
  verified with integration tests against the mock IdP before UI work.
- **Trusted origins:** effect of adding issuer origins (see Decisions).
- **TypeScript 7 and Better Auth's inferred types:** checked in the first task;
  if inference breaks, the few shapes we need are typed explicitly with Zod.
- **Cache Components:** pages that read the session are dynamic; they follow
  the Next 16 docs in `node_modules/next/dist/docs/`.
