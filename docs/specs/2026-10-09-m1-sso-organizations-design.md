# M1 – SSO and organizations: design

Status: draft, approved by reviewer subagent; awaiting maintainer approval · Date: 2026-10-09 · Branch: `feat/m1-sso-organizations`

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
| Organizations | Our own tables `auth.organization` (`id uuid`, `slug` unique, `name`, `issuer`, timestamps) and `auth.member` (`user_id`, `organization_id`, `role` `admin`/`member`, `last_sign_in_at`, timestamps; unique `user_id`, i.e. one organization per user in M1). Better Auth's organization plugin is not used: without invitations, organization creation or switching it would only add endpoints to block, and the SSO plugin's own organization provisioning cannot re-derive roles on every sign-in. This deviates from the brief's "organization plugin" deliberately; workspaces may bring it (or our own model) back later. |
| Configuration file | `AUTH_CONFIG_FILE` (env) points to a JSON file, Zod-validated at start (strict: unknown keys rejected). Shape below. Secrets are never in the file: each provider names the env var holding its client secret (`clientSecretEnv`), which must be set and non-empty. |
| Organization sync | At server start, after migrations (`register()` in `src/instrumentation.ts`), organizations are upserted by `slug` as `agenty_app` (`INSERT … ON CONFLICT (slug) DO UPDATE`, safe with concurrent instances): insert, or rename. Each organization row stores its provider's `issuer`; if the configured issuer of an existing organization differs, start fails (accounts are keyed by provider id and `sub`, so re-pointing a slug to another IdP could bind foreign `sub`s to existing users). Slugs are therefore immutable; README documents the manual steps for an intentional IdP change. Organizations missing from the config are not deleted (their data stays); their users can no longer sign in, and `getTenantContext` rejects their sessions (the organization must be present in the loaded config). Invalid config stops the server, like invalid env. |
| Providers | Built from the config and passed to the SSO plugin as `defaultSSO` (in memory), so client secrets never reach the database. The plugin docs (better-auth.com/docs/plugins/sso) describe `defaultSSO` as meant for development/testing and recommend registering providers via the API in production; we deviate deliberately, because registered providers store client secrets in plain text in the database, which our secrets rule forbids. Verified in the plugin source: `defaultSSO` takes precedence over the database, skips no token validation (signature, issuer, audience, userinfo `sub`), and needs no organization plugin. Registration is off (`providersLimit: 0`) and the provider-management endpoints are disabled. Each provider: `providerId` = organization slug, `issuer`, `clientId`, secret from env, PKCE on, scopes from config (default `openid email profile`). A `hooks.before` on `/sign-in/sso` accepts only the body keys `email`, `callbackURL` and `errorCallbackURL` and rejects any other (`scopes`, `additionalParams`, `providerId`, `providerType`, `requestSignUp`, …), so nobody can request e.g. `offline_access` or inject authorization parameters, mapping for `email` and `name`. |
| OIDC discovery | We discover ourselves instead of the plugin (which re-discovers on every sign-in and callback and requires the discovery URL **and every discovered endpoint** to be trusted origins, which also makes them allowed redirect targets and CSRF origins). Discovery is **lazy per provider with an in-memory cache** (1 hour; failures are not cached): a `hooks.before` on `/sign-in/sso` and `/sso/callback/:providerId` resolves the provider (by email domain, same rule as the plugin, or by path) and, if needed, fetches `<issuer>/.well-known/openid-configuration` (or `discoveryUrl`) with a timeout, Zod-validates it (its `issuer` must equal the configured one exactly) and sets the explicit `authorizationEndpoint`, `tokenEndpoint`, `jwksEndpoint` and `userInfoEndpoint` on that provider's `defaultSSO` entry. With those keys present the plugin performs no discovery of its own (`needsRuntimeDiscovery`). Endpoints can also be set explicitly in the config (no discovery for that provider). An unreachable or invalid IdP only breaks that organization's sign-in, with the readable error `idp_unavailable`; other organizations and server start are unaffected. |
| Provider selection | The sign-in page asks for the email address; `signIn.sso({ email, callbackURL, errorCallbackURL })` resolves the provider by the email's domain. The plugin matches case-insensitively and includes subdomains (`x.acme.test` matches `acme.test`), so config validation rejects a domain that equals or is a subdomain of another organization's domain. |
| Account binding | Better Auth already refuses implicit linking for SSO users (their email is never marked verified), and we pin `account.accountLinking.disableImplicitLinking: true`. In addition, the plugin's `resolveUser` hook (runs on every SSO sign-in inside the transaction that creates user, account and session) **returns** `reject` (never throws) when the asserted email's domain does not match the provider's organization `domains` (same matching rule as provider selection), or when the email belongs to a user bound to another provider. A rejection rolls back everything. It also stays correct should a provider ever be selected by something other than the email (the body-key allowlist already rejects `providerId`). |
| Provisioning | Organization and role are decided in `resolveUser` from the **verified ID token claims** (`verifiedIdTokenClaims`, not userinfo, which some IdPs such as Entra ID return without `groups`): `admin` if the configured claim (string or string array) contains one of the configured admin values, else `member`. The decision is handed to `databaseHooks.session.create.before` (same request, same transaction) through a `WeakMap` keyed by the Better Auth endpoint context object, which both hooks can reach (the database hooks receive it as their context argument; `resolveUser` via the core context helper); no AsyncLocalStorage `enterWith`. The `member` upsert in `session.create.before` writes through **Better Auth's transaction adapter** (`getCurrentAdapter`), never our own pool (a separate connection would block on the foreign key to the uncommitted user and would not roll back). It never changes an existing row's `organization_id` and sets `last_sign_in_at`. A missing decision makes the hook throw, which rolls back the sign-in. `provisionUser` (runs after commit) and the plugin's `organizationProvisioning` (disabled) are not used. No retries and no after-commit fallback: if the write fails, the sign-in fails. The first plan task prototypes the hand-over and the adapter write; if it cannot work, the work stops and the maintainer decides. |
| Sessions | Database sessions, cookie cache off (Better Auth default, pinned explicitly), secure cookies in production. **Absolute lifetime 12 hours, no refresh** (`expiresIn`, `disableSessionRefresh: true`): roles and memberships are re-derived only at sign-in, so a demotion or removal at the IdP takes effect at the latest 12 hours later (documented). No active-organization field: in M1 the tenant is the user's single membership. |
| Stored IdP tokens | Better Auth stores the IdP's access token in `auth.account`; `account.encryptOAuthTokens: true` encrypts it (with `BETTER_AUTH_SECRET`). The ID token stays in plain text (accepted: it is short-lived and readable only by `agenty_app`). No refresh token is requested. |
| Endpoints | Allowlist: Better Auth core session endpoints (get-session, sign-out), `sign-in/sso` and `sso/callback/:providerId`. Everything else the plugins expose (provider register/update/delete/list/get, shared `/sso/callback`, SAML ACS/SLO/metadata, domain verification) is in `disabledPaths`; a test pins the complete list of reachable endpoints. |
| Errors | `signIn.sso` is called with `errorCallbackURL: "/sign-in"`, and `onAPIError.errorURL` points to `/sign-in` too (state errors otherwise land on Better Auth's own error page). The sign-in page maps a fixed set of `error` codes (including `idp_unavailable`, the account-binding rejections and `unable to create session`) to messages and never renders `error_description` (IdP-controlled text). Two concurrent first sign-ins of the same user can make one fail on the unique email; it gets the generic "sign-in failed, try again" message. |
| Trusted origins | `BETTER_AUTH_URL` only. With explicit endpoints, IdPs on public hosts need no trust. A provider whose endpoints are on loopback/private hosts (the dev mock IdP) must set `"privateNetwork": true` in the config, which adds its endpoint origins to the trusted origins; README warns that trusted origins are also accepted redirect targets. |
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
        "roles": { "claim": "groups", "admin": ["agenty-admins"] },
        "privateNetwork": true
      }
    }
  ]
}
```

Validation: domains contain no commas (the plugin joins them with commas); `slug` matches `^[a-z0-9][a-z0-9-]{1,62}$` and is unique; `domains`
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
- Discovery: valid document, issuer mismatch rejected, unreachable IdP gives
  `idp_unavailable` for that organization only (another organization still
  signs in), cache hit, failures not cached, explicit endpoints skip discovery.
- Role rule: claim as string and as array, no match, claim missing, no rule;
  read from the ID token, not userinfo.
- Provisioning: first sign-in creates the member row with the derived role; a
  later sign-in with changed groups updates the role; a provisioning failure
  leaves no user, account or session.
- Sync: a changed issuer for an existing slug stops start.
- Endpoints: the reachable endpoint list is exactly the allowlist; any body key
  on `/sign-in/sso` other than `email`, `callbackURL`, `errorCallbackURL` is
  rejected.
- Binding: same email through another provider is rejected; an email outside
  the provider's domains is rejected; nothing is created in either case.
- `getTenantContext`: no session, no membership, organization removed from
  config, valid member.
- Wiring: the configured Better Auth plugin list is exactly the expected one;
  email/password endpoints do not exist; provider registration is rejected.

These run against the dev database and the mock IdP (compose service), with
unique users (`<test>-<random>@acme.test`) that are deleted afterwards.
Sign-ins in integration tests go through a small test helper that drives the
real flow over HTTP (`signIn.sso` → mock IdP login form POST with the given
claims → callback, with a cookie jar); hook logic is also unit-tested
directly.

**E2E** (Playwright, production build, mock IdP, dev config):

- Signed-out access to an app page redirects to `/sign-in`.
- An Acme user in group `agenty-admins` signs in, sees "Acme" and the members
  page; an Acme user without the group sees no members link and gets 404 on
  the page.
- A Globex user sees only Globex and only Globex members.
- Signing in with an `@acme.test` email at the Globex IdP is rejected with a
  readable error.
- Sign-out ends the session.
- An unknown email domain shows a readable error on `/sign-in`.

## Development, CI and test IdP

- `ghcr.io/navikt/mock-oauth2-server` (pinned tag) in `docker-compose.yml` on
  port 8080 and as a service in the CI `ci` job. It serves one issuer per path
  (`/acme`, `/globex`) and shows a login form where any username and claims
  (JSON) can be entered; Playwright fills that form. The mock only emits the
  claims entered, so tests always provide `email`, `name` and, where needed,
  `groups`. The first plan task confirms the image's defaults (interactive
  login, claims in the ID token).
- `config/agenty.dev.json` (committed) declares Acme (`acme.test`) and Globex
  (`globex.test`) against the mock; `.env.example` points `AUTH_CONFIG_FILE` to
  it and sets the two (dummy) client secrets.
- The Docker CI job starts the mock IdP too (not strictly needed, since discovery is
  lazy, but it keeps the configuration realistic), mounts the dev config into the
  container and sets its env, so the image starts with a valid configuration
  (its smoke test still only checks health). App container and mock IdP run
  with host networking, so the issuer `http://localhost:8080/...` is the same
  for both.
- Production runtime stays the app plus Postgres; the mock IdP is dev/test only.

## UI and routes

- `src/app/api/auth/[...all]/route.ts`: `toNextJsHandler(getAuth())`.
- `/sign-in`: email field, "Continue with SSO"; errors from the callback
  (unknown domain, rejected account) shown as readable messages, never raw.
- `/`: signed out → landing page (M0 heading stays) with a sign-in button;
  signed in → home placeholder ("Agents arrive in M2").
- Signed-in shell with a header: organization name, user menu (name, email,
  role, sign out).
- `/settings/members`: read-only member list (name, email, role, `last_sign_in_at`)
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
that removing an organization from the config disables it without deleting
data, that slugs and issuers are immutable (manual steps for an intentional IdP
change), that trusted origins are also accepted redirect targets, and that
IdP-side removals take effect within the 12-hour session lifetime.

## Risks (verified first in the plan)

Verified against the `@better-auth/sso` 1.7.7 source during the spec review:
`defaultSSO` is production-safe apart from its comment, works without the
organization plugin and with `providersLimit: 0`; `resolveUser` runs inside the
sign-in transaction and a returned `reject` persists nothing; default account
linking refuses unverified SSO emails. Still to prove in the first plan task,
before any UI work:

- The provisioning hand-over (`resolveUser` → `WeakMap` keyed by the endpoint
  context → `session.create.before`) and the member write through
  `getCurrentAdapter` in the same transaction (no fallback; see
  Provisioning).
- Lazy discovery filling the endpoints of a `defaultSSO` entry from a
  before-hook, so the plugin performs no discovery itself; and that
  `privateNetwork` (trusted endpoint origins) is enough for the loopback mock.
- The mock IdP's defaults (interactive login, custom claims in the ID token).
- **TypeScript 7 and Better Auth's inferred types:** if inference breaks, the
  few shapes we need are typed explicitly with Zod.
- **Cache Components:** pages that read the session are dynamic; they follow
  the Next 16 docs in `node_modules/next/dist/docs/`.
