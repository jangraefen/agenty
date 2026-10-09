# M1 – SSO and organizations: design

Status: draft · Date: 2026-10-09 · Branch: `feat/m1-sso-organizations`

## Goal

Users sign in only through an OIDC identity provider (IdP). The
**organization** is the tenant: its slug comes from a claim in the user's ID
token, it is created on the first sign-in that names it, and it belongs to the
provider that created it. A user's role comes from an IdP group claim. All
later domain data is isolated per organization by Postgres row-level security
(RLS), and that isolation is proven by tests.

**Done when:** `task ci` passes locally and in GitHub Actions, the isolation,
auth and E2E tests below exist and pass, the PR has been reviewed by a reviewer
subagent and the maintainer has approved the merge.

## Terminology

- **Provider** = an OIDC identity provider registered in Agenty (a row in
  `auth.sso_provider`). One provider can serve several organizations.
- **Organization** = tenant, the unit of data isolation. Every domain table has
  `tenant_id` referencing an organization, and RLS keys on it (`tenant_id` and
  `app.tenant_id` are the brief's names; our code APIs say `organizationId`).
- **Workspaces** (collaboration spaces inside an organization) are a later
  feature; M1 builds nothing for them.

## Non-goals

- Email/password or any other non-SSO login, also in development (development
  uses a mock IdP). Email of any kind.
- SAML (later milestone), SCIM, IdP-initiated login, single logout.
- A UI or CLI for providers or organizations. Until the UI exists, operators
  insert provider rows with SQL (README has the statements).
- Encrypting provider client secrets (maintainer decision, may come back; see
  Providers).
- Multiple organizations per user, organization switching, invitations,
  workspaces.
- Rate limiting (Better Auth's built-in limiter is switched off explicitly; it
  would otherwise be on in production with in-memory storage).
- Domain tables (agents arrive in M2). M1 delivers the RLS machinery and proves
  it on a test fixture table.

## Decisions

| Topic | Decision |
|---|---|
| Libraries | Better Auth `1.7.7` (exact; ≥ 1.7.7 for critical OAuth fixes), `@better-auth/sso` and `@better-auth/core` at the same exact version, Drizzle adapter (`provider: "pg"`, `transaction: true`). No email/password, no social providers, no `nextCookies()` (no server action sets auth cookies), **no organization plugin** (see Organizations). |
| Providers | Stored in the SSO plugin's own table `auth.sso_provider` (`provider_id` unique, `issuer`, `domain`, `oidc_config` JSON text including the client secret). Rows are written by the operator (SQL, documented in README) or the dev seed, never through the plugin's registration endpoints (disabled; `providersLimit: 0`). `user_id` (the plugin's owner column, only read by its management endpoints) is nullable and stays empty; `organization_id` stays empty. `defaultSSO` is not used. **Client secrets are stored in plain text** (maintainer decision; an explicit exception to the secrets rule in AGENTS.md, readable by `agenty_app` like the session tokens). The app Zod-validates a provider's row and settings when it uses them; an invalid row makes that provider fail with `idp_unavailable` and logs the invalid field names (never values). |
| Provider settings | Our own table `auth.sso_provider_settings` (`provider_id` primary key, references `sso_provider.provider_id` on delete cascade): `organization_claim` (claim holding the organization slug, e.g. `org`), `name_claim` (optional claim holding the organization's display name), `role_claim` and `admin_values` (optional: `admin` when the claim contains one of the values, else `member`), `private_network` (boolean, see Trusted origins). A provider without a settings row cannot sign anyone in. |
| Seed data | `task db:seed` (`scripts/seed.ts`, plain Node, idempotent upsert) writes two providers against the mock IdP: `corp` (issuer `http://localhost:8080/corp`, domains `corp.test`) and `partner` (issuer `http://localhost:8080/partner`, domains `partner.test`), both with `organization_claim` `org`, `name_claim` `org_name`, `role_claim` `groups`, `admin_values` `{agenty-admins}`, `private_network` true, dummy secrets. Used by `task dev`, `task ci` (after migrating), integration tests (`beforeAll`, idempotent) and E2E. |
| Organizations | Our own tables `auth.organization` (`id uuid`, `slug` unique, `name`, `provider_id` (the provider that created it; references `sso_provider.provider_id` `on delete set null`, see Removal), timestamps) and `auth.member` (`user_id` unique, i.e. one organization per user in M1; `organization_id`; `role` `admin`/`member`; `last_sign_in_at`; timestamps). Created **just in time**: on a sign-in whose organization claim names a slug that does not exist yet, the organization is created with that slug, the name from `name_claim` (else the slug), and the signing-in provider as its owner. Later sign-ins update the name from the claim. Better Auth's organization plugin is not used: without invitations, organization creation endpoints or switching, it would only add endpoints to block. |
| Organization rules at sign-in | The claim value must match `^[a-z0-9][a-z0-9-]{1,62}$`; missing or invalid → reject (`organization_claim_missing`). An existing organization owned by **another** provider → reject (`organization_owned_by_other_provider`): slugs are global, so without this check a second IdP could place its users into the first IdP's organization. A user whose existing membership is in a different organization → reject (`organization_changed`); an operator removes the old membership if the move is intended. |
| Removal | Deleting a provider row (SQL) leaves its organizations and their data in place: `organization.provider_id` references `sso_provider.provider_id` `on delete set null`. Orphaned organizations reject their users' sessions (`getTenantContext`), and no provider can claim them again until an operator reassigns `provider_id` (README). README rules for operators: never change an existing provider's `issuer` (accounts are keyed by provider id and `sub`; re-pointing an id at another IdP could bind foreign subjects to existing users), register a new provider id instead; domains must not overlap between providers (the plugin picks the first match) and contain no commas. |
| OIDC discovery | We discover ourselves instead of the plugin (which re-discovers on every sign-in and callback and requires the discovery URL **and every discovered endpoint** to be trusted origins, which also makes them redirect targets and CSRF origins). Discovery is **lazy per provider**: a `hooks.before` on `/sign-in/sso` and `/sso/callback/:providerId` resolves the provider (by email domain, or by path) and, if its stored `oidc_config` lacks endpoints or they are older than 1 hour, fetches `<issuer>/.well-known/openid-configuration` (or the configured discovery URL) with a timeout, Zod-validates it (its `issuer` must equal the stored one exactly), and writes `authorizationEndpoint`, `tokenEndpoint`, `jwksEndpoint`, `userInfoEndpoint` and a `discoveredAt` timestamp into the row's `oidc_config`. With those keys present the plugin performs no discovery itself (`needsRuntimeDiscovery`). A failing refresh keeps the stored endpoints if there are any. An unreachable or invalid IdP only breaks that provider's sign-in (`idp_unavailable`); server start never depends on an IdP. |
| Provider selection | The sign-in page asks for the email address (exactly one `@`, validated with `z.email()`); `signIn.sso({ email, callbackURL, errorCallbackURL })` lets the plugin pick the provider by the email's domain (case-insensitive, subdomains included). Overlapping domains between providers are an operator error documented in README. |
| Account binding | Better Auth already refuses implicit linking for SSO users (their email is never marked verified); we pin `account.accountLinking.disableImplicitLinking: true` and keep `domainVerification` unset (with it on, the plugin would treat providers as trusted for linking). The plugin's `resolveUser` hook (runs on every SSO sign-in inside the transaction that creates user, account and session) **returns** `reject` (never throws) when the asserted email's domain is not one of the provider's domains, or when the email belongs to a user bound to another provider. A rejection rolls back everything. |
| Provisioning | Organization slug, name and role are decided in `resolveUser` from the **verified ID token claims** (`verifiedIdTokenClaims`, not userinfo, which some IdPs such as Entra ID return without `groups`) and the provider's settings, including the organization rules above. The decision is handed to `databaseHooks.session.create.before` (same request, same transaction) through a `WeakMap` keyed by the Better Auth endpoint context object (verified in source: both hooks see the same object). There, through **Better Auth's transaction adapter** (`await getCurrentAdapter(...)`; a separate connection would block on the foreign key to the uncommitted user and would not roll back), the organization is created or renamed and the `member` row created or updated (role, `last_sign_in_at`). Our tables are declared to Better Auth by a small local plugin schema (all required columns, so Better Auth's schema check passes). Every failure throws an `APIError` with a code, which rolls back the sign-in and redirects to `/sign-in?error=<code>`. No retries and no after-commit fallback. The first plan task prototypes this before anything builds on it; if it cannot work, the work stops and the maintainer decides. |
| Sessions | Database sessions, cookie cache off (pinned), secure cookies whenever `BETTER_AUTH_URL` is https. **Absolute lifetime 12 hours, no refresh** (`expiresIn`, `disableSessionRefresh: true`): roles and memberships are re-derived only at sign-in, so a demotion or removal at the IdP takes effect at the latest 12 hours later (documented). No active-organization field: in M1 the tenant is the user's single membership. |
| Stored IdP tokens | `account.encryptOAuthTokens: true` encrypts the access token Better Auth stores in `auth.account`. The ID token stays in plain text (accepted: short-lived, readable only by `agenty_app`). No refresh token is requested. |
| Endpoints | Allowlist: `get-session`, `sign-out`, `sign-in/sso`, `sso/callback/:providerId`. Every other endpoint core and the plugin register is rejected with 404 by a `hooks.before` middleware checking the endpoint path (`disabledPaths` cannot match parameterised paths); a test enumerates all endpoints and pins the reachable ones. On `/sign-in/sso` only the body keys `email`, `callbackURL`, `errorCallbackURL` are accepted (no `scopes`, `additionalParams`, `providerId`, `organizationSlug`, `providerType`, `requestSignUp`, …), so nobody can request extra scopes or inject authorization parameters. A callback for an unknown provider id gets 404. |
| Errors | `errorCallbackURL: "/sign-in"` and `onAPIError.errorURL: "/sign-in"`. The sign-in page maps a fixed set of `error` codes (`unknown_domain`, `idp_unavailable`, `email_domain_mismatch`, `account_bound_to_other_provider`, `organization_claim_missing`, `organization_owned_by_other_provider`, `organization_changed`, …) to messages, everything else to a generic one, and never renders `error_description` (IdP-controlled text). |
| Trusted origins | `BETTER_AUTH_URL`, plus, for providers with `private_network` (the dev mock IdP), their issuer origin. Discovered endpoints of such providers must share the issuer's origin (otherwise `idp_unavailable`). The plugin requires private-host endpoints to be trusted; public ones need no trust. Because the trusted list depends on provider rows, Better Auth's `trustedOrigins` is given as a function that reads it (cached briefly). README warns that trusted origins are also accepted redirect targets. |
| IDs | `advanced.database.generateId: "uuid"`; id and foreign key columns are `uuid`. |
| Auth tables | Schema `auth` (owned by `agenty_owner`; `agenty_app` gets `USAGE` and table privileges via default privileges, as for `app`): `user`, `session`, `account`, `verification`, `sso_provider`, `sso_provider_settings`, `organization`, `member`. **No RLS**: they are read before a tenant is known. Only `src/server/auth/**`, `src/server/db/**` and `scripts/` may import the auth schema (Biome `noRestrictedImports`). Accepted risk: session tokens and provider client secrets are readable by `agenty_app`. |
| Schema source | `src/server/db/auth-schema.ts` is hand-maintained (no `server-only` import, because drizzle-kit and the seed script load it in plain Node): written once from the Better Auth CLI output (`pnpm dlx auth@1.7.7 generate`), moved to `pgSchema("auth")`, timestamps with time zone, plus our tables. `task auth:generate` writes the CLI output to `build/auth-schema.generated.ts` (git-ignored) for diffing after upgrades. |
| Configuration (env) | New, Zod-validated in `getEnv()`: `BETTER_AUTH_SECRET` (≥ 32 chars), `BETTER_AUTH_URL` (http(s) URL). `.env.example` gets development values; Playwright and the Docker CI job pass their own. The Better Auth instance is created lazily (`getAuth()`), so `next build` still needs no runtime env. |

## Tenant context

`src/server/auth/tenant.ts` (server-only):

```ts
export type TenantContext = {
  readonly userId: string;
  readonly organizationId: string;
  readonly role: "admin" | "member";
  readonly [brand]: true;
};
export async function getTenantContext(): Promise<TenantContext>;
```

- Loads the session from the request headers; none → `UnauthorizedError`.
- Re-reads the user's `member` row on every call; requires the organization to
  have a provider that still exists; otherwise `ForbiddenError`.
- Our code never takes the organization id from a request body, query string
  or header. `TenantContext` can only be constructed in `src/server/auth`
  (branded type, Biome rule); domain code imports only the type.

`src/server/db/tenant.ts` (server-only):

```ts
export async function withTenant<T>(ctx: TenantContext, fn: (tx: Tx) => Promise<T>): Promise<T>;
```

Opens a transaction on the `agenty_app` pool, runs
`select set_config('app.tenant_id', ${ctx.organizationId}, true)` (bind
parameter) and then `fn(tx)`. Domain code reads and writes domain tables only
through `withTenant`.

## RLS for domain tables (`app` schema)

`src/server/db/tenant-table.ts` (no `server-only`: drizzle-kit loads it through
`schema.ts`) provides the building blocks M2+ use for every domain table:

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
`tenant_id uuid not null`, `relrowsecurity = true`, a policy for `agenty_app`
whose `USING` and `WITH CHECK` reference `app.tenant_id`, and no permissive
policy that does not. It passes vacuously in M1; a test proves it fails for
fixture tables without RLS and with a `using (true)` policy. Exceptions must be
listed in the test with a reason.

## Tests

**Isolation** (integration, as `agenty_app`, against a fixture table built
with the same helpers in a test-only schema that the setup creates as
`agenty_owner` and drops):

1. In organization A's context only A's rows are visible.
2. With no tenant set nothing is visible and inserts fail.
3. As A, inserting a row with B's `tenant_id` fails.
4. As A, updating a row's `tenant_id` to B fails; updating or deleting B's
   rows affects zero rows.
5. On a `max: 1` pool, after A's transaction commits or rolls back, the next
   transaction sees nothing.
6. A malformed `app.tenant_id` raises an error.
7. `withTenant` passes the id as a bind parameter.

**Providers** (unit and integration):

- Provider row and settings validation: valid row; each invalid field gives
  `idp_unavailable` for that provider and a log line naming the field, never
  the secret.
- Better Auth reads a seeded provider with a null `user_id`.
- Seed is idempotent.

**Sign-in and provisioning** (integration, against the dev database and the
mock IdP, through a test helper that drives the real flow over HTTP:
`signIn.sso` → mock IdP login form POST with the given claims → callback, with
a cookie jar; unique users, deleted afterwards):

- First sign-in with `org: "acme-<random>"` creates the organization (name
  from `org_name`), the user and a `member` row with the role from `groups`;
  a later sign-in updates role (demotion) and organization name.
- Missing or invalid organization claim → `organization_claim_missing`; the
  `partner` provider asserting an organization created by `corp` →
  `organization_owned_by_other_provider`; the same user asserting a different
  organization → `organization_changed`; none of these leave rows behind.
- Email outside the provider's domains → `email_domain_mismatch`; a user bound
  to another provider → `account_bound_to_other_provider`.
- A failure in the member write rolls back user, account and session.
- Discovery: endpoints are written to the row on first use and reused after a
  "restart" (new Better Auth instance); issuer mismatch and unreachable IdP give
  `idp_unavailable` for that provider only, while the other provider still
  signs in; a failing refresh keeps stored endpoints.
- Endpoints: the reachable endpoint list is exactly the allowlist; extra
  sign-in body keys are rejected; a callback for an unknown provider is 404.
- Wiring: plugin list is exactly the expected one; `domainVerification` unset;
  role read from the ID token, not userinfo.
- `getTenantContext`: no session, no membership, organization whose provider
  was removed, valid member.

**E2E** (Playwright, production build, mock IdP, seeded providers):

- Signed-out access to an app page redirects to `/sign-in`.
- A `corp` user with `org: acme` and group `agenty-admins` signs in, sees
  "Acme" and the members page; a `corp` user in `acme` without the group sees
  no members link and gets 404 on the page.
- A `corp` user with `org: globex` sees only Globex and its members.
- A `partner` user asserting `org: acme` (created by `corp`) is rejected with
  a readable error.
- An unknown email domain shows a readable error; sign-out ends the session.

E2E organization slugs carry a per-run suffix so repeated runs against the
same dev database do not collide; E2E users are not deleted (documented).

## Development, CI and test IdP

- `ghcr.io/navikt/mock-oauth2-server:6.0.5` in `docker-compose.yml` on port
  8080 and as a service in the CI jobs, always addressed as
  `http://localhost:8080` (its issuer follows the Host header). It serves one
  issuer per path (`/corp`, `/partner`) and shows a login form where any
  username and claims (JSON) can be entered; Playwright fills it, integration
  tests post it. It only emits the claims entered, so tests always provide
  `email`, `name`, `org` and, where needed, `org_name` and `groups`.
- `task dev` runs `task db:seed` after the database is up; `task ci` seeds
  after migrating. The Docker CI job needs no providers (its smoke test only
  checks health).
- Production runtime stays the app plus Postgres; the mock IdP is dev/test only.

## UI and routes

- `src/app/api/auth/[...all]/route.ts`: Better Auth's handler (lazy instance).
- `/sign-in`: email field, "Continue with SSO"; errors shown as readable
  messages, never raw.
- `/`: signed out → landing page (M0 heading stays) with a sign-in button;
  signed in → home placeholder ("Agents arrive in the next milestone").
- Signed-in shell with a header: organization name, user menu (name, email,
  role, sign out).
- `/settings/members`: read-only member list (name, email, role, last sign-in)
  of the user's organization; admins only (others get 404).
- `src/proxy.ts`: optimistic redirect to `/sign-in` when no session cookie is
  present; pages and route handlers always validate the session themselves.
- Client calls go through `src/lib/auth-client.ts` (`createAuthClient` +
  `ssoClient()`).

## Database and migrations

- Migrations: `CREATE SCHEMA auth`, `GRANT USAGE` to `agenty_app`, default
  privileges for `agenty_owner` in `auth` (tables: select, insert, update,
  delete; sequences: usage, select), then the tables.
- `roles.int.test.ts` widened to schema `auth` (owner, table ownership, app role
  cannot create objects) and pins schema `USAGE` and sequence privileges for
  both schemas.

## Docs

AGENTS.md: terminology, SSO-only, providers in the database (plain-text client
secrets as a recorded exception to the secrets rule), the seed, just-in-time organizations and their
rules, schema `auth` and the import restriction, `getTenantContext`/
`withTenant`, the `tenantId()`/`tenantIsolation()` rule for every new domain
table, the catalog guard, new env vars, the mock IdP, `task auth:generate`.
README: production setup (env vars, registering Agenty at an IdP with redirect URI
`<BETTER_AUTH_URL>/api/auth/sso/callback/<provider id>`, required claims in the
**ID token**: email, name, the organization claim, optionally name and groups),
SQL statements to add, change and remove a provider (and its settings row),
immutable issuers and the manual steps for an IdP change, non-overlapping
domains, removing a provider, the 12-hour deprovisioning lag, that trusted
origins are also redirect targets, and adding the new keys to an existing
local `.env`.

## Risks (verified first in the plan)

Verified against the 1.7.7 sources during reviews: `resolveUser` runs inside
the sign-in transaction before session creation, and a returned `reject`
persists nothing; the endpoint context object seen by `resolveUser` and by
database hooks is the same; a throw in `session.create.before` rolls back the
sign-in and redirects with its code; default account linking refuses
unverified SSO emails; `hooks.before` sees the route pattern as `ctx.path`.
Still to prove in the first plan task, before anything builds on it:

- Better Auth reading `sso_provider` rows written outside it, with a nullable
  `user_id`.
- The provisioning hand-over and the organization/member writes through the
  transaction adapter, including Better Auth's schema check accepting our
  tables.
- Discovery written into the provider row from a before-hook, so the plugin
  performs no discovery itself, and `private_network` trust for the mock.
- `trustedOrigins` as a function reading provider settings.
- **TypeScript 7 and Better Auth's inferred types:** if inference breaks, the
  few shapes we need are typed explicitly with Zod.
