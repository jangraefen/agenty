# M1 – SSO and organizations: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** OIDC single sign-on as the only login, organizations (= tenants) declared in a config file and provisioned from the IdP, and Postgres RLS machinery with isolation tests that later domain tables build on.

**Architecture:** Better Auth (Drizzle adapter, transactions on) with the `@better-auth/sso` plugin; providers come from the config file as in-memory `defaultSSO` entries whose endpoints we discover lazily. Organizations and memberships are our own tables in schema `auth` (no RLS), written inside the sign-in transaction through Better Auth's transaction adapter. Domain tables in schema `app` use `tenantId()`/`tenantIsolation()` helpers; `withTenant()` sets `app.tenant_id` per transaction from a `TenantContext` that only `src/server/auth` can create.

**Tech Stack:** Next.js 16.4 (App Router, Cache Components), TypeScript 7, Better Auth 1.7.7 + `@better-auth/sso` 1.7.7, Drizzle ORM 0.45 / drizzle-kit 0.31 (postgres.js), Postgres 18, Zod 4, Vitest 5, Playwright 1.64, `ghcr.io/navikt/mock-oauth2-server:6.0.5` (dev/test only).

**Spec:** `docs/specs/2026-10-09-m1-sso-organizations-design.md`

## Global Constraints

- Better Auth and `@better-auth/sso` pinned to the same exact version, `1.7.7` (≥ 1.7.7 for security fixes); `pnpm add better-auth@1.7.7 @better-auth/sso@1.7.7`.
- Mock IdP image `ghcr.io/navikt/mock-oauth2-server:6.0.5`, port 8080, always addressed as `http://localhost:8080` (its issuer follows the Host header; never mix with `127.0.0.1`).
- Every server module imports `"server-only"` (except `src/server/db/migrate.ts`, which plain Node runs). Configuration only via `getEnv()`; owner URL only via `takeMigrationUrl()`.
- Zod at every boundary (env, config file, discovery document, ID-token claims we read, route/page inputs).
- Errors and logs never contain secrets, tokens, connection strings or IdP-controlled text (`error_description`).
- Schema `auth` holds `user`, `session`, `account`, `verification`, `sso_provider`, `organization`, `member`; no RLS. Only `src/server/auth/**` and `src/server/db/**` import `@/server/db/auth-schema` (Biome rule, Task 6).
- RLS names: column `tenant_id`, setting `app.tenant_id`. Our APIs say `organizationId`.
- Sessions: absolute 12 h (`expiresIn: 43_200`), `disableSessionRefresh: true`, cookie cache off.
- Shell scripts POSIX `sh`, BSD/macOS compatible. Biome only. Tests named after the guarantee they protect; unit `*.test.ts`, integration `*.int.test.ts`.
- Commit messages end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never `git add -A` (agent worktrees live under `.claude/worktrees`); add paths explicitly.
- Before using a library API, check the installed package's types/docs (`node_modules/…`, `node_modules/next/dist/docs/`). Research notes with exact Better Auth signatures and dist line references: `/tmp/claude-1000/-home-claude-git-agenty/78f36167-489a-4c82-a580-5cb61afe50e6/scratchpad/ba-api.md` (read the relevant section; if the file is gone, read the package `.d.mts` files).
- `task ci` must pass before the final commit of every task that touches code (needs `task db:up`).

## Review Focus

1. **Email case:** an IdP that returns `Alice@ACME.Test` must match `acme.test`, map to the same user as `alice@acme.test`, and not be rejected as a domain mismatch → tests in Task 1 (`domainMatches`) and Task 7 (sign-in with mixed-case email lands in the existing user's organization).
2. **Odd role claims:** a `groups` claim that is a string, an array with non-strings, an object, or missing must never throw; it yields `member` unless a string value matches → Task 1 `deriveRole` tests.
3. **Server restart between redirect and callback:** the callback arrives at a process whose `defaultSSO` entry has no discovered endpoints yet; it must still complete → Task 6 hook discovers on callback when endpoints are missing; test in Task 7.
4. **Config path in the standalone server:** `server.js` changes the working directory to `.next/standalone`, so a relative `AUTH_CONFIG_FILE` resolves elsewhere; the error must name the resolved absolute path, and Playwright/Docker pass absolute paths → Task 1 loader test, Task 9 configs.
5. **One IdP down:** while Globex's IdP is unreachable, Acme users still sign in and Globex users get the readable `idp_unavailable` message → Task 5 unit test, Task 7 integration test.

---

## File structure

```
config/agenty.dev.json                    dev/test organizations (Acme, Globex) against the mock IdP
docker-compose.yml                        + service mock-oidc
src/server/env.ts                         + BETTER_AUTH_SECRET, BETTER_AUTH_URL, AUTH_CONFIG_FILE
src/server/auth/
  config.ts           config file schema, loader, domain matching, role rule (pure + fs)
  discovery.ts        lazy OIDC discovery with cache
  organizations.ts    syncOrganizations (start-up), organization lookup
  tenant-context.ts   TenantContext type + brand constructor (auth-internal)
  provisioning.ts     resolveUser decision, session.create.before member upsert, hand-over WeakMap
  auth.ts             getAuth(): betterAuth instance (lazy), hooks (endpoint/body allowlist, discovery)
  tenant.ts           getTenantContext()
  members.ts          listMembers(ctx)
src/server/db/
  auth-schema.ts      Drizzle tables in pgSchema("auth")
  schema.ts           app schema (+ re-export not needed; drizzle config lists both files)
  client.ts           createDb(url, opts) + getDb()
  tenant.ts           withTenant(ctx, fn, db?)
  tenant-table.ts     appRole, tenantId(), tenantIsolation()
  migrations/0002…    CREATE SCHEMA auth / grants / tables
src/app/api/auth/[...all]/route.ts
src/app/sign-in/page.tsx (+ sign-in-form.tsx client component)
src/app/(app)/… shell layout, home, settings/members
src/app/page.tsx      landing (signed out) / home (signed in)
src/lib/auth-client.ts
src/proxy.ts
tests/support/sso.ts  HTTP sign-in helper driving the mock IdP
tests/e2e/auth.spec.ts
```

---

### Task 1: Dependencies, environment and the auth config file

**Files:**
- Modify: `package.json`, `pnpm-lock.yaml` (via pnpm), `src/server/env.ts`, `src/server/env.test.ts`, `.env.example`
- Create: `src/server/auth/config.ts`, `src/server/auth/config.test.ts`, `config/agenty.dev.json`

**Interfaces:**
- Produces:
  ```ts
  // env.ts additions to Env
  BETTER_AUTH_SECRET: string; BETTER_AUTH_URL: string; AUTH_CONFIG_FILE: string;
  // config.ts
  export type Role = "admin" | "member";
  export type RoleRule = { claim: string; admin: string[] };
  export type OrganizationConfig = {
    slug: string; name: string; domains: string[];
    oidc: { issuer: string; discoveryUrl?: string;
            endpoints?: { authorization: string; token: string; jwks: string; userInfo?: string };
            clientId: string; clientSecret: string; scopes: string[];
            roles?: RoleRule; privateNetwork: boolean };
  };
  export type AuthConfig = { organizations: OrganizationConfig[] };
  export function parseAuthConfig(json: unknown, env: Record<string, string | undefined>): AuthConfig;
  export async function loadAuthConfig(file: string, env?: Record<string, string | undefined>): Promise<AuthConfig>;
  export function getAuthConfig(): Promise<AuthConfig>; // memoised loadAuthConfig(getEnv().AUTH_CONFIG_FILE)
  export function emailDomain(email: string): string | undefined;   // lower-cased part after the last "@"
  export function domainMatches(emailDomainValue: string, domain: string): boolean;
  export function findOrganizationByEmail(config: AuthConfig, email: string): OrganizationConfig | undefined;
  export function findOrganizationBySlug(config: AuthConfig, slug: string): OrganizationConfig | undefined;
  export function deriveRole(claims: Record<string, unknown>, rule: RoleRule | undefined): Role;
  ```

- [ ] **Step 1: Install dependencies**

```bash
pnpm add better-auth@1.7.7 @better-auth/sso@1.7.7
```
Check `pnpm-workspace.yaml` `allowBuilds` if pnpm reports blocked build scripts; allow only what is needed and note why in the commit message. Run `pnpm exec tsc --noEmit` once to see whether Better Auth's types compile under TypeScript 7 (spec risk). If they do not, stop and report (DONE_WITH_CONCERNS) with the errors.

- [ ] **Step 2: Write failing env tests** (add to `src/server/env.test.ts`, following its existing style)

```ts
const base = {
  DATABASE_URL: "postgres://a:b@localhost:5432/agenty",
  BETTER_AUTH_SECRET: "x".repeat(32),
  BETTER_AUTH_URL: "http://localhost:3000",
  AUTH_CONFIG_FILE: "config/agenty.dev.json",
};

it("accepts the auth settings", () => {
  expect(parseEnv(base)).toMatchObject({ BETTER_AUTH_URL: "http://localhost:3000" });
});

it("rejects a short BETTER_AUTH_SECRET without echoing it", () => {
  const secret = "short-secret-value";
  expect(() => parseEnv({ ...base, BETTER_AUTH_SECRET: secret })).toThrow(/BETTER_AUTH_SECRET/);
  expect(() => parseEnv({ ...base, BETTER_AUTH_SECRET: secret })).not.toThrow(new RegExp(secret));
});

it("rejects a non-http BETTER_AUTH_URL", () => {
  expect(() => parseEnv({ ...base, BETTER_AUTH_URL: "ftp://x" })).toThrow(/BETTER_AUTH_URL/);
});

it("requires AUTH_CONFIG_FILE", () => {
  const { AUTH_CONFIG_FILE: _, ...rest } = base;
  expect(() => parseEnv(rest)).toThrow(/AUTH_CONFIG_FILE/);
});
```
Update the existing env tests' fixtures to include the three new variables.

- [ ] **Step 3: Implement env** — in `envSchema` add:

```ts
BETTER_AUTH_SECRET: z.string().min(32, "must be at least 32 characters"),
BETTER_AUTH_URL: z.url({ protocol: /^https?$/, error: "must be an http(s) URL" }),
AUTH_CONFIG_FILE: z.string().min(1, "must be set"),
```
Run `task test -- src/server/env.test.ts` → PASS.

- [ ] **Step 4: Write failing config tests** (`src/server/auth/config.test.ts`)

```ts
import { mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { describe, expect, it } from "vitest";
import {
  deriveRole, domainMatches, emailDomain, findOrganizationByEmail, loadAuthConfig, parseAuthConfig,
} from "./config";

const env = { ACME_SECRET: "acme-secret-value", GLOBEX_SECRET: "globex-secret-value" };
const org = (slug: string, domains: string[], extra: object = {}) => ({
  slug, name: slug.toUpperCase(), domains,
  oidc: { issuer: `http://localhost:8080/${slug}`, clientId: "agenty",
          clientSecretEnv: `${slug.toUpperCase()}_SECRET`, ...extra },
});
const valid = { organizations: [org("acme", ["acme.test"]), org("globex", ["globex.test"])] };

describe("parseAuthConfig", () => {
  it("resolves secrets from env and applies defaults", () => {
    const config = parseAuthConfig(valid, env);
    expect(config.organizations[0]?.oidc).toMatchObject({
      clientSecret: "acme-secret-value", scopes: ["openid", "email", "profile"], privateNetwork: false,
    });
  });

  it.each([
    ["unknown key", { organizations: [{ ...org("acme", ["acme.test"]), extra: 1 }] }, /organizations\.0/],
    ["bad slug", { organizations: [org("Acme!", ["acme.test"])] }, /organizations\.0\.slug/],
    ["upper-case domain", { organizations: [org("acme", ["ACME.test"])] }, /domains\.0/],
    ["comma in domain", { organizations: [org("acme", ["a.test,b.test"])] }, /domains\.0/],
    ["no domains", { organizations: [org("acme", [])] }, /domains/],
    ["duplicate slug", { organizations: [org("acme", ["a.test"]), org("acme", ["b.test"])] }, /slug/],
    ["same domain twice", { organizations: [org("acme", ["a.test"]), org("globex", ["a.test"])] }, /domains/],
    ["subdomain of another org", { organizations: [org("acme", ["a.test"]), org("globex", ["x.a.test"])] }, /domains/],
    ["issuer not http(s)", { organizations: [org("acme", ["a.test"], { issuer: "ftp://x" })] }, /oidc\.issuer/],
    ["scopes without openid", { organizations: [org("acme", ["a.test"], { scopes: ["email"] })] }, /scopes/],
    ["no organizations", { organizations: [] }, /organizations/],
  ])("rejects %s", (_name, json, pathPattern) => {
    expect(() => parseAuthConfig(json, env)).toThrow(pathPattern);
  });

  it("rejects a missing secret env var by name, not value", () => {
    expect(() => parseAuthConfig(valid, { ACME_SECRET: "acme-secret-value" })).toThrow(/GLOBEX_SECRET/);
  });

  it("never puts secret values into errors", () => {
    const bad = { organizations: [org("acme", ["acme.test"], { issuer: "nope" })] };
    expect(() => parseAuthConfig(bad, env)).not.toThrow(/acme-secret-value/);
  });
});

describe("loadAuthConfig", () => {
  it("names the resolved absolute path when the file is missing", async () => {
    await expect(loadAuthConfig("does/not/exist.json", env)).rejects.toThrow(
      path.resolve("does/not/exist.json"),
    );
  });

  it("rejects invalid JSON without echoing the content", async () => {
    const dir = await mkdtemp(path.join(tmpdir(), "agenty-config-"));
    const file = path.join(dir, "c.json");
    await writeFile(file, '{"organizations": [ "acme-secret-value" ');
    await expect(loadAuthConfig(file, env)).rejects.toThrow(/not valid JSON/);
    await expect(loadAuthConfig(file, env)).rejects.not.toThrow(/acme-secret-value/);
  });

  it("loads the committed dev config", async () => {
    const config = await loadAuthConfig("config/agenty.dev.json", {
      ACME_OIDC_CLIENT_SECRET: "x", GLOBEX_OIDC_CLIENT_SECRET: "y",
    });
    expect(config.organizations.map((o) => o.slug)).toEqual(["acme", "globex"]);
  });
});

describe("domain matching", () => {
  it("extracts the lower-cased domain after the last @", () => {
    expect(emailDomain("Alice@ACME.Test")).toBe("acme.test");
    expect(emailDomain("weird@name@acme.test")).toBe("acme.test");
    expect(emailDomain("no-at-sign")).toBeUndefined();
  });

  it("matches exact domains and subdomains, case-insensitively", () => {
    expect(domainMatches("acme.test", "acme.test")).toBe(true);
    expect(domainMatches("EU.Acme.Test", "acme.test")).toBe(true);
    expect(domainMatches("notacme.test", "acme.test")).toBe(false);
  });

  it("finds the organization for an email", () => {
    const config = parseAuthConfig(valid, env);
    expect(findOrganizationByEmail(config, "Bob@Globex.TEST")?.slug).toBe("globex");
    expect(findOrganizationByEmail(config, "eve@evil.test")).toBeUndefined();
  });
});

describe("deriveRole", () => {
  const rule = { claim: "groups", admin: ["agenty-admins"] };
  it.each([
    [{ groups: ["agenty-admins", "x"] }, "admin"],
    [{ groups: "agenty-admins" }, "admin"],
    [{ groups: ["x"] }, "member"],
    [{ groups: [1, null, { a: 1 }] }, "member"],
    [{ groups: { "agenty-admins": true } }, "member"],
    [{}, "member"],
  ] as const)("%j → %s", (claims, role) => {
    expect(deriveRole(claims, rule)).toBe(role);
  });

  it("is member without a rule", () => {
    expect(deriveRole({ groups: ["agenty-admins"] }, undefined)).toBe("member");
  });
});
```
Run → FAIL (module missing).

- [ ] **Step 5: Implement `src/server/auth/config.ts`**

```ts
import "server-only";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { z } from "zod";
import { getEnv } from "@/server/env";

export type Role = "admin" | "member";

const httpUrl = z.url({ protocol: /^https?$/, error: "must be an http(s) URL" });
const domainName = z
  .string()
  .regex(/^(?=.{1,253}$)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z][a-z0-9-]{0,62}$/, {
    error: "must be a lower-case domain name",
  });

const roleRuleSchema = z.strictObject({
  claim: z.string().min(1),
  admin: z.array(z.string().min(1)).min(1),
});

const oidcSchema = z.strictObject({
  issuer: httpUrl,
  discoveryUrl: httpUrl.optional(),
  endpoints: z
    .strictObject({ authorization: httpUrl, token: httpUrl, jwks: httpUrl, userInfo: httpUrl.optional() })
    .optional(),
  clientId: z.string().min(1),
  clientSecretEnv: z.string().regex(/^[A-Z][A-Z0-9_]*$/, { error: "must be an environment variable name" }),
  scopes: z
    .array(z.string().regex(/^[!#-[\]-~]+$/, { error: "must be a scope token" }))
    .default(["openid", "email", "profile"])
    .refine((s) => s.includes("openid"), { error: "must include openid" }),
  roles: roleRuleSchema.optional(),
  privateNetwork: z.boolean().default(false),
});

const organizationSchema = z.strictObject({
  slug: z.string().regex(/^[a-z0-9][a-z0-9-]{1,62}$/, { error: "must be lower-case letters, digits, dashes" }),
  name: z.string().trim().min(1).max(100),
  domains: z.array(domainName).min(1),
  oidc: oidcSchema,
});

const fileSchema = z
  .strictObject({ organizations: z.array(organizationSchema).min(1) })
  .superRefine((config, ctx) => {
    const slugs = new Set<string>();
    config.organizations.forEach((org, i) => {
      if (slugs.has(org.slug)) ctx.addIssue({ code: "custom", path: ["organizations", i, "slug"], message: "is not unique" });
      slugs.add(org.slug);
      config.organizations.forEach((other, j) => {
        if (j <= i) return;
        org.domains.forEach((a, k) => {
          for (const b of other.domains) {
            if (a === b || a.endsWith(`.${b}`) || b.endsWith(`.${a}`)) {
              ctx.addIssue({ code: "custom", path: ["organizations", i, "domains", k],
                message: `overlaps with a domain of organization "${other.slug}"` });
            }
          }
        });
      });
    });
  });

type FileConfig = z.infer<typeof fileSchema>;
type FileOrganization = FileConfig["organizations"][number];
export type RoleRule = z.infer<typeof roleRuleSchema>;
export type OrganizationConfig = Omit<FileOrganization, "oidc"> & {
  oidc: Omit<FileOrganization["oidc"], "clientSecretEnv"> & { clientSecret: string };
};
export type AuthConfig = { organizations: OrganizationConfig[] };

function fail(problems: string[]): never {
  throw new Error(`Invalid auth configuration:\n${problems.map((p) => `  - ${p}`).join("\n")}`);
}

/** Validates the parsed file and resolves secrets. Errors name JSON paths, never values. */
export function parseAuthConfig(json: unknown, env: Record<string, string | undefined>): AuthConfig {
  const result = fileSchema.safeParse(json);
  if (!result.success) {
    fail(result.error.issues.map((issue) => `${issue.path.join(".") || "(root)"}: ${issue.message}`));
  }
  const missing: string[] = [];
  const organizations = result.data.organizations.map((org, i) => {
    const { clientSecretEnv, ...oidc } = org.oidc;
    const clientSecret = env[clientSecretEnv];
    if (!clientSecret) missing.push(`organizations.${i}.oidc.clientSecretEnv: environment variable ${clientSecretEnv} is not set`);
    return { ...org, oidc: { ...oidc, clientSecret: clientSecret ?? "" } };
  });
  if (missing.length > 0) fail(missing);
  return { organizations };
}

export async function loadAuthConfig(
  file: string,
  env: Record<string, string | undefined> = process.env,
): Promise<AuthConfig> {
  const resolved = path.resolve(file);
  let text: string;
  try {
    text = await readFile(resolved, "utf8");
  } catch {
    throw new Error(`Auth configuration file ${resolved} cannot be read`);
  }
  let json: unknown;
  try {
    json = JSON.parse(text.replace(/^﻿/, ""));
  } catch {
    throw new Error(`Auth configuration file ${resolved} is not valid JSON`);
  }
  return parseAuthConfig(json, env);
}

let cached: Promise<AuthConfig> | undefined;

/** The configuration of this process, loaded once (each server bundle loads its own copy). */
export function getAuthConfig(): Promise<AuthConfig> {
  cached ??= loadAuthConfig(getEnv().AUTH_CONFIG_FILE).catch((error: unknown) => {
    cached = undefined;
    throw error;
  });
  return cached;
}

export function emailDomain(email: string): string | undefined {
  const at = email.lastIndexOf("@");
  if (at < 0 || at === email.length - 1) return undefined;
  return email.slice(at + 1).toLowerCase();
}

/** Same rule as @better-auth/sso: exact match or subdomain, case-insensitive. */
export function domainMatches(emailDomainValue: string, domain: string): boolean {
  const value = emailDomainValue.toLowerCase();
  return value === domain || value.endsWith(`.${domain}`);
}

export function findOrganizationByEmail(config: AuthConfig, email: string): OrganizationConfig | undefined {
  const domain = emailDomain(email);
  if (!domain) return undefined;
  return config.organizations.find((org) => org.domains.some((d) => domainMatches(domain, d)));
}

export function findOrganizationBySlug(config: AuthConfig, slug: string): OrganizationConfig | undefined {
  return config.organizations.find((org) => org.slug === slug);
}

export function deriveRole(claims: Record<string, unknown>, rule: RoleRule | undefined): Role {
  if (!rule) return "member";
  const value = claims[rule.claim];
  const values = typeof value === "string" ? [value] : Array.isArray(value) ? value : [];
  return values.some((v) => typeof v === "string" && rule.admin.includes(v)) ? "admin" : "member";
}
```
Adjust Zod 4 call shapes to the installed version's types if needed (e.g. `{ error }` vs. `{ message }`) without changing behaviour.

- [ ] **Step 6: Create `config/agenty.dev.json`**

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
        "roles": { "claim": "groups", "admin": ["agenty-admins"] },
        "privateNetwork": true
      }
    },
    {
      "slug": "globex",
      "name": "Globex",
      "domains": ["globex.test"],
      "oidc": {
        "issuer": "http://localhost:8080/globex",
        "clientId": "agenty",
        "clientSecretEnv": "GLOBEX_OIDC_CLIENT_SECRET",
        "roles": { "claim": "groups", "admin": ["agenty-admins"] },
        "privateNetwork": true
      }
    }
  ]
}
```

- [ ] **Step 7: `.env.example`** — append:

```sh
# Better Auth. Development values only; generate a real secret with `openssl rand -base64 32`.
BETTER_AUTH_SECRET=dev-only-secret-change-me-0123456789abcdef
BETTER_AUTH_URL=http://localhost:3000
# Organizations and their identity providers (absolute path recommended in production).
AUTH_CONFIG_FILE=config/agenty.dev.json
# Client secrets referenced by the config file (the mock IdP accepts any value).
ACME_OIDC_CLIENT_SECRET=dev-acme-secret
GLOBEX_OIDC_CLIENT_SECRET=dev-globex-secret
```
Add the same keys to your local `.env` (it is not regenerated).

- [ ] **Step 8: Run tests and lint** — `task test -- src/server/auth src/server/env.test.ts` → PASS; `task lint`, `task typecheck` → clean.

- [ ] **Step 9: Commit** — `git add package.json pnpm-lock.yaml pnpm-workspace.yaml src/server/env.ts src/server/env.test.ts src/server/auth/config.ts src/server/auth/config.test.ts config/agenty.dev.json .env.example` then `git commit -m "feat(auth): env and organization config file"`.

---

### Task 2: Schema `auth`, migrations, role guarantees, mock IdP service

**Files:**
- Create: `src/server/db/auth-schema.ts`, migrations `0002_*`, `0003_auth_grants.sql`, `0004_*` (names as drizzle-kit generates)
- Modify: `drizzle.config.ts`, `src/server/db/client.ts`, `src/server/db/roles.int.test.ts`, `docker-compose.yml`, `.github/workflows/ci.yml` (job `ci`), `Taskfile.yml`, `.gitignore`

**Interfaces:**
- Produces (`auth-schema.ts`), exported table objects keyed by Better Auth model names: `user`, `session`, `account`, `verification`, `ssoProvider`, `organization`, `member`, plus `export const authSchema = pgSchema("auth")`. Column names snake_case, JS keys camelCase, ids `uuid` with `default(sql\`pg_catalog.gen_random_uuid()\`)`, all timestamps `timestamp(..., { withTimezone: true })`.
  - `organization`: `id`, `slug` (text, unique, not null), `name` (text not null), `issuer` (text not null), `createdAt`, `updatedAt` (default now, not null).
  - `member`: `id`, `userId` (uuid not null → `user.id` on delete cascade, **unique**), `organizationId` (uuid not null → `organization.id` on delete cascade, index), `role` (text not null, check `role in ('admin','member')`), `lastSignInAt` (timestamptz not null), `createdAt`, `updatedAt`.
- Produces (`client.ts`): `export function createDb(url: string, options?: { max?: number })` returning the Drizzle instance with the combined schema; `export type Db = ReturnType<typeof createDb>`; `getDb()` unchanged in behaviour.

- [ ] **Step 1: Generate the Better Auth reference schema** — create a throw-away config in the scratchpad (not in the repo) that builds `betterAuth({ database: drizzleAdapter(drizzle("postgres://x@localhost/x"), { provider: "pg", schemaName: "auth", transaction: true }), advanced: { database: { generateId: "uuid" } }, plugins: [sso()] })` and run `npx auth@1.7.7 generate --config <file> --output <scratch>/generated.ts -y` (see `ba-api.md` §10). Use its `user`, `session`, `account`, `verification`, `sso_provider` definitions as the basis for `auth-schema.ts`; do not include the organization-plugin tables or `session.activeOrganizationId`. Add `organization` and `member` as specified above. Convert timestamps to `withTimezone: true`. Keep Better Auth's indexes. Drop the generated `relations()` unless something needs them.

- [ ] **Step 2: Add `task auth:generate`** to `Taskfile.yml`: runs the same generation with a committed `scripts/auth-generate.config.ts` (the throw-away config from step 1, importing nothing from `src/`) into `build/auth-schema.generated.ts`; `build/` is already git-ignored (check). Description: "Write Better Auth's reference schema to build/ for diffing after upgrades".

- [ ] **Step 3: Migrations in three steps** (drizzle-kit emits `CREATE SCHEMA` itself, and default privileges must exist before the tables):
  1. `drizzle.config.ts`: `schema: ["./src/server/db/schema.ts", "./src/server/db/auth-schema.ts"]`, `schemaFilter: ["app", "auth"]`. Temporarily reduce `auth-schema.ts` to `export const authSchema = pgSchema("auth");` and run `task db:generate -- --name auth_schema` → `0002_auth_schema.sql` with `CREATE SCHEMA "auth";` only.
  2. `task db:generate -- --custom --name auth_grants` and write:
     ```sql
     GRANT USAGE ON SCHEMA "auth" TO agenty_app;
     --> statement-breakpoint
     ALTER DEFAULT PRIVILEGES FOR ROLE agenty_owner IN SCHEMA "auth"
       GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO agenty_app;
     --> statement-breakpoint
     ALTER DEFAULT PRIVILEGES FOR ROLE agenty_owner IN SCHEMA "auth"
       GRANT USAGE, SELECT ON SEQUENCES TO agenty_app;
     ```
     (Compare with `0001_app_grants.sql` and mirror its exact form.)
  3. Restore the full `auth-schema.ts` and run `task db:generate -- --name auth_tables`. Review the SQL: tables in `"auth"`, uuid ids, FKs, unique `member.user_id`, check constraint on `role`.
  Apply with `task db:migrate`.

- [ ] **Step 4: Client** — `src/server/db/client.ts`:

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

- [ ] **Step 5: Widen `roles.int.test.ts`** — parameterise the "schema ownership" block over `["app", "auth"]` (`describe.each`): schema owner is `agenty_owner`, every table owned by `agenty_owner`, the grant probe (create a probe table in that schema inside the rolled-back owner transaction) yields SELECT/INSERT/UPDATE/DELETE true and TRUNCATE false, and the app role cannot `CREATE` in it. Add:

```ts
it.each(["app", "auth"])("agenty_app has USAGE on schema %s", async (schema) => {
  const [row] = await appSql`select has_schema_privilege(current_user, ${schema}, 'USAGE') as ok`;
  expect(row?.ok).toBe(true);
});

it.each(["app", "auth"])("sequences agenty_owner creates in %s are usable by agenty_app", async (schema) => {
  // inside a rolled-back owner transaction: create sequence <schema>.__seq_probe, then
  // has_sequence_privilege('agenty_app', '<schema>.__seq_probe', 'USAGE') and 'SELECT' are true
});
```
Write the sequence probe with the same `Rollback` pattern as the table probe. Run `task test -- src/server/db` → PASS (and confirm a mutation bites: temporarily remove the sequence default-privilege statement in a scratch DB? Not needed; reviewing the assertion suffices).

- [ ] **Step 6: Mock IdP in compose** — `docker-compose.yml`:

```yaml
  mock-oidc:
    image: ghcr.io/navikt/mock-oauth2-server:6.0.5
    ports:
      - "127.0.0.1:8080:8080"
```
The image has no curl, so it has no healthcheck; `task db:up` waits for it explicitly. Change `db:up` to:

```yaml
  db:up:
    desc: Start local Postgres and the mock IdP, wait until both are ready
    cmds:
      - docker compose up --detach --wait postgres
      - docker compose up --detach mock-oidc
      - sh scripts/wait-for-url.sh http://localhost:8080/isalive
```
Create `scripts/wait-for-url.sh` (POSIX sh, BSD-compatible; uses `curl -fsS` in a loop with `sleep 1`, 60 tries, prints "<url> not reachable" to stderr and exits 1 on timeout). Update `db:down`/`db:reset` descriptions ("Postgres and mock IdP").

- [ ] **Step 7: Mock IdP in CI (job `ci`)** — under `services:` add

```yaml
      mock-oidc:
        image: ghcr.io/navikt/mock-oauth2-server:6.0.5
        ports: ["8080:8080"]
```
and a step before `task setup`: `- name: Wait for the mock IdP` / `run: sh scripts/wait-for-url.sh http://localhost:8080/isalive`.

- [ ] **Step 8: Verify and commit** — `task db:up`, `task ci` → green. `git add` the touched files and `git commit -m "feat(db): schema auth with Better Auth, organization and member tables"`.

---

### Task 3: RLS machinery and isolation tests

**Files:**
- Create: `src/server/auth/tenant-context.ts`, `src/server/db/tenant.ts`, `src/server/db/tenant-table.ts`, `src/server/db/tenant.int.test.ts`, `src/server/db/rls-guard.int.test.ts`

**Interfaces:**
- Consumes: `createDb`, `Db`, `getDb` (Task 2), `organization` table (Task 2).
- Produces:
  ```ts
  // src/server/auth/tenant-context.ts
  export type TenantContext = { readonly userId: string; readonly organizationId: string; readonly role: Role; readonly [brand]: true };
  export function createTenantContext(values: { userId: string; organizationId: string; role: Role }): TenantContext; // auth-internal + tests
  // src/server/db/tenant.ts
  export type Tx = Parameters<Parameters<Db["transaction"]>[0]>[0];
  export async function withTenant<T>(ctx: TenantContext, fn: (tx: Tx) => Promise<T>, db?: Db): Promise<T>;
  // src/server/db/tenant-table.ts
  export const appRole: PgRole;            // pgRole("agenty_app").existing()
  export const tenantIdSetting: SQL;       // nullif(current_setting('app.tenant_id', true), '')::uuid
  export function tenantId(): /* uuid("tenant_id").notNull().references(() => organization.id, { onDelete: "cascade" }) */;
  export function tenantIsolation(table: { tenantId: AnyPgColumn }): PgPolicy;
  ```

- [ ] **Step 1: `tenant-context.ts`**

```ts
import "server-only";
import type { Role } from "./config";

declare const brand: unique symbol;

/** Who is acting, in which organization. Only src/server/auth creates these (Biome rule, Task 6). */
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

- [ ] **Step 2: `tenant-table.ts`**

```ts
import "server-only";
import { type SQL, sql } from "drizzle-orm";
import { type AnyPgColumn, pgPolicy, pgRole, uuid } from "drizzle-orm/pg-core";
import { organization } from "./auth-schema";

/** The runtime role; declared as existing so drizzle-kit never creates it. */
export const appRole = pgRole("agenty_app").existing();

/** The current transaction's tenant, or NULL when none is set (then no row matches). */
export const tenantIdSetting: SQL = sql`nullif(current_setting('app.tenant_id', true), '')::uuid`;

/** Every domain table's tenant column. */
export function tenantId() {
  return uuid("tenant_id").notNull().references(() => organization.id, { onDelete: "cascade" });
}

/** Every domain table's only policy: rows of the current tenant, for reads and writes. */
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
Usage (document in a JSDoc example on `tenantIsolation`): `app.table("agents", { id: …, tenantId: tenantId() }, (t) => [tenantIsolation(t)])`.

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

- [ ] **Step 4: Isolation tests** (`src/server/db/tenant.int.test.ts`). Setup (`beforeAll`), as owner (`postgres(DATABASE_MIGRATION_URL, { max: 1 })`):
  - create two organizations in `auth.organization` with random slugs (`rls-a-<random>`, `rls-b-<random>`) and issuer `http://localhost:8080/test` (as `agenty_app` via `createDb(DATABASE_URL)` is fine too); keep their ids `orgA`, `orgB`.
  - `create schema rls_fixture_<random>`; `grant usage on schema … to agenty_app`; create the fixture table **from the helpers**: define in the test
    ```ts
    const fixtureSchema = pgSchema(fixtureSchemaName);
    const notes = fixtureSchema.table("notes", { id: uuid("id").primaryKey().defaultRandom(), tenantId: tenantId(), body: text("body").notNull() }, (t) => [tenantIsolation(t)]);
    ```
    and produce its DDL with `drizzle-kit/api` (`generateDrizzleJson({ notes })` → `generateMigration(generateDrizzleJson({}), that)`), execute the statements as owner, then `grant select, insert, update, delete on all tables in schema … to agenty_app`. If `drizzle-kit/api` cannot produce DDL for a table referencing a table outside the snapshot, rule: write the DDL by hand in the test **using `tenantIdSetting`'s SQL text** (render it with the Drizzle `PgDialect().sqlToQuery`) and add a unit test that snapshots the helper output (`getTableConfig(notes).policies[0]`); record the ruling in the report.
  - seed as owner: two rows for A, one for B.
  - `afterAll`: drop the fixture schema cascade, delete both organizations, end pools.

  Tests (app-role Drizzle instance `createDb(DATABASE_URL, { max: 1 })` named `appDb`, contexts via `createTenantContext`):

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
  await expect(
    withTenant(ctxA, (tx) => tx.insert(notes).values({ tenantId: orgB, body: "x" }), appDb),
  ).rejects.toThrow(/row-level security/);
});

it("rejects moving a row to another organization and cannot touch foreign rows", async () => {
  await expect(
    withTenant(ctxA, (tx) => tx.update(notes).set({ tenantId: orgB }), appDb),
  ).rejects.toThrow(/row-level security/);
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
(`ctxA`/`ctxB` use random user ids; adjust the `/row-level security/` patterns to the exact Postgres messages if needed.)

- [ ] **Step 5: Catalog guard** (`src/server/db/rls-guard.int.test.ts`):

```ts
/** Tables in `schema` that miss tenant_id, RLS, or have a policy not keyed on app.tenant_id. */
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
  const violations = (await findRlsViolations(appSql, "app")).filter((v) => !(v.split(":")[0]! in EXCEPTIONS));
  expect(violations).toEqual([]);
});

it("the guard reports a table without tenant_id, RLS and policy", async () => {
  // as owner: create schema rls_guard_<random>, table t (id int), plus a table with RLS and a
  // permissive `using (true)` policy; expect findRlsViolations to list all four problems; drop schema.
});
```

- [ ] **Step 6: Run** `task test -- src/server/db` → PASS; `task lint`, `task typecheck`.

- [ ] **Step 7: Commit** — `git commit -m "feat(db): tenant RLS helpers, withTenant and isolation tests"` (explicit paths).

---

### Task 4: Organization sync at server start

**Files:**
- Create: `src/server/auth/organizations.ts`, `src/server/auth/organizations.int.test.ts`
- Modify: `src/instrumentation.ts`

**Interfaces:**
- Consumes: `AuthConfig` (Task 1), `createDb`/`Db` (Task 2), `organization` table.
- Produces:
  ```ts
  export async function syncOrganizations(db: Db, config: AuthConfig): Promise<Map<string, string>>; // slug → id
  export async function getOrganizationIdBySlug(slug: string, db?: Db): Promise<string | undefined>;
  ```

- [ ] **Step 1: Failing integration tests** (`organizations.int.test.ts`, app-role `createDb(DATABASE_URL, { max: 2 })`, slugs with a random suffix, cleanup in `afterAll`):
  - inserts new organizations and returns their ids;
  - renames an existing organization (name changed in config) and keeps its id;
  - is idempotent (second run, same ids, no new rows);
  - rejects a changed issuer for an existing slug with an error naming the slug (`/issuer of organization "<slug>" changed/`) and leaves the row unchanged;
  - leaves organizations that are not in the config untouched;
  - two concurrent `syncOrganizations` calls with the same new config both succeed and create one row per slug.

- [ ] **Step 2: Implement**

```ts
import "server-only";
import { and, eq, sql } from "drizzle-orm";
import { type Db, getDb } from "@/server/db/client";
import { organization } from "@/server/db/auth-schema";
import type { AuthConfig } from "./config";

/**
 * Upserts the configured organizations by slug (insert or rename). An organization's issuer is
 * immutable: accounts are keyed by provider (= slug) and subject, so pointing a slug at another
 * IdP could hand existing users to foreign subjects.
 */
export async function syncOrganizations(db: Db, config: AuthConfig): Promise<Map<string, string>> {
  const ids = new Map<string, string>();
  for (const org of config.organizations) {
    const [row] = await db
      .insert(organization)
      .values({ slug: org.slug, name: org.name, issuer: org.oidc.issuer })
      .onConflictDoUpdate({
        target: organization.slug,
        set: { name: org.name, updatedAt: sql`now()` },
        setWhere: eq(organization.issuer, org.oidc.issuer),
      })
      .returning({ id: organization.id });
    if (!row) throw new Error(`The issuer of organization "${org.slug}" changed; see README (changing an IdP)`);
    ids.set(org.slug, row.id);
  }
  return ids;
}

export async function getOrganizationIdBySlug(slug: string, db: Db = getDb()): Promise<string | undefined> {
  const [row] = await db.select({ id: organization.id }).from(organization).where(eq(organization.slug, slug));
  return row?.id;
}
```
(`and` import only if used; check `onConflictDoUpdate`'s `setWhere` name in the installed Drizzle types.)

- [ ] **Step 3: Start-up wiring** — in `src/instrumentation.ts`, after `migrateDatabase(...)`:

```ts
const { loadAuthConfig } = await import("./server/auth/config");
const { syncOrganizations } = await import("./server/auth/organizations");
const { createDb } = await import("./server/db/client");
const config = await loadAuthConfig(env.AUTH_CONFIG_FILE);
const db = createDb(env.DATABASE_URL, { max: 1 });
try {
  await syncOrganizations(db, config);
} finally {
  await db.$client.end();
}
```
(`env` = the result of `getEnv()`, which `register()` already calls — keep the value.) Invalid config or a changed issuer therefore stops the server with the error message, as for invalid env. Note in a comment that the app bundle loads the config again (`getAuthConfig()`), because instrumentation and route handlers do not share module state.

- [ ] **Step 4: Verify start-up behaviour manually** — `task build`, then run `node .next/standalone/server.js` with the `.env` values exported and `AUTH_CONFIG_FILE` set to the absolute path of `config/agenty.dev.json`: health is ok and `select slug from auth.organization` shows acme and globex. Run again with `AUTH_CONFIG_FILE=/nonexistent.json`: the process exits 1 and logs "cannot be read". Record both in the report.

- [ ] **Step 5: Run `task ci` and commit** — `git commit -m "feat(auth): sync configured organizations at server start"`.

---

### Task 5: Lazy OIDC discovery

**Files:**
- Create: `src/server/auth/discovery.ts`, `src/server/auth/discovery.test.ts`

**Interfaces:**
- Consumes: `OrganizationConfig` (Task 1).
- Produces:
  ```ts
  export type OidcEndpoints = { authorizationEndpoint: string; tokenEndpoint: string; jwksEndpoint: string; userInfoEndpoint?: string };
  export class IdpUnavailableError extends Error {}   // message never contains IdP output
  export function createDiscovery(options?: { fetch?: typeof fetch; now?: () => number; ttlMs?: number; timeoutMs?: number }): {
    endpointsFor(org: OrganizationConfig): Promise<OidcEndpoints>;
  };
  ```

- [ ] **Step 1: Failing unit tests** with an injected `fetch` stub (no network):
  - returns the endpoints of a valid discovery document from `<issuer>/.well-known/openid-configuration` (and from `discoveryUrl` when configured);
  - returns configured `endpoints` without fetching;
  - rejects (`IdpUnavailableError`) when the document's `issuer` differs from the configured one (also a trailing-slash difference);
  - rejects on non-2xx, invalid JSON, missing `jwks_uri`, a non-http(s) endpoint, and a fetch that times out (stub that never resolves + small `timeoutMs`);
  - with `privateNetwork: true`, rejects a document whose endpoints are on a different origin than the issuer (the trusted-origin list only covers the issuer origin);
  - caches per slug for `ttlMs` (second call no fetch; after `now` advances past ttl, fetches again);
  - does not cache failures (fail, then succeed on the next call);
  - concurrent calls for the same slug share one fetch;
  - one organization's failure does not affect another's cached endpoints;
  - the error message contains neither the response body nor the URL's query string.

- [ ] **Step 2: Implement**

```ts
import "server-only";
import { z } from "zod";
import type { OrganizationConfig } from "./config";

const httpUrl = z.url({ protocol: /^https?$/ });
const documentSchema = z.object({
  issuer: z.string(),
  authorization_endpoint: httpUrl,
  token_endpoint: httpUrl,
  jwks_uri: httpUrl,
  userinfo_endpoint: httpUrl.optional(),
});

export type OidcEndpoints = {
  authorizationEndpoint: string;
  tokenEndpoint: string;
  jwksEndpoint: string;
  userInfoEndpoint?: string;
};

export class IdpUnavailableError extends Error {
  constructor(slug: string, reason: string) {
    super(`Identity provider of organization "${slug}" is unavailable: ${reason}`);
    this.name = "IdpUnavailableError";
  }
}

export function createDiscovery({
  fetch: fetchFn = globalThis.fetch,
  now = Date.now,
  ttlMs = 60 * 60 * 1000,
  timeoutMs = 5000,
}: { fetch?: typeof fetch; now?: () => number; ttlMs?: number; timeoutMs?: number } = {}) {
  const cache = new Map<string, { endpoints: OidcEndpoints; expiresAt: number }>();
  const inflight = new Map<string, Promise<OidcEndpoints>>();

  async function discover(org: OrganizationConfig): Promise<OidcEndpoints> {
    const url = org.oidc.discoveryUrl ?? `${org.oidc.issuer.replace(/\/$/, "")}/.well-known/openid-configuration`;
    let response: Response;
    try {
      response = await fetchFn(url, { signal: AbortSignal.timeout(timeoutMs), redirect: "error" });
    } catch {
      throw new IdpUnavailableError(org.slug, "discovery request failed");
    }
    if (!response.ok) throw new IdpUnavailableError(org.slug, `discovery returned HTTP ${response.status}`);
    let json: unknown;
    try {
      json = await response.json();
    } catch {
      throw new IdpUnavailableError(org.slug, "discovery document is not JSON");
    }
    const parsed = documentSchema.safeParse(json);
    if (!parsed.success) throw new IdpUnavailableError(org.slug, "discovery document is incomplete");
    if (parsed.data.issuer !== org.oidc.issuer) throw new IdpUnavailableError(org.slug, "issuer mismatch");
    const endpoints: OidcEndpoints = {
      authorizationEndpoint: parsed.data.authorization_endpoint,
      tokenEndpoint: parsed.data.token_endpoint,
      jwksEndpoint: parsed.data.jwks_uri,
      ...(parsed.data.userinfo_endpoint ? { userInfoEndpoint: parsed.data.userinfo_endpoint } : {}),
    };
    if (org.oidc.privateNetwork) {
      const origin = new URL(org.oidc.issuer).origin;
      if (Object.values(endpoints).some((e) => new URL(e).origin !== origin)) {
        throw new IdpUnavailableError(org.slug, "endpoints are not on the issuer's origin");
      }
    }
    return endpoints;
  }

  return {
    async endpointsFor(org: OrganizationConfig): Promise<OidcEndpoints> {
      const configured = org.oidc.endpoints;
      if (configured) {
        return {
          authorizationEndpoint: configured.authorization,
          tokenEndpoint: configured.token,
          jwksEndpoint: configured.jwks,
          ...(configured.userInfo ? { userInfoEndpoint: configured.userInfo } : {}),
        };
      }
      const hit = cache.get(org.slug);
      if (hit && hit.expiresAt > now()) return hit.endpoints;
      let pending = inflight.get(org.slug);
      if (!pending) {
        pending = discover(org)
          .then((endpoints) => {
            cache.set(org.slug, { endpoints, expiresAt: now() + ttlMs });
            return endpoints;
          })
          .finally(() => inflight.delete(org.slug));
        inflight.set(org.slug, pending);
      }
      return pending;
    },
  };
}
```

- [ ] **Step 3: Run tests, lint, typecheck; commit** — `git commit -m "feat(auth): lazy OIDC discovery with cache"`.

---

### Task 6: Better Auth instance, endpoint allowlist and route handler

**Files:**
- Create: `src/server/auth/auth.ts`, `src/server/auth/auth.int.test.ts`, `src/app/api/auth/[...all]/route.ts`, `tests/support/sso.ts`
- Modify: `biome.json`

**Interfaces:**
- Consumes: Tasks 1, 2, 5. `getDb()`.
- Produces:
  ```ts
  export const ALLOWED_ENDPOINTS: ReadonlySet<string>; // "/sign-in/sso", "/sso/callback/:providerId", "/get-session", "/sign-out"
  export async function getAuth(): Promise<Auth>;        // lazy, memoised; builds from getAuthConfig()
  export async function createAuth(config: AuthConfig, deps?: { db?: Db; discovery?: Discovery; baseURL?: string; secret?: string }): Promise<Auth>; // tests
  export type Auth = ReturnType<typeof betterAuth<…>>;    // inferred
  // tests/support/sso.ts
  export async function signInViaMock(auth: Auth, opts: { email: string; name?: string; groups?: string[]; idpEmail?: string; baseURL: string }):
    Promise<{ status: number; location: string; cookie: string }>; // cookie = Cookie header value with the session (empty on failure)
  ```

- [ ] **Step 1: Write `auth.ts`** — build the instance (verify every option name against `node_modules/better-auth` types; see `ba-api.md` §6–§9):

```ts
import "server-only";
import { sso } from "@better-auth/sso";
import { betterAuth } from "better-auth";
import { drizzleAdapter } from "better-auth/adapters/drizzle";
import { APIError, createAuthMiddleware } from "better-auth/api";
import * as authSchema from "@/server/db/auth-schema";
import { type Db, getDb } from "@/server/db/client";
import { getEnv } from "@/server/env";
import { type AuthConfig, findOrganizationByEmail, findOrganizationBySlug, getAuthConfig } from "./config";
import { createDiscovery, IdpUnavailableError } from "./discovery";

export const ALLOWED_ENDPOINTS: ReadonlySet<string> = new Set([
  "/sign-in/sso", "/sso/callback/:providerId", "/get-session", "/sign-out",
]);
const SIGN_IN_BODY_KEYS = new Set(["email", "callbackURL", "errorCallbackURL"]);
const SESSION_SECONDS = 12 * 60 * 60;
```
`createAuth(config, deps)`:
- `defaultSSO`: one entry per organization: `{ providerId: org.slug, domain: org.domains.join(","), oidcConfig: { issuer, clientId, clientSecret, pkce: true, scopes, mapping: { email: "email", name: "name", emailVerified: "email_verified" } } }`. Keep the array; the hooks mutate `oidcConfig` in place with discovered endpoints (`Object.assign(entry.oidcConfig, endpoints)`), which the plugin reads per request (verified, `ba-api.md` §4).
- `database: drizzleAdapter(db, { provider: "pg", schema: authSchema, transaction: true })`.
- `secret`, `baseURL` (from env unless given), `basePath: "/api/auth"`.
- `trustedOrigins`: `[baseURL, ...issuer origins of organizations with privateNetwork]`.
- `advanced: { database: { generateId: "uuid" }, useSecureCookies: baseURL.startsWith("https://") }`.
- `session: { expiresIn: SESSION_SECONDS, disableSessionRefresh: true, cookieCache: { enabled: false } }`.
- `account: { encryptOAuthTokens: true, accountLinking: { disableImplicitLinking: true } }`.
- `rateLimit: { enabled: false }`; no `emailAndPassword`, no `socialProviders`.
- `onAPIError: { errorURL: "/sign-in" }`.
- `plugins: [sso({ defaultSSO, providersLimit: 0, organizationProvisioning: { disabled: true } }), organizationsPlugin]` where `organizationsPlugin` is a local Better Auth plugin (`{ id: "agenty-organizations", schema: { organization: {...}, member: {...} } }`) that declares our two tables so the adapter accepts them as models (Task 7 writes through the adapter). Field types per Better Auth's `DBFieldAttribute` (`string`, `date`, `references`). Task 7 adds `resolveUser` and `databaseHooks`; leave clearly marked placeholders that reject (`resolveUser: () => ({ action: "reject", code: "not_ready" })`) so no sign-in can complete before provisioning exists.
- `hooks.before: createAuthMiddleware(async (ctx) => { … })`:
  1. If `!ALLOWED_ENDPOINTS.has(ctx.path)` → `throw new APIError("NOT_FOUND")`. Verify that `ctx.path` carries the route pattern (`/sso/callback/:providerId`) for parameterised routes; if it carries the concrete path, match `^/sso/callback/[^/]+$` instead.
  2. On `/sign-in/sso`: reject any body key outside `SIGN_IN_BODY_KEYS` (`APIError("BAD_REQUEST", { code: "invalid_request" })`); require a string `email`; `org = findOrganizationByEmail(config, email)` or `APIError("BAD_REQUEST", { code: "unknown_domain" })`; `await ensureEndpoints(org)`.
  3. On `/sso/callback/:providerId`: `org = findOrganizationBySlug(config, ctx.params.providerId)`; if found and its `defaultSSO` entry lacks endpoints (process restarted after the redirect), `await ensureEndpoints(org)`; on `IdpUnavailableError` redirect to `/sign-in?error=idp_unavailable` (`throw ctx.redirect(...)`).
  - `ensureEndpoints(org)`: `Object.assign(entry.oidcConfig, await discovery.endpointsFor(org))`; maps `IdpUnavailableError` to `APIError("SERVICE_UNAVAILABLE", { code: "idp_unavailable" })` and logs the error message server-side.
- `getAuth()`: memoised `createAuth(await getAuthConfig())` with a reset on failure (like `getAuthConfig`).

- [ ] **Step 2: Route handler** — `src/app/api/auth/[...all]/route.ts`:

```ts
import { connection } from "next/server";
import { getAuth } from "@/server/auth/auth";

async function handle(request: Request): Promise<Response> {
  await connection();
  return (await getAuth()).handler(request);
}

export { handle as GET, handle as POST };
```
(`toNextJsHandler` needs a synchronous instance; the lazy async `getAuth()` makes a thin wrapper simpler. Check that Next accepts the re-exported names; otherwise declare `export async function GET/POST`.)

- [ ] **Step 3: Biome import restriction** — in `biome.json` add an override for all files except `src/server/auth/**`, `src/server/db/**` and `**/*.test.ts`: rule `style/noRestrictedImports` (check its group/name in Biome 2.5 docs) with paths `@/server/db/auth-schema` ("Only src/server/auth may use the auth tables") and `@/server/auth/tenant-context` ("Use getTenantContext()"). Verify with a scratch import in `src/app` that `task lint` fails, then remove it.

- [ ] **Step 4: Test helper** `tests/support/sso.ts` (drives the real flow over HTTP; mock form facts in `infra-facts.md` §1):
  1. `POST {baseURL}/api/auth/sign-in/sso` via `auth.handler(new Request(...))` with JSON `{ email, callbackURL: "/", errorCallbackURL: "/sign-in" }` and header `origin: baseURL`; read `{ url }` and the `set-cookie` headers (state cookie).
  2. `fetch(url, { method: "POST", body: new URLSearchParams({ username: idpEmail ?? email, claims: JSON.stringify({ email: idpEmail ?? email, name: name ?? "Test User", email_verified: true, ...(groups ? { groups } : {}) }) }), redirect: "manual" })` → `302` with `location` = app callback URL.
  3. `auth.handler(new Request(location, { headers: { cookie } }))` → 302; return its `location` and the merged `Cookie` header (session cookie included on success).
  Use a small cookie-jar function (parse `getSetCookie()`), no new dependency.

- [ ] **Step 5: Wiring tests** (`auth.int.test.ts`, `createAuth(await loadAuthConfig("config/agenty.dev.json"), { baseURL: "http://localhost:3000" })`):

```ts
it("exposes exactly the allowed endpoints", async () => {
  const paths = Object.values(auth.api).map((e) => (e as { path?: string }).path).filter(Boolean) as string[];
  for (const path of paths) {
    const url = `http://localhost:3000/api/auth${path.replace(/:\w+/g, "x")}`;
    const res = await auth.handler(new Request(url, { method: "POST", headers: { origin: "http://localhost:3000", "content-type": "application/json" }, body: "{}" }));
    if (ALLOWED_ENDPOINTS.has(path)) expect.soft(res.status, path).not.toBe(404);
    else expect.soft(res.status, path).toBe(404);
  }
});

it("has the expected plugins only", () => {
  expect(auth.options.plugins?.map((p) => p.id).sort()).toEqual(["agenty-organizations", "sso"]);
});

it("rejects sign-in bodies with extra keys", async () => { /* scopes, additionalParams, providerId → 400 invalid_request */ });
it("rejects an unknown email domain", async () => { /* → 400, body code unknown_domain */ });
it("redirects a known email to its IdP with PKCE and the configured scopes", async () => {
  // POST sign-in for alice@acme.test → { url } starts with http://localhost:8080/acme/authorize,
  // has code_challenge, scope "openid email profile" (no offline_access), redirect_uri .../sso/callback/acme
});
it("reports idp_unavailable for an unreachable IdP while others still work", async () => {
  // createAuth with a config copy whose globex issuer is http://localhost:1/globex (nothing listens):
  // sign-in for bob@globex.test → 503 idp_unavailable; alice@acme.test still gets an authorize URL
});
```
Methods: use the endpoint's own method (`e.options.method`, GET for get-session) when building requests so allowed endpoints don't fail for the wrong reason.

- [ ] **Step 6: Run `task ci`; commit** — `git commit -m "feat(auth): Better Auth with SSO, endpoint allowlist and lazy discovery"`.

---

### Task 7: Provisioning, account binding and tenant context

This task proves the spec's two risky mechanisms (hand-over and transactional member write). **If either cannot be made to work as specified, stop and report BLOCKED with findings; do not implement a fallback** (maintainer decision).

**Files:**
- Create: `src/server/auth/provisioning.ts`, `src/server/auth/provisioning.test.ts`, `src/server/auth/sign-in.int.test.ts`, `src/server/auth/tenant.ts`, `src/server/auth/tenant.int.test.ts`, `src/server/auth/members.ts`
- Modify: `src/server/auth/auth.ts`

**Interfaces:**
- Consumes: Tasks 1–6.
- Produces:
  ```ts
  // provisioning.ts
  export type SignInDecision = { organizationSlug: string; role: Role };
  export function decideSignIn(input: { providerId: string; email: string; verifiedIdTokenClaims: Record<string, unknown> }, config: AuthConfig):
    { ok: true; decision: SignInDecision } | { ok: false; code: "email_domain_mismatch" | "unknown_provider" };
  export function rememberDecision(endpointContext: object, decision: SignInDecision): void;   // WeakMap
  export function takeDecision(endpointContext: object | null | undefined): SignInDecision | undefined;
  // tenant.ts
  export class UnauthorizedError extends Error {}; export class ForbiddenError extends Error {}
  export async function getTenantContext(headers?: Headers): Promise<TenantContext>;  // defaults to next/headers
  // members.ts
  export async function listMembers(ctx: TenantContext): Promise<{ name: string; email: string; role: Role; lastSignInAt: Date }[]>;
  ```

- [ ] **Step 1: Unit tests for `decideSignIn`** — provider slug unknown → `unknown_provider`; email outside the provider's domains → `email_domain_mismatch`; mixed-case email inside → ok; role from ID-token claims via `deriveRole` (admin, member).

- [ ] **Step 2: Implement `provisioning.ts`** (pure decision + WeakMap hand-over):

```ts
const decisions = new WeakMap<object, SignInDecision>();
export function rememberDecision(endpointContext: object, decision: SignInDecision) { decisions.set(endpointContext, decision); }
export function takeDecision(endpointContext: object | null | undefined) {
  if (!endpointContext) return undefined;
  const decision = decisions.get(endpointContext);
  decisions.delete(endpointContext);
  return decision;
}
```

- [ ] **Step 3: Wire the hooks in `auth.ts`** (replace the placeholders):
  - `sso({ …, resolveUser: async (input, { database }) => { … } })`:
    1. `decideSignIn({ providerId: input.providerId, email: input.providerUser.email, verifiedIdTokenClaims: input.verifiedIdTokenClaims }, config)`; on failure `return { action: "reject", code }`.
    2. Binding: find the user by email (`database.findOne({ model: "user", where: [{ field: "email", value: email.toLowerCase() }] })`); if found, list its accounts (`database.findMany({ model: "account", where: [{ field: "userId", value: user.id }] })`); if any account's `providerId` differs from `input.providerId` → `return { action: "reject", code: "account_bound_to_other_provider" }`. Check how Better Auth normalises stored emails (lower-case?) and match that.
    3. `rememberDecision(getCurrentAuthEndpointContext(), decision)` (import from `@better-auth/core/context`; verify the export name in `ba-api.md` §1) and `return { action: "continue" }`. Never throw.
  - `databaseHooks.session.create.before: async (session, ctx) => { … }`:
    1. `const decision = takeDecision(ctx)`; none → `throw new APIError("FORBIDDEN", { code: "provisioning_missing" })`.
    2. `const adapter = getCurrentAdapter(ctx.context.adapter)` (transaction-bound).
    3. Look up the organization id by slug through the adapter (`model: "organization"`); missing → throw.
    4. Existing member row for `session.userId`: if its `organizationId` differs → throw (never move users between organizations); else update `role`, `lastSignInAt`, `updatedAt`. No row → create `{ userId, organizationId, role, lastSignInAt: new Date() }`.
    5. Return nothing (session unchanged).
  - Confirm, by test, that a throw here rolls back user, account and session.

- [ ] **Step 4: Sign-in integration tests** (`sign-in.int.test.ts`, mock IdP via `signInViaMock`, unique emails like `t-<random>@acme.test`, cleanup: delete created users by email in `afterAll`; member rows cascade):
  - first sign-in → 302 to `/`; `auth.user` row exists; `auth.member` row for acme with role `member`; session cookie works with `auth.api.getSession({ headers })`.
  - sign-in with `groups: ["agenty-admins"]` → role `admin`; a later sign-in without the group → `member` (demotion), `last_sign_in_at` advanced.
  - mixed-case email (`T-x@ACME.test` at the IdP) for an existing user → same user id, no second user (Review Focus 1). If Better Auth does not lower-case, normalise in `mapping`/`resolveUser` and assert the stored email is lower-case.
  - Globex IdP asserting an `@acme.test` email (sign-in email `t@globex.test`, `idpEmail` `t@acme.test`) → redirect location `/sign-in?error=email_domain_mismatch`; no user, account or session rows for that email.
  - provisioning failure rolls back: run against a `createAuth` whose config contains an organization slug not yet synced to the DB (organization lookup fails in `session.create.before`) → redirect with an error; no user/account/session rows created.
  - restart between redirect and callback (Review Focus 3): start the flow with instance A, finish the callback with a freshly created instance B (`createAuth` again, empty discovery cache) → success.
  - Globex IdP unreachable (config copy pointing Globex at `http://localhost:1/globex`): Globex sign-in → `idp_unavailable`; Acme sign-in in the same instance succeeds (Review Focus 5).
  - `account` row's `access_token` is not stored in plain text (does not equal the token the mock issued; compare by checking it is not a JWT-shaped string).

- [ ] **Step 5: `getTenantContext`** (`tenant.ts`):

```ts
export async function getTenantContext(requestHeaders?: Headers): Promise<TenantContext> {
  const auth = await getAuth();
  const session = await auth.api.getSession({ headers: requestHeaders ?? (await headers()) });
  if (!session) throw new UnauthorizedError("Not signed in");
  const [row] = await getDb()
    .select({ organizationId: member.organizationId, role: member.role, slug: organization.slug })
    .from(member)
    .innerJoin(organization, eq(organization.id, member.organizationId))
    .where(eq(member.userId, session.user.id));
  if (!row) throw new ForbiddenError("No organization membership");
  const config = await getAuthConfig();
  if (!findOrganizationBySlug(config, row.slug)) throw new ForbiddenError("Organization is not configured");
  return createTenantContext({ userId: session.user.id, organizationId: row.organizationId, role: row.role });
}
```
(`role` column typed as `"admin" | "member"` in the Drizzle schema via `text("role", { enum: ["admin", "member"] })`.)
Tests (`tenant.int.test.ts`, sessions from `signInViaMock`): no cookie → `UnauthorizedError`; valid member → context with the right organization and role; member row deleted → `ForbiddenError`; organization removed from config (instance whose config lacks globex, Globex user's session) → `ForbiddenError`; expired/invalid cookie → `UnauthorizedError`.

- [ ] **Step 6: `members.ts`** — `listMembers(ctx)`: users joined with member rows where `member.organizationId = ctx.organizationId`, ordered by name; returns name, email, role, lastSignInAt. Test: an Acme context never sees Globex members.

- [ ] **Step 7: Run `task ci`; commit** — `git commit -m "feat(auth): provision organization membership at sign-in, tenant context"`.

---

### Task 8: UI — sign-in, shell, home, members, proxy

**Files:**
- Create: `src/lib/auth-client.ts`, `src/app/sign-in/page.tsx`, `src/app/sign-in/sign-in-form.tsx`, `src/app/sign-in/error-messages.ts`, `src/app/sign-in/error-messages.test.ts`, `src/components/app-header.tsx`, `src/components/user-menu.tsx`, `src/app/settings/members/page.tsx`, `src/proxy.ts`, shadcn components as needed (`input`, `label`, `dropdown-menu`, `table`, `badge`; add via `pnpm dlx shadcn@latest add …`)
- Modify: `src/app/page.tsx`, `src/app/layout.tsx` (only if the header belongs there)

**Interfaces:**
- Consumes: `getTenantContext`, `UnauthorizedError`, `ForbiddenError`, `listMembers`, `getAuth` (Task 7).
- Produces: routes `/sign-in`, `/`, `/settings/members`; `src/lib/auth-client.ts` exporting `authClient`.

Read first: `node_modules/next/dist/docs/01-app/02-guides/authentication-with-cache-components.md` and the proxy file convention doc (`infra-facts.md` §3). Under Cache Components, anything reading the session (cookies/headers) sits inside `<Suspense>`.

- [ ] **Step 1: Error messages (unit-tested, pure)** — `error-messages.ts`:

```ts
const MESSAGES: Record<string, string> = {
  unknown_domain: "No organization is configured for this email domain.",
  idp_unavailable: "Your organization's sign-in service is unavailable. Please try again later.",
  email_domain_mismatch: "Your identity provider returned an account that doesn't belong to your organization.",
  account_bound_to_other_provider: "This account belongs to a different organization's sign-in.",
  invalid_request: "The sign-in request was invalid. Please try again.",
};
const FALLBACK = "Sign-in failed. Please try again.";

/** Maps an error code from the URL or API to fixed text; never shows IdP-provided text. */
export function signInErrorMessage(code: string | null | undefined): string | undefined {
  if (!code) return undefined;
  return MESSAGES[code] ?? FALLBACK;
}
```
Tests: known codes map; unknown code and `"unable to create session"` → fallback; empty → undefined.

- [ ] **Step 2: `auth-client.ts`**

```ts
import { ssoClient } from "@better-auth/sso/client";
import { createAuthClient } from "better-auth/react";

export const authClient = createAuthClient({ plugins: [ssoClient()] });
```
(Base URL defaults to the current origin.)

- [ ] **Step 3: Sign-in page** — `page.tsx` (server component): heading "Sign in to Agenty", reads `searchParams.error` inside `<Suspense>` and passes `signInErrorMessage(error)` to `SignInForm`. `sign-in-form.tsx` (client): email `Input` with `Label` "Work email", `Button` "Continue with SSO", on submit `authClient.signIn.sso({ email, callbackURL: "/", errorCallbackURL: "/sign-in" })`; on `{ error }` show `signInErrorMessage(error.code)`; disable the button while pending; error text in an element with `role="alert"`. Zod-validate the email client-side (`z.email()`), message "Enter a valid email address."

- [ ] **Step 4: Header and user menu** — `AppHeader` (async server component, used inside `<Suspense>`): calls `getTenantContext()` and the session for name/email; shows the organization name (`auth.organization.name` looked up by id in `src/server/auth/organizations.ts`, add `getOrganizationName(id)`), a "Members" link for admins, and `UserMenu` (client; name, email, role badge, "Sign out" → `authClient.signOut()` then `router.push("/sign-in")`).

- [ ] **Step 5: Home `/`** — static shell; inside `<Suspense>` an async component: signed out (`UnauthorizedError`) → the M0 landing (keep the `h1` "Agenty" and description) with a "Sign in" button linking to `/sign-in`; signed in → `AppHeader` plus a card "Agents arrive in the next milestone". `ForbiddenError` (no membership / organization removed) → a card "Your account has no active organization. Contact your administrator." plus sign-out.

- [ ] **Step 6: `/settings/members`** — inside `<Suspense>`: `getTenantContext()`; `UnauthorizedError` → `redirect("/sign-in")`; `ForbiddenError` or `role !== "admin"` → `notFound()`. Renders `AppHeader` and a table (Name, Email, Role, Last sign-in formatted with `Intl.DateTimeFormat("en", { dateStyle: "medium", timeStyle: "short" })`).

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
Check `getSessionCookie` honours secure-cookie prefixes (`__Secure-`) the instance uses in https deployments; pass the same `cookiePrefix` options if needed.

- [ ] **Step 8: Verify by hand** — `task dev`; sign in as `alice@acme.test` with claims `{"email":"alice@acme.test","name":"Alice","email_verified":true,"groups":["agenty-admins"]}`; see "Acme", Members link, members table; sign out; sign in as `bob@globex.test` without groups: no Members link, `/settings/members` → 404. Note results in the report.

- [ ] **Step 9: `task ci`; commit** — `git commit -m "feat(ui): SSO sign-in, organization shell and members page"`.

---

### Task 9: E2E tests, Playwright and Docker CI wiring

**Files:**
- Create: `tests/e2e/auth.spec.ts`, `tests/e2e/support/mock-idp.ts`
- Modify: `playwright.config.ts`, `tests/e2e/home.spec.ts` (if the landing changed), `.github/workflows/ci.yml` (job `docker`)

- [ ] **Step 1: Playwright env** — `webServer.env` adds:

```ts
BETTER_AUTH_SECRET: process.env.BETTER_AUTH_SECRET ?? "",
BETTER_AUTH_URL: `http://127.0.0.1:${port}`,
AUTH_CONFIG_FILE: path.resolve("config/agenty.dev.json"),   // server.js changes its cwd
ACME_OIDC_CLIENT_SECRET: process.env.ACME_OIDC_CLIENT_SECRET ?? "",
GLOBEX_OIDC_CLIENT_SECRET: process.env.GLOBEX_OIDC_CLIENT_SECRET ?? "",
```

- [ ] **Step 2: IdP helper** `tests/e2e/support/mock-idp.ts`:

```ts
import type { Page } from "@playwright/test";

/** Completes the mock IdP's login form (it has no labels; fields by name). */
export async function loginAtMockIdp(page: Page, claims: { email: string; name: string; groups?: string[] }) {
  await page.locator("input[name=username]").fill(claims.email);
  await page.locator("textarea[name=claims]").fill(JSON.stringify({ ...claims, email_verified: true }));
  await page.getByRole("button", { name: "Sign-in" }).click();
}

export const uniqueEmail = (prefix: string, domain: string) =>
  `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}@${domain}`;
```

- [ ] **Step 3: `tests/e2e/auth.spec.ts`**

```ts
test("signed-out visitors are sent to sign-in from app pages", async ({ page }) => {
  await page.goto("/settings/members");
  await expect(page).toHaveURL(/\/sign-in$/);
});

test("an Acme admin sees the organization and its members", async ({ page }) => {
  const email = uniqueEmail("admin", "acme.test");
  await page.goto("/sign-in");
  await page.getByLabel("Work email").fill(email);
  await page.getByRole("button", { name: "Continue with SSO" }).click();
  await loginAtMockIdp(page, { email, name: "Ada Admin", groups: ["agenty-admins"] });
  await expect(page.getByText("Acme", { exact: true })).toBeVisible();
  await page.getByRole("link", { name: "Members", exact: true }).click();
  await expect(page.getByRole("cell", { name: email, exact: true })).toBeVisible();
});

test("an Acme member has no members page", async ({ page }) => { /* no Members link; /settings/members → 404 text */ });

test("a Globex user sees only Globex", async ({ page }) => {
  /* sign in admin@globex; members table contains no @acme.test emails; header says Globex */
});

test("an IdP asserting another organization's email is rejected", async ({ page }) => {
  /* sign-in email x@globex.test, at the mock enter email y@acme.test → back on /sign-in with
     the email_domain_mismatch message (role=alert) */
});

test("an unknown email domain shows a readable error", async ({ page }) => { /* eve@evil.test → message */ });

test("sign-out ends the session", async ({ page }) => {
  /* sign in, open user menu, Sign out → /sign-in; visiting /settings/members redirects to /sign-in */
});
```
Fill in the bodies following the first two tests (same selectors and helper).

- [ ] **Step 4: Docker CI job** — in job `docker`: add the `mock-oidc` service (as in job `ci`) and the wait step; the run command gains

```sh
-v "$PWD/config/agenty.dev.json:/config/agenty.json:ro" \
-e AUTH_CONFIG_FILE=/config/agenty.json \
-e BETTER_AUTH_SECRET=ci-only-secret-0123456789abcdef0123 \
-e BETTER_AUTH_URL=http://localhost:3000 \
-e ACME_OIDC_CLIENT_SECRET=ci -e GLOBEX_OIDC_CLIENT_SECRET=ci \
```
(Host networking keeps `http://localhost:8080` identical for app and IdP.)

- [ ] **Step 5: `task ci`** (E2E included) → green; commit `git commit -m "test(e2e): SSO sign-in flows against the mock IdP"`.

---

### Task 10: Documentation and final verification

**Files:**
- Modify: `AGENTS.md`, `README.md`, `.env.example` (comments only if needed)

- [ ] **Step 1: AGENTS.md** — update:
  - intro: SSO-only (OIDC), organization = tenant, workspaces later.
  - Commands: `task db:up` also starts the mock IdP; `task auth:generate`.
  - Architecture tree: `src/server/auth/` modules (config, discovery, organizations, auth, provisioning, tenant, members, tenant-context), `config/`, `src/proxy.ts`, `tests/support/sso.ts`.
  - Database: schema `auth` (tables, no RLS, why, import restriction, accepted risk), our `organization`/`member` tables, sync at start and issuer pinning; remove the "M1 must decide" notes.
  - New section "Auth": config file format (link to `config/agenty.dev.json`), lazy discovery and `privateNetwork`, endpoint and sign-in body allowlist, provisioning in `resolveUser` → `session.create.before` (transaction adapter, no fallback), 12 h sessions, `getTenantContext()`.
  - Rule 1 (tenant isolation) concretised: every domain table uses `tenantId()` + `tenantIsolation()`, all access through `withTenant(getTenantContext())`; the catalog guard enforces it.
  - Version notes: Better Auth / sso pinned to the same exact version; mock IdP tag.
- [ ] **Step 2: README** — production configuration: env vars (`BETTER_AUTH_SECRET`, `BETTER_AUTH_URL`, `AUTH_CONFIG_FILE` absolute path, client-secret env vars), config file reference, registering Agenty at an IdP (redirect URI `<BETTER_AUTH_URL>/api/auth/sso/callback/<slug>`, scopes, a groups claim **in the ID token**), https recommended, `privateNetwork` and that trusted origins are also redirect targets, 12 h deprovisioning lag, removing an organization (disables, keeps data), changing an organization's IdP (immutable issuer; manual steps: delete that organization's `auth.account` rows, update `auth.organization.issuer`, then restart), dev sign-in with the mock IdP (claims JSON example). Troubleshooting: port 8080 in use.
- [ ] **Step 3: Final verification** — `task db:reset && task db:up && task ci` → green; `task docker:build` and the Docker run command from CI locally → health ok. Commit `git commit -m "docs: SSO, organizations and RLS in AGENTS.md and README"`.
