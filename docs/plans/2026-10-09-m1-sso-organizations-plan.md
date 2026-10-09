# M1 – SSO and organizations: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** OIDC single sign-on as the only login, with providers stored in the database and organizations (= tenants) created just in time from an ID-token claim, plus the Postgres RLS machinery and isolation tests later domain tables build on.

**Architecture:** Better Auth (Drizzle adapter, transactions on) with `@better-auth/sso` reading providers from `auth.sso_provider`. A Better Auth `hooks.before` middleware enforces an endpoint allowlist and, on sign-in, selects and validates the provider and runs one-off discovery. `resolveUser` decides organization and role from the verified ID token; `session.create.before` writes organization and membership through Better Auth's transaction adapter. Domain tables in schema `app` use `tenantId()`/`tenantIsolation()`; `withTenant()` sets `app.tenant_id` per transaction from a `TenantContext` that only `src/server/auth` creates.

**Tech Stack:** Next.js 16.4 (App Router, Cache Components), TypeScript 7, Better Auth 1.7.7 + `@better-auth/sso` 1.7.7 + `@better-auth/core` 1.7.7, Drizzle ORM 0.45 / drizzle-kit 0.31 (postgres.js), Postgres 18, Zod 4, Vitest 5, Playwright 1.64, `ghcr.io/navikt/mock-oauth2-server:6.0.5` (dev/test only).

**Spec:** `docs/specs/2026-10-09-m1-sso-organizations-design.md`

## Global Constraints

- `better-auth`, `@better-auth/sso`, `@better-auth/core` pinned to exactly `1.7.7` (same version for all three).
- Mock IdP image `ghcr.io/navikt/mock-oauth2-server:6.0.5`, port 8080, always addressed as `http://localhost:8080` (its issuer follows the Host header; never mix with `127.0.0.1`).
- Every server module imports `"server-only"`, **except** modules plain Node or drizzle-kit load: `src/server/db/migrate.ts`, `src/server/db/auth-schema.ts`, `src/server/db/schema.ts`, `src/server/db/tenant-table.ts`, `src/server/db/seed.ts`. Those use relative imports with `.ts` extensions (no `@/` alias) so `node scripts/*.ts` can load them.
- Configuration only via `getEnv()`; owner URL only via `takeMigrationUrl()`.
- Zod at every boundary (env, provider rows, ID-token claims we read, request bodies we accept, page inputs).
- Errors and logs never contain secrets, tokens, connection strings or IdP-controlled text (`error_description`).
- RLS names: column `tenant_id`, setting `app.tenant_id`. Our APIs say `organizationId`.
- Sessions: absolute 12 h (`expiresIn: 43_200`), `disableSessionRefresh: true`, cookie cache off.
- Provider `oidc_config` accepted keys and values exactly as the spec's "Provider validation" row.
- Shell scripts POSIX `sh`, BSD/macOS compatible. Biome only. Tests named after the guarantee they protect; unit `*.test.ts`, integration `*.int.test.ts`.
- Commit messages end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never `git add -A` (agent worktrees live under `.claude/worktrees`); add paths explicitly.
- Before using a library API, check the installed package's types (`node_modules/…`) and docs (`node_modules/next/dist/docs/`). Research notes with Better Auth signatures and dist line references: `/tmp/claude-1000/-home-claude-git-agenty/78f36167-489a-4c82-a580-5cb61afe50e6/scratchpad/ba-api.md` and `…/scratchpad/infra-facts.md` (if gone, read the package `.d.mts`/`dist` files).
- `task ci` must pass before the final commit of every task that touches code (needs `task db:up`).

## Review Focus

1. **Email case:** an IdP returning `Alice@CORP.Test` must select `corp.test`, map to the same user as `alice@corp.test` and not trip the domain check → Task 2 (`selectProvider`, `decideSignIn` unit tests) and Task 3 (sign-in with mixed-case email reuses the user).
2. **Odd claims:** `org` as a number, array, empty string, upper-case or too long, and `groups` as a string, non-string array or object must never throw: invalid `org` → `organization_claim_missing`, odd `groups` → `member` → Task 2 `decideSignIn` tests.
3. **Process restart between redirect and callback:** the callback arrives at a fresh process; it must complete without re-discovery → Task 3 test (finish callback with a new `createAuth` instance).
4. **Seed during in-flight sign-ins:** integration test files run in parallel and seed in `beforeAll`; the seed must never modify an existing provider row (that would change the provider fingerprint and fail other files' callbacks) → Task 1 seed uses insert-if-absent, test asserts a second seed leaves `oidc_config` untouched.
5. **One IdP down:** while one provider's IdP is unreachable, users of another provider still sign in, and the broken one gets the readable `idp_unavailable` → Task 2 integration test.

---

## File structure

```
docker-compose.yml                      + service mock-oidc
scripts/seed.ts                         task db:seed (plain Node)
scripts/wait-for-url.sh                 wait for the mock IdP
scripts/auth-generate.config.ts         throw-away config for `task auth:generate`
src/server/env.ts                       + BETTER_AUTH_SECRET, BETTER_AUTH_URL, BETTER_AUTH_TRUSTED_ORIGINS
src/server/db/
  auth-schema.ts      Drizzle tables in pgSchema("auth") (no server-only)
  seed.ts             seedDevProviders(db) (no server-only)
  client.ts           createDb(url, opts) + getDb()
  tenant-table.ts     appRole, tenantIdSetting, tenantId(), tenantIsolation() (no server-only)
  tenant.ts           withTenant(ctx, fn, db?)
  migrations/0002…    CREATE SCHEMA auth / grants / tables
src/server/auth/
  providers.ts        provider row schema, email domain, provider selection (pure)
  claims.ts           decideSignIn, deriveRole (pure)
  provisioning.ts     hand-over WeakMap, membership write through the BA adapter
  auth.ts             createAuth()/getAuth(): Better Auth instance and hooks
  tenant-context.ts   TenantContext type + createTenantContext (auth-internal)
  tenant.ts           getTenantContext(), errors; re-exports the TenantContext type
  members.ts          listMembers(ctx), getOrganizationSlug(ctx)
src/app/api/auth/[...all]/route.ts
src/app/sign-in/…, src/app/settings/members/page.tsx, src/app/page.tsx
src/components/app-header.tsx, src/components/user-menu.tsx
src/lib/auth-client.ts
src/proxy.ts
tests/support/sso.ts                    HTTP sign-in helper driving the mock IdP
tests/e2e/auth.spec.ts, tests/e2e/support/mock-idp.ts
```

---

### Task 1: Dependencies, env, schema `auth`, mock IdP and seed

**Files:**
- Modify: `package.json`, `pnpm-lock.yaml`, `pnpm-workspace.yaml` (only if pnpm asks), `src/server/env.ts`, `src/server/env.test.ts`, `.env.example`, `drizzle.config.ts`, `src/server/db/client.ts`, `src/server/db/roles.int.test.ts`, `docker-compose.yml`, `.github/workflows/ci.yml` (job `ci`), `Taskfile.yml`
- Create: `src/server/db/auth-schema.ts`, migrations `0002_*`–`0004_*`, `src/server/db/seed.ts`, `src/server/db/seed.int.test.ts`, `scripts/seed.ts`, `scripts/wait-for-url.sh`, `scripts/auth-generate.config.ts`

**Interfaces:**
- Produces:
  ```ts
  // env.ts: Env gains
  BETTER_AUTH_SECRET: string; BETTER_AUTH_URL: string; BETTER_AUTH_TRUSTED_ORIGINS: string[]; // [] when unset
  // auth-schema.ts: exported tables keyed by Better Auth model name
  user, session, account, verification, ssoProvider, organization, member; authSchema = pgSchema("auth")
  // client.ts
  export function createDb(url: string, options?: { max?: number }): Db; export type Db; export function getDb(): Db;
  // seed.ts
  export const DEV_PROVIDERS: readonly { providerId: "corp" | "partner"; issuer: string; domain: string }[];
  export async function seedDevProviders(db: PostgresJsDatabase<…>): Promise<void>; // insert-if-absent
  ```

- [ ] **Step 1: Install** — `pnpm add better-auth@1.7.7 @better-auth/sso@1.7.7 @better-auth/core@1.7.7`. Pin exact versions in `package.json` (no `^`). If pnpm reports blocked build scripts, allow only what is needed in `pnpm-workspace.yaml` and say why in the commit. Run `pnpm exec tsc --noEmit`; if Better Auth's types fail under TypeScript 7, stop and report DONE_WITH_CONCERNS with the errors.

- [ ] **Step 2: Env (test first)** — add to `src/server/env.test.ts` (update existing fixtures with the new required keys):

```ts
const base = {
  DATABASE_URL: "postgres://a:b@localhost:5432/agenty",
  BETTER_AUTH_SECRET: "x".repeat(32),
  BETTER_AUTH_URL: "http://localhost:3000",
};

it("accepts the auth settings, trusted origins default to none", () => {
  expect(parseEnv(base)).toMatchObject({ BETTER_AUTH_TRUSTED_ORIGINS: [] });
});

it("parses comma-separated trusted origins", () => {
  expect(parseEnv({ ...base, BETTER_AUTH_TRUSTED_ORIGINS: "http://localhost:8080, https://idp.internal" }))
    .toMatchObject({ BETTER_AUTH_TRUSTED_ORIGINS: ["http://localhost:8080", "https://idp.internal"] });
});

it("rejects a trusted origin with a path", () => {
  expect(() => parseEnv({ ...base, BETTER_AUTH_TRUSTED_ORIGINS: "http://localhost:8080/corp" }))
    .toThrow(/BETTER_AUTH_TRUSTED_ORIGINS/);
});

it("rejects a short BETTER_AUTH_SECRET without echoing it", () => {
  const secret = "short-secret-value";
  expect(() => parseEnv({ ...base, BETTER_AUTH_SECRET: secret })).toThrow(/BETTER_AUTH_SECRET/);
  expect(() => parseEnv({ ...base, BETTER_AUTH_SECRET: secret })).not.toThrow(new RegExp(secret));
});

it("rejects a non-http BETTER_AUTH_URL", () => {
  expect(() => parseEnv({ ...base, BETTER_AUTH_URL: "ftp://x" })).toThrow(/BETTER_AUTH_URL/);
});
```
Implement in `envSchema`:

```ts
BETTER_AUTH_SECRET: z.string().min(32, "must be at least 32 characters"),
BETTER_AUTH_URL: z.url({ protocol: /^https?$/, error: "must be an http(s) URL" }),
BETTER_AUTH_TRUSTED_ORIGINS: z
  .string()
  .optional()
  .transform((value) => (value ?? "").split(",").map((o) => o.trim()).filter(Boolean))
  .pipe(z.array(z.url({ protocol: /^https?$/ }).refine((o) => new URL(o).origin === o, "must be an origin (scheme://host[:port])"))),
```
(Adjust to the installed Zod 4 API without changing behaviour.) `.env.example` appends:

```sh
# Better Auth. Development values only; generate a real secret with `openssl rand -base64 32`.
BETTER_AUTH_SECRET=dev-only-secret-change-me-0123456789abcdef
BETTER_AUTH_URL=http://localhost:3000
# Origins (comma-separated) of identity providers on private or loopback hosts; the dev mock IdP.
BETTER_AUTH_TRUSTED_ORIGINS=http://localhost:8080
```
Add the same keys to your local `.env`.

- [ ] **Step 3: Reference schema** — `scripts/auth-generate.config.ts`: `export const auth = betterAuth({ database: drizzleAdapter(drizzle(postgres("postgres://x@localhost/x")), { provider: "pg", schemaName: "auth", transaction: true }), advanced: { database: { generateId: "uuid" } }, plugins: [sso()] })` using `drizzle-orm/postgres-js` (the repo has no `pg`), with a dummy `secret`. Taskfile:

```yaml
  auth:generate:
    desc: Write Better Auth's reference schema to build/ for diffing after upgrades
    cmds:
      - mkdir -p build
      - pnpm dlx auth@1.7.7 generate --config scripts/auth-generate.config.ts --output build/auth-schema.generated.ts -y
```
Run it (a "Drizzle schema mismatch" log line is expected).

- [ ] **Step 4: `auth-schema.ts`** — from the generated file take `user`, `session`, `account`, `verification`, `ssoProvider`; keep column names, indexes and `pg_catalog.gen_random_uuid()` defaults; all timestamps `timestamp(name, { withTimezone: true })`; drop `relations()`. Changes:
  - `ssoProvider.userId`: nullable (keep the FK, `on delete cascade`).
  - `ssoProvider` gains `organizationClaim: text("organization_claim").notNull()`, `roleClaim: text("role_claim")`, `adminValues: text("admin_values")` (comma-separated).
  - Add our tables:
    ```ts
    export const organization = authSchema.table("organization", {
      id: uuid("id").default(sql`pg_catalog.gen_random_uuid()`).primaryKey(),
      slug: text("slug").notNull().unique(),
      providerId: text("provider_id").references(() => ssoProvider.providerId, { onDelete: "set null" }),
      createdAt: timestamp("created_at", { withTimezone: true }).defaultNow().notNull(),
      updatedAt: timestamp("updated_at", { withTimezone: true }).defaultNow().notNull(),
    });

    export const member = authSchema.table("member", {
      id: uuid("id").default(sql`pg_catalog.gen_random_uuid()`).primaryKey(),
      userId: uuid("user_id").notNull().unique().references(() => user.id, { onDelete: "cascade" }),
      organizationId: uuid("organization_id").notNull().references(() => organization.id, { onDelete: "cascade" }),
      role: text("role", { enum: ["admin", "member"] }).notNull(),
      createdAt: timestamp("created_at", { withTimezone: true }).defaultNow().notNull(),
      updatedAt: timestamp("updated_at", { withTimezone: true }).defaultNow().notNull(),
    }, (t) => [index("member_organization_id_idx").on(t.organizationId), check("member_role_check", sql`${t.role} in ('admin', 'member')`)]);
    ```
  - Imports are package imports only (no `@/`), and no `server-only`.

- [ ] **Step 5: Migrations in three steps** (drizzle-kit emits `CREATE SCHEMA`; default privileges must exist before the tables):
  1. `drizzle.config.ts`: `schema: ["./src/server/db/schema.ts", "./src/server/db/auth-schema.ts"]`, `schemaFilter: ["app", "auth"]`. Temporarily reduce `auth-schema.ts` to `export const authSchema = pgSchema("auth");` and run `task db:generate -- --name auth_schema` → `CREATE SCHEMA "auth";`.
  2. `task db:generate -- --custom --name auth_grants`, mirroring `0001_app_grants.sql`:
     ```sql
     GRANT USAGE ON SCHEMA "auth" TO agenty_app;
     --> statement-breakpoint
     ALTER DEFAULT PRIVILEGES FOR ROLE agenty_owner IN SCHEMA "auth"
       GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO agenty_app;
     --> statement-breakpoint
     ALTER DEFAULT PRIVILEGES FOR ROLE agenty_owner IN SCHEMA "auth"
       GRANT USAGE, SELECT ON SEQUENCES TO agenty_app;
     ```
  3. Restore the full file; `task db:generate -- --name auth_tables`. Review the SQL (schema `auth`, uuid ids, FKs incl. `on delete set null`, unique `member.user_id`, the check). `task db:migrate`.

- [ ] **Step 6: Client** — `src/server/db/client.ts`:

```ts
import "server-only";
import { drizzle } from "drizzle-orm/postgres-js";
import postgres from "postgres";
import { getEnv } from "@/server/env";
import * as authSchema from "./auth-schema";
import * as appSchema from "./schema";

const schema = { ...appSchema, ...authSchema };

/** A Drizzle instance on its own connection pool (role taken from the URL). */
export function createDb(url: string, options: { max?: number } = {}) {
  const client = postgres(url, { max: options.max ?? 10, onnotice: () => {} });
  return drizzle({ client, schema });
}

export type Db = ReturnType<typeof createDb>;

let db: Db | undefined;

/** The shared connection pool, as role agenty_app. Created on first use. */
export function getDb(): Db {
  db ??= createDb(getEnv().DATABASE_URL);
  return db;
}
```

- [ ] **Step 7: Role guarantees** — in `roles.int.test.ts`, run the "schema ownership" block for both schemas (`describe.each(["app", "auth"])`): owned by `agenty_owner`; every table owned by `agenty_owner`; the rolled-back grant probe (probe table in that schema) yields SELECT/INSERT/UPDATE/DELETE true, TRUNCATE false; the app role cannot `CREATE` there. Add `has_schema_privilege(current_user, <schema>, 'USAGE')` true for both, and a rolled-back sequence probe (`create sequence <schema>.__seq_probe`; `has_sequence_privilege('agenty_app', …, 'USAGE')` and `'SELECT'` true).

- [ ] **Step 8: Mock IdP** — `docker-compose.yml`:

```yaml
  mock-oidc:
    image: ghcr.io/navikt/mock-oauth2-server:6.0.5
    ports:
      - "127.0.0.1:8080:8080"
```
`scripts/wait-for-url.sh` (POSIX sh): `url=$1`; up to 60 tries of `curl -fsS "$url" >/dev/null 2>&1` with `sleep 1`; on timeout print `$url not reachable` to stderr and exit 1. Taskfile:

```yaml
  db:up:
    desc: Start local Postgres and the mock IdP and wait until both are ready
    cmds:
      - docker compose up --detach --wait postgres
      - docker compose up --detach mock-oidc
      - sh scripts/wait-for-url.sh http://localhost:8080/isalive
```
Update `db:down`/`db:reset` descriptions. CI job `ci`: service `mock-oidc` (`image: ghcr.io/navikt/mock-oauth2-server:6.0.5`, `ports: ["8080:8080"]`, no health options: the image has no curl) and a step `sh scripts/wait-for-url.sh http://localhost:8080/isalive` before `task setup`.

- [ ] **Step 9: Seed (test first)** — `src/server/db/seed.int.test.ts` (owner-free: `createDb(DATABASE_URL)` would import server-only code; build the Drizzle instance in the test with `drizzle({ client: postgres(DATABASE_URL, { max: 1 }), schema: authSchema })`):
  - after `seedDevProviders`, rows `corp` and `partner` exist with the spec's values (`organization_claim` `org`, `role_claim` `groups`, `admin_values` `agenty-admins`, `oidc_config` parses to `{ clientId: "agenty", clientSecret: "dev-secret", pkce: true, scopes: ["openid","email","profile"] }`, `user_id` null);
  - a second call is a no-op: it does not touch an existing row (update `corp`'s `oidc_config` in the test to add a marker key, seed again, the marker is still there; restore afterwards).

  `src/server/db/seed.ts`:

```ts
import type { PostgresJsDatabase } from "drizzle-orm/postgres-js";
import * as authSchema from "./auth-schema.ts";

const oidcConfig = JSON.stringify({
  clientId: "agenty",
  clientSecret: "dev-secret",
  pkce: true,
  scopes: ["openid", "email", "profile"],
});

export const DEV_PROVIDERS = [
  { providerId: "corp", issuer: "http://localhost:8080/corp", domain: "corp.test" },
  { providerId: "partner", issuer: "http://localhost:8080/partner", domain: "partner.test" },
] as const;

/**
 * Registers the mock IdP's providers. Never modifies an existing row: Better Auth binds a
 * fingerprint of the provider row into each sign-in, so changing it would fail sign-ins in flight.
 */
export async function seedDevProviders(db: PostgresJsDatabase<typeof authSchema>): Promise<void> {
  await db
    .insert(authSchema.ssoProvider)
    .values(DEV_PROVIDERS.map((p) => ({
      ...p, oidcConfig, organizationClaim: "org", roleClaim: "groups", adminValues: "agenty-admins",
    })))
    .onConflictDoNothing({ target: authSchema.ssoProvider.providerId });
}
```
`scripts/seed.ts` (plain Node, run as `node scripts/seed.ts`): reads `DATABASE_URL` from `process.env` (Taskfile loads `.env`), creates a `max: 1` postgres.js client, calls `seedDevProviders`, ends the client, prints `Seeded development identity providers (corp, partner).`; on error prints the message only and exits 1. Taskfile: `db:seed` ("Register the mock IdP's development providers (insert-if-absent)"); `dev` runs `task: db:seed` after `db:up`; `ci` runs `task: db:seed` after `db:migrate`. If the relative `.ts` import chain does not load in plain Node (tsconfig `allowImportingTsExtensions` is already on), fix it there, not with a bundler.

- [ ] **Step 10: Verify and commit** — `task db:up`, `task ci` → green. `git commit -m "feat(db): schema auth, Better Auth tables, mock IdP and dev seed"` (explicit paths).

---

### Task 2: Better Auth instance, sign-in hook, provider validation and discovery

This and Task 3 prove the spec's risky mechanisms before anything builds on them. If one cannot be made to work as specified, stop and report BLOCKED with findings; do not invent a fallback.

**Files:**
- Create: `src/server/auth/providers.ts`, `src/server/auth/providers.test.ts`, `src/server/auth/claims.ts`, `src/server/auth/claims.test.ts`, `src/server/auth/auth.ts`, `src/server/auth/auth.int.test.ts`, `src/app/api/auth/[...all]/route.ts`, `tests/support/sso.ts`
- Modify: `biome.json`

**Interfaces:**
- Consumes: Task 1 (`getDb`, `createDb`, `Db`, auth tables, `seedDevProviders`, env).
- Produces:
  ```ts
  // providers.ts
  export type Role = "admin" | "member";
  export type ProviderRow = typeof ssoProvider.$inferSelect;
  export type ValidProvider = { providerId: string; issuer: string; domains: string[]; organizationClaim: string;
    roleClaim?: string; adminValues: string[]; oidc: OidcConfig; hasEndpoints: boolean };
  export function parseProvider(row: ProviderRow): { ok: true; provider: ValidProvider } | { ok: false; fields: string[] };
  export function emailDomain(email: string): string | undefined;          // requires exactly one "@", lower-cased
  export function domainMatches(emailDomainValue: string, domain: string): boolean;
  export function selectProvider<T extends { domain: string; providerId: string }>(rows: T[], email: string): T | undefined;
  // claims.ts
  export type SignInDecision = { organizationSlug: string; role: Role };
  export type RejectCode = "email_domain_mismatch" | "organization_claim_missing";
  export function deriveRole(claims: Record<string, unknown>, roleClaim: string | undefined, adminValues: string[]): Role;
  export function decideSignIn(input: { provider: ValidProvider; email: string; claims: Record<string, unknown> }):
    { ok: true; decision: SignInDecision } | { ok: false; code: RejectCode };
  // auth.ts
  export const ALLOWED_ENDPOINTS: ReadonlySet<string>;  // "/sign-in/sso", "/sso/callback/:providerId", "/get-session", "/sign-out"
  export function createAuth(deps?: { db?: Db; baseURL?: string; secret?: string; trustedOrigins?: string[]; resolveSignIn?: ResolveSignIn }): Auth; // resolveSignIn: Task 3
  export function getAuth(): Auth;                      // memoised createAuth() from getEnv()
  export type Auth;                                     // ReturnType of betterAuth(...)
  // tests/support/sso.ts
  export async function signInViaMock(auth: Auth, opts: { baseURL: string; email: string; name?: string;
    claims?: Record<string, unknown>; idpEmail?: string; stopAfterIdp?: boolean }):
    Promise<{ status: number; location: string; cookie: string; callbackUrl?: string }>;
  ```

- [ ] **Step 1: Pure provider tests** (`providers.test.ts`):

```ts
const row = (over: Partial<ProviderRow> = {}): ProviderRow => ({
  id: "00000000-0000-0000-0000-000000000001", providerId: "corp", issuer: "http://localhost:8080/corp",
  domain: "corp.test", userId: null, organizationId: null, samlConfig: null,
  oidcConfig: JSON.stringify({ clientId: "agenty", clientSecret: "dev-secret", pkce: true, scopes: ["openid", "email", "profile"] }),
  organizationClaim: "org", roleClaim: "groups", adminValues: "agenty-admins", ...over,
}) as ProviderRow;
const oidc = (o: object) => JSON.stringify({ clientId: "agenty", clientSecret: "dev-secret", pkce: true, scopes: ["openid", "email", "profile"], ...o });

it("accepts a complete row", () => {
  expect(parseProvider(row())).toMatchObject({ ok: true, provider: { domains: ["corp.test"], adminValues: ["agenty-admins"], hasEndpoints: false } });
});

it("recognises discovered endpoints", () => {
  const r = row({ oidcConfig: oidc({ authorizationEndpoint: "http://localhost:8080/corp/authorize", tokenEndpoint: "http://localhost:8080/corp/token", jwksEndpoint: "http://localhost:8080/corp/jwks" }) });
  expect(parseProvider(r)).toMatchObject({ ok: true, provider: { hasEndpoints: true } });
});

it.each([
  ["missing pkce", { oidcConfig: oidc({ pkce: undefined }) }, "oidcConfig.pkce"],
  ["pkce false", { oidcConfig: oidc({ pkce: false }) }, "oidcConfig.pkce"],
  ["offline_access", { oidcConfig: oidc({ scopes: ["openid", "email", "profile", "offline_access"] }) }, "oidcConfig.scopes"],
  ["userInfoEndpoint", { oidcConfig: oidc({ userInfoEndpoint: "http://x/userinfo" }) }, "oidcConfig"],
  ["allowIdpInitiated", { oidcConfig: oidc({ allowIdpInitiated: true }) }, "oidcConfig"],
  ["mapping", { oidcConfig: oidc({ mapping: { email: "upn" } }) }, "oidcConfig"],
  ["overrideUserInfo", { oidcConfig: oidc({ overrideUserInfo: true }) }, "oidcConfig"],
  ["private_key_jwt", { oidcConfig: oidc({ tokenEndpointAuthentication: "private_key_jwt" }) }, "oidcConfig"],
  ["no client secret", { oidcConfig: oidc({ clientSecret: "" }) }, "oidcConfig.clientSecret"],
  ["invalid JSON", { oidcConfig: "{" }, "oidcConfig"],
  ["no oidc config", { oidcConfig: null }, "oidcConfig"],
  ["saml config", { samlConfig: "{}" }, "samlConfig"],
  ["plugin organization", { organizationId: "x" }, "organizationId"],
  ["no organization claim", { organizationClaim: "" }, "organizationClaim"],
  ["upper-case domain", { domain: "CORP.test" }, "domain"],
  ["empty domain entry", { domain: "corp.test," }, "domain"],
  ["issuer not http(s)", { issuer: "ftp://x" }, "issuer"],
] as const)("rejects %s, naming the field", (_name, over, field) => {
  const result = parseProvider(row(over as Partial<ProviderRow>));
  expect(result.ok).toBe(false);
  if (!result.ok) expect(result.fields.join(" ")).toContain(field);
});

it("never puts the client secret into the result's fields", () => {
  const result = parseProvider(row({ oidcConfig: oidc({ clientSecret: "super-secret", pkce: false }) }));
  expect(JSON.stringify(result)).not.toContain("super-secret");
});

describe("provider selection", () => {
  const rows = [{ providerId: "corp", domain: "corp.test" }, { providerId: "partner", domain: "partner.test,eu.partner.test" }];
  it("requires exactly one @", () => {
    expect(emailDomain("a@b@corp.test")).toBeUndefined();
    expect(emailDomain("no-at")).toBeUndefined();
    expect(emailDomain("Alice@CORP.Test")).toBe("corp.test");
  });
  it("matches case-insensitively, exact before subdomain", () => {
    expect(selectProvider(rows, "Alice@CORP.Test")?.providerId).toBe("corp");
    expect(selectProvider(rows, "x@eu.partner.test")?.providerId).toBe("partner");
    expect(selectProvider(rows, "x@dev.corp.test")?.providerId).toBe("corp");
    expect(selectProvider(rows, "x@notcorp.test")).toBeUndefined();
  });
});
```

- [ ] **Step 2: Implement `providers.ts`** (server-only): a strict Zod schema for the parsed `oidcConfig`:

```ts
const oidcConfigSchema = z.strictObject({
  clientId: z.string().min(1),
  clientSecret: z.string().min(1),
  pkce: z.literal(true),
  scopes: z.tuple([z.literal("openid"), z.literal("email"), z.literal("profile")]),
  discoveryEndpoint: z.url({ protocol: /^https?$/ }).optional(),
  authorizationEndpoint: z.url({ protocol: /^https?$/ }).optional(),
  tokenEndpoint: z.url({ protocol: /^https?$/ }).optional(),
  jwksEndpoint: z.url({ protocol: /^https?$/ }).optional(),
});
```
(If Better Auth itself stores extra keys in rows it writes — e.g. after the plugin's own runtime discovery, which we prevent — extend the allowlist only with keys that are provably harmless, and record why.) `parseProvider` checks the row fields (`issuer` http(s) URL; `domain` = comma-separated, each lower-case domain name matching `/^(?=.{1,253}$)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z][a-z0-9-]{0,62}$/`; `samlConfig` null; `organizationId` null; `organizationClaim` non-empty claim name; optional `roleClaim`; `adminValues` split by comma, trimmed, non-empty entries) and returns field paths of all failures (`issue.path` joined with `.`, prefixed with `oidcConfig.` for config issues), never values. `hasEndpoints` = all three endpoint keys present. `emailDomain`: exactly one `@`, non-empty local part and domain, lower-cased. `domainMatches`: `value === domain || value.endsWith("." + domain)`. `selectProvider`: first an exact match over all rows (sorted by `providerId` for determinism), then a subdomain match.

- [ ] **Step 3: Claims tests** (`claims.test.ts`), with a `ValidProvider` fixture (domains `["corp.test"]`, `organizationClaim: "org"`, `roleClaim: "groups"`, `adminValues: ["agenty-admins"]`):
  - `{ org: "acme" }` → `{ organizationSlug: "acme", role: "member" }`; with `groups: ["agenty-admins"]` → `admin`; `groups: "agenty-admins"` → `admin`.
  - `org` missing, `""`, `"Acme"`, `"a"` (too short), 64+ chars, `"a_b"`, `42`, `["acme"]`, `null` → `organization_claim_missing`.
  - `groups` as `[1, null, {}]`, `{ "agenty-admins": true }`, `"x"` → `member`; no `roleClaim` → `member` even with matching groups.
  - email `bob@partner.test` for the corp provider → `email_domain_mismatch`; `Bob@Dev.Corp.Test` → ok.

- [ ] **Step 4: Implement `claims.ts`** (server-only, pure):

```ts
const slugSchema = z.string().regex(/^[a-z0-9][a-z0-9-]{1,62}$/);

export function deriveRole(claims: Record<string, unknown>, roleClaim: string | undefined, adminValues: string[]): Role {
  if (!roleClaim) return "member";
  const value = claims[roleClaim];
  const values = typeof value === "string" ? [value] : Array.isArray(value) ? value : [];
  return values.some((v) => typeof v === "string" && adminValues.includes(v)) ? "admin" : "member";
}

export function decideSignIn({ provider, email, claims }: { provider: ValidProvider; email: string; claims: Record<string, unknown> }) {
  const domain = emailDomain(email);
  if (!domain || !provider.domains.some((d) => domainMatches(domain, d))) return { ok: false, code: "email_domain_mismatch" } as const;
  const slug = slugSchema.safeParse(claims[provider.organizationClaim]);
  if (!slug.success) return { ok: false, code: "organization_claim_missing" } as const;
  return { ok: true, decision: { organizationSlug: slug.data, role: deriveRole(claims, provider.roleClaim, provider.adminValues) } } as const;
}
```

- [ ] **Step 5: `auth.ts`** — build the instance (verify every option name against the installed types; see `ba-api.md` §1–§9):
  - `database: drizzleAdapter(db, { provider: "pg", schema: { user, session, account, verification, ssoProvider, organization, member }, transaction: true })`.
  - `secret`, `baseURL` from `getEnv()` unless given in `deps`; `basePath: "/api/auth"`; `trustedOrigins: [baseURL, ...BETTER_AUTH_TRUSTED_ORIGINS]`.
  - `advanced: { database: { generateId: "uuid" }, useSecureCookies: baseURL.startsWith("https://") }`.
  - `session: { expiresIn: 43_200, disableSessionRefresh: true, cookieCache: { enabled: false } }`.
  - `account: { encryptOAuthTokens: true, accountLinking: { disableImplicitLinking: true } }`; `rateLimit: { enabled: false }`; `onAPIError: { errorURL: "/sign-in" }`. No `emailAndPassword`, no `socialProviders`.
  - `plugins`:
    1. `sso({ providersLimit: 0, organizationProvisioning: { disabled: true }, schema: { ssoProvider: { additionalFields: { organizationClaim: { type: "string", required: true, fieldName: "organizationClaim" }, roleClaim: { type: "string", required: false }, adminValues: { type: "string", required: false } } } } })` (check the exact option shape for plugin `schema`/`additionalFields` in `@better-auth/sso`'s types; `domainVerification` stays unset). Leave `resolveUser` as a placeholder returning `{ action: "reject", code: "not_ready" }` until Task 3.
    2. A local plugin declaring our tables so Better Auth's schema check and its adapter accept them: `{ id: "agenty-organizations", schema: { organization: { fields: { slug: { type: "string", required: true, unique: true }, providerId: { type: "string", required: false, references: { model: "ssoProvider", field: "providerId", onDelete: "set null" } }, createdAt: { type: "date", required: true }, updatedAt: { type: "date", required: true } } }, member: { fields: { userId: { type: "string", required: true, unique: true, references: { model: "user", field: "id", onDelete: "cascade" } }, organizationId: { type: "string", required: true, references: { model: "organization", field: "id", onDelete: "cascade" } }, role: { type: "string", required: true }, createdAt: { type: "date", required: true }, updatedAt: { type: "date", required: true } } } } } satisfies BetterAuthPlugin` (every NOT NULL column without default must be declared, otherwise Better Auth's per-request schema diff throws). Type it with `BetterAuthPlugin` from `better-auth`.
  - `hooks.before: createAuthMiddleware(async (ctx) => { … })` (`createAuthMiddleware`, `APIError` from `better-auth/api`; `ctx.path` is the route pattern, verified):
    1. `!ALLOWED_ENDPOINTS.has(ctx.path)` → `throw new APIError("NOT_FOUND")`.
    2. `/sign-in/sso`:
       - body keys ⊆ `{email, callbackURL, errorCallbackURL}`, `callbackURL`/`errorCallbackURL` (if present) match `^/(?!/)` → else `APIError("BAD_REQUEST", { code: "invalid_request" })`; `email` must yield an `emailDomain`, else `invalid_request`.
       - load all provider rows (`db.select().from(ssoProvider)`), `selectProvider(rows, email)`; none → `APIError("BAD_REQUEST", { code: "unknown_domain" })`.
       - `parseProvider(row)`; invalid → log `Identity provider "<id>" is misconfigured: <fields>` and `APIError("SERVICE_UNAVAILABLE", { code: "idp_unavailable" })`.
       - if `!hasEndpoints`: `discoverOIDCConfig` (exported from `@better-auth/sso`; check its signature) with the row's issuer and optional `discoveryEndpoint`, `isTrustedOrigin` = `ctx.context.isTrustedOrigin`; on success write `{ ...oidc, authorizationEndpoint, tokenEndpoint, jwksEndpoint }` (never `userInfoEndpoint`) to the row with `update … where provider_id = … and oidc_config = <old value>` (no clobbering a concurrent write); any error → log the error class/code (not the IdP's text) and `idp_unavailable`.
       - inject the provider: `return { context: { body: { ...ctx.body, providerId: row.providerId } } }` (verify the before-hook context-merge shape in Better Auth's types; the injected key must not be re-checked against the client allowlist).
    3. `/sso/callback/:providerId`: `ctx.params.providerId` must name an existing row, else `APIError("NOT_FOUND")` (verify `ctx.params` is populated in before-hooks; if not, parse it from `ctx.request.url`). No discovery here.
  - `export function getAuth()` memoises `createAuth()`.

- [ ] **Step 6: Route handler** — `src/app/api/auth/[...all]/route.ts`:

```ts
import { toNextJsHandler } from "better-auth/next-js";
import { connection } from "next/server";
import { getAuth } from "@/server/auth/auth";

export async function GET(request: Request) {
  await connection();
  return toNextJsHandler(getAuth()).GET(request);
}

export async function POST(request: Request) {
  await connection();
  return toNextJsHandler(getAuth()).POST(request);
}
```

- [ ] **Step 7: Biome restriction** — override for all files except `src/server/auth/**`, `src/server/db/**`, `scripts/**`, `tests/**` and `**/*.test.ts`: `noRestrictedImports` (check group/name for Biome 2.5) on `@/server/db/auth-schema` ("Only src/server/auth may use the auth tables"). Second override for all files except `src/server/auth/**` and tests: restrict `@/server/auth/tenant-context` ("Use getTenantContext(); import the TenantContext type from @/server/auth/tenant"). Prove each with a scratch import that makes `task lint` fail; remove it.

- [ ] **Step 8: HTTP helper** `tests/support/sso.ts` (mock form facts: `infra-facts.md` §1):
  1. `auth.handler(new Request(`${baseURL}/api/auth/sign-in/sso`, { method: "POST", headers: { "content-type": "application/json", origin: baseURL }, body: JSON.stringify({ email, callbackURL: "/", errorCallbackURL: "/sign-in" }) }))`; non-200 → return `{ status, location: "", cookie: "" }` with the JSON code available to the caller (return the parsed body too). Read `{ url }` and keep `set-cookie` (state) in a small jar (`response.headers.getSetCookie()`).
  2. `fetch(url, { method: "POST", body: new URLSearchParams({ username: idpEmail ?? email, claims: JSON.stringify({ email: idpEmail ?? email, name: name ?? "Test User", email_verified: true, ...claims }) }), redirect: "manual" })` → 302; `location` = the app callback URL. If `stopAfterIdp`, return it as `callbackUrl` without calling the callback.
  3. `auth.handler(new Request(location, { headers: { cookie: jar } }))` → 302; return its `location` and the jar's `Cookie` header including the session cookie.
  Also export `finishCallback(auth, callbackUrl, cookie)` for the restart test.

- [ ] **Step 9: Integration tests** (`auth.int.test.ts`; `beforeAll`: `seedDevProviders`; instance `createAuth({ baseURL: "http://localhost:3000" })`; a dedicated provider row per test that needs to break or change rows, with a random `provider_id`/domain, deleted in `afterAll` — never modify `corp`/`partner`):
  - **endpoints**: for every `Object.values(auth.api)` with a `path`, a request with the endpoint's own method (`e.options.method`, first if array; params replaced by `x`): allowed → not 404, others → 404 (`expect.soft` per path).
  - **wiring**: `auth.options.plugins.map(p => p.id).sort()` equals `["agenty-organizations", "sso"]`; `domainVerification` not configured.
  - **body allowlist**: `scopes`, `additionalParams`, `providerId`, `organizationSlug`, `requestSignUp` → 400 `invalid_request`; `callbackURL: "https://evil.test/"` and `"//evil.test"` → 400.
  - **unknown domain** → 400 `unknown_domain`; `a@b@corp.test` → 400 `invalid_request`.
  - **authorize redirect**: `alice@corp.test` → `{ url }` starts with `http://localhost:8080/corp/authorize`, has `code_challenge`, `scope=openid email profile` (no `offline_access`), `redirect_uri` ends `/api/auth/sso/callback/corp`.
  - **discovery write**: a fresh provider row (issuer `http://localhost:8080/<random>`, domain `<random>.test`) has no endpoints; after one sign-in request its `oidc_config` has the three endpoint keys and no `userInfoEndpoint`; a second sign-in request leaves the row byte-identical.
  - **misconfigured row** (`pkce` missing) → 503 `idp_unavailable`; the log line names `oidcConfig.pkce` and not the secret (spy on `console.error`/the logger used).
  - **unreachable IdP** (issuer `http://localhost:1/x`, its origin added to the instance's `trustedOrigins` via deps) → 503 `idp_unavailable`; `alice@corp.test` on the same instance still gets an authorize URL (Review Focus 5).
  - **callback for unknown provider** → 404.

- [ ] **Step 10: `task ci`; commit** — `git commit -m "feat(auth): Better Auth with SSO, provider validation and one-off discovery"`.

---

### Task 3: Provisioning, account binding, hand-over

**Files:**
- Create: `src/server/auth/provisioning.ts`, `src/server/auth/provisioning.test.ts`, `src/server/auth/sign-in.int.test.ts`
- Modify: `src/server/auth/auth.ts`

**Interfaces:**
- Consumes: Task 2 (`parseProvider`, `decideSignIn`, `createAuth`, helper).
- Produces:
  ```ts
  export type ProvisioningDecision = SignInDecision & { providerId: string };
  export function rememberDecision(endpointContext: object, decision: ProvisioningDecision): void;
  export function takeDecision(endpointContext: object | null | undefined): ProvisioningDecision | undefined;
  export async function provisionMembership(adapter: DBAdapter, userId: string, decision: ProvisioningDecision): Promise<void>; // throws APIError with code
  ```

- [ ] **Step 1: Unit tests** (`provisioning.test.ts`) for the hand-over: remembered decision is returned once, then `undefined`; `takeDecision(null)` → `undefined`; different context objects don't see each other's decisions. And for `provisionMembership` with an in-memory fake adapter (`findOne`/`create`/`update` recording calls): creates organization (slug, providerId) and member (role) when absent; updates only `role`/`updatedAt` for an existing member of the same organization; throws `APIError` code `organization_owned_by_other_provider` when the slug's organization has another `providerId` (including `null`, i.e. orphaned); throws `organization_changed` when the member row points to another organization.

- [ ] **Step 2: Implement `provisioning.ts`** (server-only): the `WeakMap` hand-over as in the interface; `provisionMembership` uses `adapter.findOne({ model: "organization", where: [{ field: "slug", value }] })`, `adapter.create({ model: "organization", data: { slug, providerId, createdAt, updatedAt } })`, `adapter.findOne({ model: "member", where: [{ field: "userId", value: userId }] })`, `adapter.create`/`adapter.update` for the member. Map a Postgres unique violation (`code === "23505"`, on the error or its `cause`) to `APIError("CONFLICT", { code: "try_again" })`. Every other failure → `APIError("FORBIDDEN"|"INTERNAL_SERVER_ERROR", { code: "provisioning_failed" })`, logging the error class only.

- [ ] **Step 3: Wire the hooks in `auth.ts`**:
  - `sso({ …, resolveUser: async (input, { database }) => { … } })` — never throws:
    1. Load the provider row by `input.providerId` (through `database`, model `ssoProvider`) and `parseProvider` it; invalid → `{ action: "reject", code: "idp_unavailable" }`.
    2. `decideSignIn({ provider, email: input.providerUser.email, claims: input.verifiedIdTokenClaims })`; failure → `{ action: "reject", code }`.
    3. Binding: find the user by lower-cased email (`database.findOne({ model: "user", where: [{ field: "email", value }] })`); if found, `database.findMany({ model: "account", where: [{ field: "userId", value: user.id }] })`; any account whose `providerId !== input.providerId` → `{ action: "reject", code: "account_bound_to_other_provider" }`. Check how Better Auth stores emails (lower-cased?) and match it.
    4. `rememberDecision(getCurrentAuthEndpointContext(), { ...decision, providerId: input.providerId })` (from `@better-auth/core/context`; verify the export name) → `{ action: "continue" }`.
    5. Wrap 1–4 in `try/catch`; any exception → log the class and `{ action: "reject", code: "provisioning_failed" }`.
  - `databaseHooks.session.create.before: async (session, ctx) => { const decision = takeDecision(ctx); if (!decision) throw new APIError("FORBIDDEN", { code: "provisioning_missing" }); const adapter = await getCurrentAdapter(ctx.context.adapter); await provisionMembership(adapter, session.userId, decision); }` (`ctx` may be `null` → same `provisioning_missing`; `getCurrentAdapter` from `better-auth`, awaited).

- [ ] **Step 4: Integration tests** (`sign-in.int.test.ts`; `beforeAll` seed; users `t-<random>@corp.test`; organization slugs `org-<random>`; `afterAll` deletes the created users (members cascade) and organizations by slug):
  - **first sign-in** with `claims: { org }` → callback redirects to `/`; organization row with `provider_id = 'corp'`; user; member `member`; `auth.api.getSession({ headers: { cookie } })` returns the user.
  - **admin and demotion**: `groups: ["agenty-admins"]` → `admin`; next sign-in without groups → `member`.
  - **mixed-case email** `T-x@CORP.Test` at the IdP for an existing `t-x@corp.test` → same user id, one user row (Review Focus 1).
  - **claim missing** → location `/sign-in?error=organization_claim_missing`; no user/account/session rows for that email.
  - **other provider's organization**: `partner` user (`x@partner.test`) asserting an `org` created by `corp` → `organization_owned_by_other_provider`; no rows.
  - **organization changed**: same user signs in again with a different `org` → `organization_changed`; the member row is unchanged.
  - **email outside domains**: sign-in email `x@partner.test`, `idpEmail` `x@corp.test` → `email_domain_mismatch`; no rows.
  - **account bound elsewhere**: not reachable end-to-end (the domain check runs first and domains do not overlap), so it is covered by a unit test: export the `resolveUser` implementation as `resolveSignIn(input, database)` and call it with a fake `database` whose user has an account with another `providerId` → `{ action: "reject", code: "account_bound_to_other_provider" }`. Add the same style of unit tests for the reject paths and for "an exception becomes `provisioning_failed`".
  - **missing hand-over fails closed**: call `auth.options.databaseHooks.session.create.before` with a session and a fresh context object → throws `APIError` code `provisioning_missing`.
  - **rollback when the session hook fails** (deterministic): `createAuth` accepts `deps.resolveSignIn` (defaults to the real implementation). A test instance passes one that returns `{ action: "continue" }` without remembering a decision; the real sign-in then fails in `session.create.before` → callback redirects to `/sign-in?error=provisioning_missing`, and there are no user, account or session rows for that email.
  - **concurrent first sign-ins**: two different new users, same new slug, `Promise.all`: either both succeed with members in the same organization, or one ends at `/sign-in?error=try_again` and leaves no user, account or session rows.
  - **restart between redirect and callback** (Review Focus 3): `signInViaMock(authA, { stopAfterIdp: true })`, then `finishCallback(createAuth({ baseURL }), callbackUrl, cookie)` → success.
  - **access token encrypted**: the `account.access_token` of a fresh sign-in is not JWT-shaped (`/^ey[\w-]+\.[\w-]+\.[\w-]+$/` does not match).

- [ ] **Step 5: `task ci`; commit** — `git commit -m "feat(auth): provision organizations and memberships from ID-token claims"`.

---

### Task 4: RLS machinery and isolation tests

**Files:**
- Create: `src/server/auth/tenant-context.ts`, `src/server/db/tenant-table.ts`, `src/server/db/tenant.ts`, `src/server/db/tenant.int.test.ts`, `src/server/db/rls-guard.int.test.ts`

**Interfaces:**
- Consumes: `createDb`, `Db`, `getDb`, `organization` (Task 1), `Role` (Task 2).
- Produces:
  ```ts
  // tenant-context.ts (auth-internal; tests may import)
  export type TenantContext = { readonly userId: string; readonly organizationId: string; readonly role: Role; readonly [brand]: true };
  export function createTenantContext(values: { userId: string; organizationId: string; role: Role }): TenantContext;
  // tenant-table.ts (no server-only)
  export const appRole; export const tenantIdSetting: SQL; export function tenantId(); export function tenantIsolation(table: { tenantId: AnyPgColumn });
  // tenant.ts
  export type Tx; export async function withTenant<T>(ctx: TenantContext, fn: (tx: Tx) => Promise<T>, db?: Db): Promise<T>;
  ```

- [ ] **Step 1: `tenant-context.ts`**

```ts
import "server-only";
import type { Role } from "./providers";

declare const brand: unique symbol;

/** Who is acting, in which organization. Only src/server/auth creates these (Biome rule). */
export type TenantContext = {
  readonly userId: string;
  readonly organizationId: string;
  readonly role: Role;
  readonly [brand]: true;
};

export function createTenantContext(values: { userId: string; organizationId: string; role: Role }): TenantContext {
  return Object.freeze({ ...values }) as TenantContext;
}
```

- [ ] **Step 2: `tenant-table.ts`** (no `server-only`; relative `.ts` import of `./auth-schema.ts`)

```ts
import { type SQL, sql } from "drizzle-orm";
import { type AnyPgColumn, pgPolicy, pgRole, uuid } from "drizzle-orm/pg-core";
import { organization } from "./auth-schema.ts";

/** The runtime role; declared as existing so drizzle-kit never creates it. */
export const appRole = pgRole("agenty_app").existing();

/** The current transaction's tenant, or NULL when none is set (then no row matches). */
export const tenantIdSetting: SQL = sql`nullif(current_setting('app.tenant_id', true), '')::uuid`;

/** Every domain table's tenant column. */
export function tenantId() {
  return uuid("tenant_id").notNull().references(() => organization.id, { onDelete: "cascade" });
}

/**
 * Every domain table's only policy: rows of the current tenant, for reads and writes.
 * Usage: app.table("agents", { id: …, tenantId: tenantId() }, (t) => [tenantIsolation(t)])
 */
export function tenantIsolation(table: { tenantId: AnyPgColumn }) {
  return pgPolicy("tenant_isolation", {
    as: "permissive",
    for: "all",
    to: appRole,
    using: sql`${table.tenantId} = ${tenantIdSetting}`,
    withCheck: sql`${table.tenantId} = ${tenantIdSetting}`,
  });
}
```

- [ ] **Step 3: `tenant.ts`**

```ts
import "server-only";
import { sql } from "drizzle-orm";
import type { TenantContext } from "@/server/auth/tenant-context";
import { type Db, getDb } from "./client";

export type Tx = Parameters<Parameters<Db["transaction"]>[0]>[0];

/**
 * Runs fn in a transaction scoped to ctx's organization: RLS on domain tables only shows and
 * accepts that organization's rows. The setting is transaction-local and vanishes at commit/rollback.
 */
export async function withTenant<T>(ctx: TenantContext, fn: (tx: Tx) => Promise<T>, db: Db = getDb()): Promise<T> {
  return db.transaction(async (tx) => {
    await tx.execute(sql`select set_config('app.tenant_id', ${ctx.organizationId}, true)`);
    return fn(tx);
  });
}
```
(`src/server/db/**` is exempt from the tenant-context import restriction only for the type; it must not call `createTenantContext` — adjust the Biome override so `src/server/db/tenant.ts` may import the type, e.g. via `import type` allowed by the rule's options, or re-export the type from `@/server/auth/tenant` (Task 5) and import it from there.)

- [ ] **Step 4: Isolation tests** (`tenant.int.test.ts`). Setup as owner (`postgres(DATABASE_MIGRATION_URL, { max: 1 })`): two organizations in `auth.organization` with random slugs and `provider_id` null (`orgA`, `orgB`); schema `rls_fixture_<random>`, `grant usage … to agenty_app`; the fixture table **built from the helpers**:

```ts
const fixtureSchema = pgSchema(fixtureSchemaName);
const notes = fixtureSchema.table("notes", {
  id: uuid("id").primaryKey().defaultRandom(), tenantId: tenantId(), body: text("body").notNull(),
}, (t) => [tenantIsolation(t)]);
```
DDL via `drizzle-kit/api` (`generateDrizzleJson({ notes })` against an empty snapshot → `generateMigration`), executed as owner, then `grant select, insert, update, delete on all tables in schema … to agenty_app`. If `drizzle-kit/api` cannot produce it (e.g. the FK to a table outside the snapshot), write the DDL by hand using the SQL text of `tenantIdSetting` rendered with `new PgDialect().sqlToQuery(...)`, and add a unit assertion on `getTableConfig(notes).policies[0]`; record the ruling in the report. Seed as owner: two rows for A, one for B. `afterAll`: drop the schema cascade, delete both organizations, end pools. App-role instance `appDb = createDb(DATABASE_URL, { max: 1 })`; contexts via `createTenantContext`.

```ts
it("shows only the current organization's rows", async () => {
  const rows = await withTenant(ctxA, (tx) => tx.select().from(notes), appDb);
  expect(rows.map((r) => r.tenantId)).toEqual([orgA, orgA]);
});

it("shows nothing and accepts no writes without a tenant", async () => {
  expect(await appDb.select().from(notes)).toEqual([]);
  await expect(appDb.insert(notes).values({ tenantId: orgA, body: "x" })).rejects.toThrow(/row-level security/);
});

it("rejects inserting a row for another organization", async () => {
  await expect(withTenant(ctxA, (tx) => tx.insert(notes).values({ tenantId: orgB, body: "x" }), appDb))
    .rejects.toThrow(/row-level security/);
});

it("rejects moving a row to another organization and cannot touch foreign rows", async () => {
  await expect(withTenant(ctxA, (tx) => tx.update(notes).set({ tenantId: orgB }), appDb)).rejects.toThrow(/row-level security/);
  const updated = await withTenant(ctxA, (tx) => tx.update(notes).set({ body: "y" }).where(eq(notes.tenantId, orgB)).returning(), appDb);
  const deleted = await withTenant(ctxA, (tx) => tx.delete(notes).where(eq(notes.tenantId, orgB)).returning(), appDb);
  expect([updated, deleted]).toEqual([[], []]);
});

it.each(["commit", "rollback"])("does not leak the tenant to the next transaction after %s", async (end) => {
  const run = withTenant(ctxA, async (tx) => {
    await tx.select().from(notes);
    if (end === "rollback") throw new Error("rollback");
  }, appDb);
  if (end === "rollback") await expect(run).rejects.toThrow("rollback"); else await run;
  expect(await appDb.select().from(notes)).toEqual([]); // same single pooled connection
});

it("fails closed on a malformed tenant setting", async () => {
  const bad = createTenantContext({ userId: ctxA.userId, organizationId: "not-a-uuid", role: "member" });
  await expect(withTenant(bad, (tx) => tx.select().from(notes), appDb)).rejects.toThrow(/uuid/);
});

it("passes the organization id as a bind parameter", async () => {
  const injection = createTenantContext({ userId: ctxA.userId, organizationId: "x', true); drop table notes; --", role: "member" });
  await expect(withTenant(injection, (tx) => tx.select().from(notes), appDb)).rejects.toThrow(/uuid/);
  expect(await withTenant(ctxA, (tx) => tx.select().from(notes), appDb)).toHaveLength(2);
});
```
(Adjust the `/row-level security/` patterns to Postgres' exact messages if needed.)

- [ ] **Step 5: Catalog guard** (`rls-guard.int.test.ts`):

```ts
/** Tables in `schema` that miss tenant_id or RLS, or have a permissive policy not keyed on app.tenant_id. */
async function findRlsViolations(sql: postgres.Sql, schema: string): Promise<string[]> {
  const rows = await sql<{ table: string; problem: string }[]>`
    with t as (
      select c.oid, c.relname, c.relrowsecurity
      from pg_class c join pg_namespace n on n.oid = c.relnamespace
      where n.nspname = ${schema} and c.relkind in ('r', 'p')
    )
    select relname as table, 'no tenant_id uuid not null column' as problem from t
      where not exists (select 1 from pg_attribute a where a.attrelid = t.oid and a.attname = 'tenant_id'
        and a.atttypid = 'uuid'::regtype and a.attnotnull and not a.attisdropped)
    union all
    select relname, 'row-level security disabled' from t where not relrowsecurity
    union all
    select relname, 'no tenant_isolation policy for agenty_app' from t
      where not exists (select 1 from pg_policies p where p.schemaname = ${schema} and p.tablename = t.relname
        and 'agenty_app' = any(p.roles) and p.cmd = 'ALL'
        and p.qual like '%app.tenant_id%' and p.with_check like '%app.tenant_id%')
    union all
    select p.tablename, 'policy ' || p.policyname || ' not keyed on app.tenant_id' from pg_policies p
      where p.schemaname = ${schema} and p.permissive = 'PERMISSIVE'
        and (p.qual is null or p.qual not like '%app.tenant_id%'
             or (p.with_check is not null and p.with_check not like '%app.tenant_id%'))
    order by 1, 2`;
  return rows.map((r) => `${r.table}: ${r.problem}`);
}

/** Tables in schema app that are deliberately not tenant-scoped, with the reason. */
const EXCEPTIONS: Record<string, string> = {};

it("every table in schema app is tenant-scoped by RLS", async () => {
  const violations = (await findRlsViolations(appSql, "app")).filter((v) => !((v.split(":")[0] ?? "") in EXCEPTIONS));
  expect(violations).toEqual([]);
});

it("the guard reports missing tenant_id, disabled RLS and an open policy", async () => {
  // as owner: create schema rls_guard_<random> with table plain (id int) and table open
  // (tenant_id uuid not null, RLS enabled, policy `using (true)` for agenty_app);
  // expect findRlsViolations to list: plain → no tenant_id, RLS disabled, no policy;
  // open → no tenant_isolation policy, policy not keyed. Drop the schema.
});
```

- [ ] **Step 6: Run** `task test -- src/server/db`, `task lint`, `task typecheck` → green. Commit `git commit -m "feat(db): tenant RLS helpers, withTenant and isolation tests"`.

---

### Task 5: Tenant context and members

**Files:**
- Create: `src/server/auth/tenant.ts`, `src/server/auth/tenant.int.test.ts`, `src/server/auth/members.ts`

**Interfaces:**
- Consumes: Tasks 2–4.
- Produces:
  ```ts
  export type { TenantContext } from "./tenant-context";
  export class UnauthorizedError extends Error {}
  export class ForbiddenError extends Error {}
  export async function getTenantContext(deps?: { headers?: Headers; auth?: Auth; db?: Db }): Promise<TenantContext>;
  // members.ts
  export async function listMembers(ctx: TenantContext, db?: Db): Promise<{ name: string; email: string; role: Role }[]>;
  export async function getOrganizationSlug(ctx: TenantContext, db?: Db): Promise<string>;
  ```

- [ ] **Step 1: Implement `getTenantContext`**

```ts
export async function getTenantContext(deps: { headers?: Headers; auth?: Auth; db?: Db } = {}): Promise<TenantContext> {
  const auth = deps.auth ?? getAuth();
  const session = await auth.api.getSession({ headers: deps.headers ?? (await headers()) });
  if (!session) throw new UnauthorizedError("Not signed in");
  const db = deps.db ?? getDb();
  const [row] = await db
    .select({ organizationId: member.organizationId, role: member.role, providerId: ssoProvider.providerId })
    .from(member)
    .innerJoin(organization, eq(organization.id, member.organizationId))
    .leftJoin(ssoProvider, eq(ssoProvider.providerId, organization.providerId))
    .where(eq(member.userId, session.user.id));
  if (!row) throw new ForbiddenError("No organization membership");
  if (!row.providerId) throw new ForbiddenError("The organization has no identity provider");
  return createTenantContext({ userId: session.user.id, organizationId: row.organizationId, role: row.role });
}
```
`members.ts`: `listMembers` selects `user.name`, `user.email`, `member.role` where `member.organizationId = ctx.organizationId`, ordered by name; `getOrganizationSlug` by `ctx.organizationId`.

- [ ] **Step 2: Tests** (`tenant.int.test.ts`, sessions from `signInViaMock`, unique slugs):
  - no cookie → `UnauthorizedError`; garbage cookie → `UnauthorizedError`.
  - valid member → context with the organization's id and the role.
  - member row deleted → `ForbiddenError`.
  - provider removed: create provider `tmp-<random>` (issuer `http://localhost:8080/tmp-<random>`, domain `tmp-<random>.test`), sign in, delete the provider row (organization's `provider_id` becomes null) → `ForbiddenError`; clean up.
  - `listMembers` for an organization never returns members of another organization (two orgs, one member each).

- [ ] **Step 3: `task ci`; commit** — `git commit -m "feat(auth): tenant context and member list"`.

---

### Task 6: UI — sign-in, shell, home, members, proxy

**Files:**
- Create: `src/lib/auth-client.ts`, `src/app/sign-in/page.tsx`, `src/app/sign-in/sign-in-form.tsx`, `src/app/sign-in/error-messages.ts`, `src/app/sign-in/error-messages.test.ts`, `src/components/app-header.tsx`, `src/components/user-menu.tsx`, `src/app/settings/members/page.tsx`, `src/proxy.ts`, shadcn components as needed (`input`, `label`, `dropdown-menu`, `table`, `badge` via `pnpm dlx shadcn@latest add …`)
- Modify: `src/app/page.tsx`

Read first: `node_modules/next/dist/docs/01-app/02-guides/authentication-with-cache-components.md` and the proxy file convention doc. Under Cache Components, anything reading the session (headers/cookies) sits inside `<Suspense>`.

- [ ] **Step 1: Error messages (pure, unit-tested)** — `error-messages.ts`:

```ts
const MESSAGES: Record<string, string> = {
  unknown_domain: "No identity provider is configured for this email domain.",
  idp_unavailable: "Your organization's sign-in service is unavailable. Please try again later.",
  email_domain_mismatch: "Your identity provider returned an account outside its allowed email domains.",
  account_bound_to_other_provider: "This account signs in through a different identity provider.",
  organization_claim_missing: "Your account isn't assigned to an organization. Contact your administrator.",
  organization_owned_by_other_provider: "This organization belongs to a different identity provider.",
  organization_changed: "Your account belongs to a different organization. Contact your administrator.",
  try_again: "Sign-in could not be completed. Please try again.",
  invalid_request: "The sign-in request was invalid. Please try again.",
};
const FALLBACK = "Sign-in failed. Please try again.";

/** Maps an error code from the URL or API to fixed text; never shows IdP-provided text. */
export function signInErrorMessage(code: string | null | undefined): string | undefined {
  if (!code) return undefined;
  return MESSAGES[code] ?? FALLBACK;
}
```
Tests: every code maps; unknown code, `"unable to create session"`, `"provisioning_failed"` → fallback; empty → undefined.

- [ ] **Step 2: `auth-client.ts`**

```ts
import { ssoClient } from "@better-auth/sso/client";
import { createAuthClient } from "better-auth/react";

export const authClient = createAuthClient({ plugins: [ssoClient()] });
```

- [ ] **Step 3: Sign-in page** — `page.tsx` (server): heading "Sign in to Agenty"; reads `searchParams.error` inside `<Suspense>` and passes `signInErrorMessage(error)` to `SignInForm`. `sign-in-form.tsx` (client): `Label` "Work email" + `Input type="email"`, `Button` "Continue with SSO"; validates with `z.email()` ("Enter a valid email address."); calls `authClient.signIn.sso({ email, callbackURL: "/", errorCallbackURL: "/sign-in" })`; on `{ error }` shows `signInErrorMessage(error.code)`; button disabled while pending; messages in an element with `role="alert"`.

- [ ] **Step 4: Header and user menu** — `AppHeader` (async server component, rendered inside `<Suspense>`): `getTenantContext()`, the session user's name/email (`getAuth().api.getSession`), `getOrganizationSlug(ctx)`; shows the slug as organization name, a "Members" link for admins, and `UserMenu` (client: name, email, role badge, "Sign out" → `authClient.signOut()` then `router.push("/sign-in")`).

- [ ] **Step 5: Home `/`** — static shell; inside `<Suspense>` an async component: `UnauthorizedError` → the M0 landing (keep the `h1` "Agenty" and description) with a "Sign in" link to `/sign-in`; signed in → `AppHeader` plus a card "Agents arrive in the next milestone"; `ForbiddenError` → card "Your account has no active organization. Contact your administrator." plus sign-out.

- [ ] **Step 6: `/settings/members`** — inside `<Suspense>`: `getTenantContext()`; `UnauthorizedError` → `redirect("/sign-in")`; `ForbiddenError` or `role !== "admin"` → `notFound()`. `AppHeader` and a table (Name, Email, Role).

- [ ] **Step 7: `src/proxy.ts`**

```ts
import { getSessionCookie } from "better-auth/cookies";
import { type NextRequest, NextResponse } from "next/server";

/** Optimistic check only: no session cookie → sign-in. Pages validate the session themselves. */
export function proxy(request: NextRequest) {
  if (!getSessionCookie(request)) return NextResponse.redirect(new URL("/sign-in", request.url));
  return NextResponse.next();
}

export const config = { matcher: ["/settings/:path*"] };
```

- [ ] **Step 8: Verify by hand** — `task dev`; sign in as `alice@corp.test` with claims `{"email":"alice@corp.test","name":"Alice","email_verified":true,"org":"acme","groups":["agenty-admins"]}` → header "acme", Members link, table; sign out; `bob@corp.test` with `org: "acme"` and no groups → no Members link, `/settings/members` → 404. Note results in the report.

- [ ] **Step 9: `task ci`; commit** — `git commit -m "feat(ui): SSO sign-in, organization shell and members page"`.

---

### Task 7: E2E tests, Playwright and Docker CI wiring

**Files:**
- Create: `tests/e2e/auth.spec.ts`, `tests/e2e/support/mock-idp.ts`
- Modify: `playwright.config.ts`, `tests/e2e/home.spec.ts` (if the landing changed), `.github/workflows/ci.yml` (job `docker`)

- [ ] **Step 1: Playwright env** — `webServer.env` adds `BETTER_AUTH_SECRET: process.env.BETTER_AUTH_SECRET ?? ""`, `BETTER_AUTH_URL: \`http://127.0.0.1:${port}\``, `BETTER_AUTH_TRUSTED_ORIGINS: "http://localhost:8080"`. (`task test:e2e` depends on seeded providers: add `db:seed` to its `deps` or document that `task ci` seeds first; prefer the dependency.)

- [ ] **Step 2: IdP helper** `tests/e2e/support/mock-idp.ts`:

```ts
import type { Page } from "@playwright/test";

/** Completes the mock IdP's login form (no labels; fields by name). */
export async function loginAtMockIdp(page: Page, claims: { email: string; name: string; org?: string; groups?: string[] }) {
  await page.locator("input[name=username]").fill(claims.email);
  await page.locator("textarea[name=claims]").fill(JSON.stringify({ ...claims, email_verified: true }));
  await page.getByRole("button", { name: "Sign-in" }).click();
}

const run = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;
/** Per-run names, so repeated runs against the same dev database never collide. */
export const uniqueEmail = (prefix: string, domain: string) => `${prefix}-${run}@${domain}`;
export const uniqueOrg = (prefix: string) => `${prefix}-${run}`;
```

- [ ] **Step 3: `tests/e2e/auth.spec.ts`**

```ts
async function signIn(page: Page, email: string, claims: { name: string; org?: string; groups?: string[] }, idpEmail = email) {
  await page.goto("/sign-in");
  await page.getByLabel("Work email").fill(email);
  await page.getByRole("button", { name: "Continue with SSO" }).click();
  await loginAtMockIdp(page, { email: idpEmail, ...claims });
}

test("signed-out visitors are sent to sign-in from app pages", async ({ page }) => {
  await page.goto("/settings/members");
  await expect(page).toHaveURL(/\/sign-in$/);
});

test("an admin sees the organization and its members", async ({ page }) => {
  const org = uniqueOrg("acme");
  const email = uniqueEmail("admin", "corp.test");
  await signIn(page, email, { name: "Ada Admin", org, groups: ["agenty-admins"] });
  await expect(page.getByText(org, { exact: true })).toBeVisible();
  await page.getByRole("link", { name: "Members", exact: true }).click();
  await expect(page.getByRole("cell", { name: email, exact: true })).toBeVisible();
});
```
Add, following the same pattern: a member of the same org has no Members link and sees the 404 page at `/settings/members`; a user in another org (`uniqueOrg("globex")`) sees only their org and only their members; a `partner.test` user asserting the admin test's org (create it first in that test via a `corp` sign-in in a separate browser context) gets the `organization_owned_by_other_provider` message (`role=alert`); `eve@evil.test` gets the unknown-domain message; sign-out (user menu → "Sign out") returns to `/sign-in` and `/settings/members` redirects to `/sign-in` again. E2E users are not deleted (documented in AGENTS.md).

- [ ] **Step 4: Docker CI job** — add the env to the `docker run`: `-e BETTER_AUTH_SECRET=ci-only-secret-0123456789abcdef0123 -e BETTER_AUTH_URL=http://localhost:3000`. No providers or mock IdP needed: the smoke test only checks health.

- [ ] **Step 5: `task ci`** (E2E included) → green; commit `git commit -m "test(e2e): SSO sign-in flows against the mock IdP"`.

---

### Task 8: Documentation and final verification

**Files:**
- Modify: `AGENTS.md`, `README.md`

- [ ] **Step 1: AGENTS.md** —
  - intro: SSO-only (OIDC), organization = tenant (from an ID-token claim, owned by the creating provider), workspaces later.
  - Commands: `task db:up` also starts the mock IdP; `task db:seed`; `task auth:generate`.
  - Architecture tree: `src/server/auth/` modules, `src/proxy.ts`, `tests/support/sso.ts`; the list of server modules without `server-only` (and why).
  - Database: schema `auth` (tables, no RLS, why, import restriction, accepted risks: session tokens and **plain-text provider client secrets** readable by `agenty_app`); remove the "M1 must decide" notes; E2E users accumulate in the dev database.
  - New section "Auth": providers in `auth.sso_provider` with our columns, strict row validation (why: no plugin defaults for SQL-written rows), one-off discovery and the provider fingerprint (never modify a provider row while sign-ins may be in flight; the seed is insert-if-absent), endpoint and body allowlists, provisioning (`resolveUser` → hand-over → `session.create.before` through the transaction adapter, no fallback), organization rules, 12 h sessions, `getTenantContext()`.
  - Non-negotiable rule 1 concretised: every domain table uses `tenantId()` + `tenantIsolation()`, all access through `withTenant(getTenantContext())`; the catalog guard enforces it. Rule 4 gets the recorded exception for provider client secrets.
  - Version notes: Better Auth packages pinned to the same exact version; mock IdP tag.
- [ ] **Step 2: README** — production setup: env vars (`BETTER_AUTH_SECRET`, `BETTER_AUTH_URL`, `BETTER_AUTH_TRUSTED_ORIGINS` for IdPs on internal networks); registering Agenty at an IdP (redirect URI `<BETTER_AUTH_URL>/api/auth/sso/callback/<provider id>`, required **ID-token** claims: `email`, `name`, the organization claim, optionally the groups claim); SQL to add a provider (the Keycloak example with `pkce` and `scopes`), to re-discover (remove the three endpoint keys), and to remove one (delete its `auth.account` rows and the provider row; its organizations stay, orphaned); rules: never change an issuer, never re-use a provider id, non-overlapping domains without commas; the 12-hour deprovisioning lag; development sign-in with the mock IdP (claims JSON example). Troubleshooting: port 8080 in use; existing `.env` lacks the new keys (copy them from `.env.example`).
- [ ] **Step 3: Final verification** — `task db:reset && task db:up && task ci` → green; `task docker:build` and the CI `docker run` locally → health ok. Commit `git commit -m "docs: SSO, organizations and RLS in AGENTS.md and README"`.
