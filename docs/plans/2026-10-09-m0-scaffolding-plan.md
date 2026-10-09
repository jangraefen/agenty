# M0 – Scaffolding Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A complete, empty Agenty skeleton (Next.js standalone app, Postgres with two roles, OPA→WASM toolchain, Vitest, Playwright, Taskfile, Docker, CI) where `task ci` passes locally and in GitHub Actions.

**Architecture:** One Next.js 16 App Router app running as a standalone Node server, talking to Postgres through Drizzle with a runtime role that cannot bypass RLS. Shared shell scripts hold build logic so the Taskfile and the Dockerfile run the same commands. Rego policies compile to WASM at build time and are evaluated in-process.

**Tech Stack:** Node 24, pnpm 12, go-task 3, Next.js 16.4, React 19.3, TypeScript (strict), Tailwind 4.3, shadcn 4.21, TanStack Query 5, Zod 4, Biome 2.5.15, Drizzle ORM 0.45 + drizzle-kit 0.31 + postgres.js 3.4, OPA 1.21.1 + `@open-policy-agent/opa-wasm` 1.10, Vitest 5, Playwright 1.64, Postgres 18, esbuild.

**Spec:** `docs/specs/2026-10-09-m0-scaffolding-design.md`

## Global Constraints

- Never read or reference the git branch `legacy`.
- Node 24 LTS: `.nvmrc` = `24`, `engines.node` = `>=24`, Docker base `node:24-bookworm-slim`, CI `node-version-file: .nvmrc`.
- pnpm only, pinned via `packageManager`; dependency build scripts allowed explicitly in `pnpm-workspace.yaml` (`allowBuilds`).
- Biome only (exact version `2.5.15`); no ESLint, no Prettier, no `.eslintrc`, no `.prettierrc`.
- TypeScript `strict` + `noUncheckedIndexedAccess`.
- English only, no i18n.
- Zod at every boundary (env, route responses, policy results).
- Runtime DB role `agenty_app`: never superuser, never `BYPASSRLS`, owns nothing. Migrations run as `agenty_owner` only.
- No secrets in logs or response bodies; error messages must not echo connection strings.
- Every command runs through `Taskfile.yml`; CI calls `task setup` and `task ci`.
- Before adding or configuring a library, check its current docs (context7 or official site). Versions listed here were current on 2026-10-09; use `pnpm add <pkg>@latest` unless a version is pinned below.
- Stage files with explicit paths, never `git add -A` / `git add .`.
- Commit messages end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **Invalid `DATABASE_URL` (contains a password)** → startup fails with a readable message that does **not** contain the URL or password. Test in Task 3.
2. **Database down while the server runs** → `/api/health` returns `503 {status:"error",db:"unavailable"}` and the body contains no error details. Test in Task 5.
3. **Policy WASM returns an empty or malformed result set** (undefined `decision`, wrong types) → `evaluatePolicy` throws (fail closed), never returns an allow. Test in Task 6.
4. **A future migration run as the wrong role / a table created by someone else** → integration test fails because a table in `app` is not owned by `agenty_owner`, or default privileges don't reach `agenty_app`. Test in Task 4.
5. **E2E accidentally testing a running dev server** → Playwright starts its own standalone server on port 3100 with `reuseExistingServer: false`; a server already on 3100 makes the run fail instead of being reused. Config in Task 7.

---

## File Structure

| Path | Responsibility | Task |
|---|---|---|
| `package.json`, `pnpm-lock.yaml`, `pnpm-workspace.yaml`, `.nvmrc`, `tsconfig.json`, `next.config.ts`, `biome.json`, `.gitignore` | Project/tool configuration | 1 |
| `Taskfile.yml` | All developer/CI commands (grows per task) | 1, 3–9 |
| `src/app/layout.tsx`, `src/app/page.tsx`, `src/app/providers.tsx`, `src/app/globals.css` | App shell, home page, client providers | 2 |
| `components.json`, `src/components/ui/*`, `src/lib/utils.ts` | shadcn setup and generated components | 2 |
| `vitest.config.ts`, `tests/support/empty.ts` | Test runner config; `server-only` stub | 3 |
| `src/server/env.ts`, `src/instrumentation.ts` | Lazy Zod-validated env; startup validation | 3 |
| `docker-compose.yml`, `docker/postgres/init.sql`, `.env.example` | Local Postgres and roles | 4 |
| `drizzle.config.ts`, `src/server/db/schema.ts`, `src/server/db/client.ts`, `src/server/db/migrations/*` | Drizzle setup and migrations | 4 |
| `scripts/migrate.mjs`, `scripts/check-db.mjs` | Apply migrations as owner; DB reachability check | 4 |
| `src/server/db/roles.int.test.ts` | Role/privilege guarantees | 4 |
| `src/server/health.ts`, `src/app/api/health/route.ts` | Health check logic and thin route | 5 |
| `policies/agenty/*.rego`, `scripts/build-policy.sh`, `src/server/policy/evaluate.ts` | Policy source, WASM build, evaluation | 6 |
| `scripts/build.sh`, `playwright.config.ts`, `tests/e2e/*.spec.ts` | Production build incl. standalone assembly; E2E | 7 |
| `Dockerfile`, `.dockerignore` | Container image | 8 |
| `.github/workflows/ci.yml` | CI | 9 |
| `CLAUDE.md`, `README.md` | Docs | 10 |

---

### Task 1: Next.js app, Biome, TypeScript and Taskfile skeleton

**Files:**
- Create: `package.json`, `pnpm-lock.yaml`, `pnpm-workspace.yaml`, `.nvmrc`, `tsconfig.json`, `next.config.ts`, `biome.json`, `.gitignore`, `Taskfile.yml`, `src/app/layout.tsx`, `src/app/page.tsx`, `src/app/globals.css`, `src/app/favicon.ico`

**Interfaces:**
- Produces: tasks `setup`, `dev`, `lint`, `typecheck`, `build` (temporary simple version, replaced in Task 7); path alias `@/*` → `src/*`.

- [ ] **Step 1: Scaffold into a temporary directory and copy in**

The repo root is not empty (`docs/`), so scaffold elsewhere:

```bash
SCRATCH=$(mktemp -d)
pnpm dlx create-next-app@latest "$SCRATCH/app" --ts --tailwind --biome --app --src-dir \
  --import-alias "@/*" --use-pnpm --no-agents-md --no-agent-feedback --disable-git --skip-install --yes
rm -rf "$SCRATCH/app/public" "$SCRATCH/app/README.md"
cp -R "$SCRATCH/app/." .
```

- [ ] **Step 2: Adjust `package.json`**

Set `"name": "agenty"`, keep `"private": true`, delete `"version"`, add `"engines": { "node": ">=24" }`, replace `"scripts"` with `{}` (all commands live in the Taskfile). Keep the `packageManager` field create-next-app wrote. Then:

```bash
pnpm add -D --save-exact @biomejs/biome@2.5.15
pnpm add -D @types/node@24 typescript@latest
echo 24 > .nvmrc
```

- [ ] **Step 3: TypeScript config**

In `tsconfig.json` set `"target": "ES2022"` and add `"noUncheckedIndexedAccess": true` to `compilerOptions`. Keep everything else generated.

- [ ] **Step 4: Next config**

`next.config.ts` (keep whatever turbopack/Tailwind loader config create-next-app generated, plus):

```ts
import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  output: "standalone",
  cacheComponents: true,
  partialPrefetching: true,
  turbopack: {
    rules: {
      "*.css": {
        loaders: ["@tailwindcss/turbopack"],
        as: "*.css",
      },
    },
  },
};

export default nextConfig;
```

- [ ] **Step 5: Biome config**

`biome.json`:

```json
{
  "$schema": "https://biomejs.dev/schemas/2.5.15/schema.json",
  "vcs": { "enabled": true, "clientKind": "git", "useIgnoreFile": true },
  "files": {
    "ignoreUnknown": true,
    "includes": ["**", "!node_modules", "!.next", "!build", "!src/server/db/migrations"]
  },
  "formatter": { "enabled": true, "indentStyle": "space", "indentWidth": 2, "lineWidth": 100 },
  "css": { "parser": { "tailwindDirectives": true } },
  "linter": {
    "enabled": true,
    "rules": {
      "recommended": true,
      "nursery": {
        "useSortedClasses": {
          "level": "error",
          "options": { "attributes": ["className"], "functions": ["cn", "cva"] }
        }
      }
    },
    "domains": { "next": "recommended", "react": "recommended" }
  },
  "assist": { "actions": { "source": { "organizeImports": "on" } } }
}
```

Check the option names against https://biomejs.dev/linter/rules/use-sorted-classes/ before committing.

- [ ] **Step 6: `.gitignore` additions**

Append to the generated file (it already ignores `/build`, `/.next/`, `.env*`, `next-env.d.ts`):

```gitignore
# keep the example env file
!.env.example

# tools downloaded by `task setup`
/.tools/

# playwright
/test-results/
/playwright-report/
```

- [ ] **Step 7: Home page and layout without network fonts**

`next/font/google` downloads fonts at build time, which breaks offline/Docker builds. Use the system font stack instead.

`src/app/layout.tsx`:

```tsx
import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Agenty",
  description: "Self-hosted AI agents for your organization.",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en" className="h-full antialiased">
      <body className="flex min-h-full flex-col">{children}</body>
    </html>
  );
}
```

`src/app/page.tsx` (temporary; replaced with shadcn in Task 2):

```tsx
export default function HomePage() {
  return (
    <main className="mx-auto flex w-full max-w-2xl flex-1 flex-col justify-center gap-4 p-8">
      <h1 className="font-semibold text-4xl tracking-tight">Agenty</h1>
      <p className="text-lg">Self-hosted AI agents for your organization.</p>
    </main>
  );
}
```

In `src/app/globals.css` remove the `--font-geist-*` references and set `body { font-family: ui-sans-serif, system-ui, sans-serif; }`.

- [ ] **Step 8: `pnpm-workspace.yaml` build approvals**

Run `pnpm install`. For every package pnpm reports as having ignored build scripts, decide explicitly in `pnpm-workspace.yaml`:

```yaml
allowBuilds:
  sharp: false
  unrs-resolver: false
```

(`sharp` is not needed: no `next/image` optimisation in M0. Add further entries as later tasks introduce packages; `esbuild: true` arrives in Task 7.)

- [ ] **Step 9: Taskfile skeleton**

`Taskfile.yml`:

```yaml
# Run `task --list` to see all tasks. Every command, locally and in CI, goes through this file.
version: "3"

dotenv: [".env"]

vars:
  OPA_VERSION: v1.21.1

env:
  PATH: "{{.ROOT_DIR}}/.tools/bin:{{env \"PATH\"}}"
  NEXT_TELEMETRY_DISABLED: "1"

tasks:
  setup:
    desc: Install dependencies and tools (opa, Playwright browser); create .env
    cmds:
      - pnpm install --frozen-lockfile

  dev:
    desc: Run the app in development mode
    cmds:
      - pnpm exec next dev

  lint:
    desc: Lint and check formatting (Biome)
    cmds:
      - pnpm exec biome ci .

  format:
    desc: Apply formatting and safe+unsafe Biome fixes (incl. Tailwind class sorting)
    cmds:
      - pnpm exec biome check --write --unsafe .

  typecheck:
    desc: Type-check the project
    cmds:
      - pnpm exec next typegen
      - pnpm exec tsc --noEmit

  build:
    desc: Production build (standalone)
    run: once
    cmds:
      - pnpm exec next build
```

- [ ] **Step 10: Verify**

```bash
task format && task lint && task typecheck && task build
```

Expected: all succeed; `.next/standalone/server.js` exists.

If `tsc`/`next build` fail because of the TypeScript major (TypeScript 7 is the native compiler and may not yet be supported by Next.js), run `pnpm add -D typescript@^6` and re-verify; record the reason in `CLAUDE.md` (Task 10) under "Version notes".

- [ ] **Step 11: Commit**

```bash
git add package.json pnpm-lock.yaml pnpm-workspace.yaml .nvmrc tsconfig.json next.config.ts biome.json .gitignore Taskfile.yml src/app
git commit -m "chore: scaffold Next.js app with Biome and Taskfile"
```

---

### Task 2: shadcn/ui, TanStack Query provider, home page

**Files:**
- Create: `components.json`, `src/components/ui/card.tsx`, `src/components/ui/button.tsx`, `src/lib/utils.ts`, `src/app/providers.tsx`
- Modify: `src/app/globals.css`, `src/app/layout.tsx`, `src/app/page.tsx`, `package.json`, `pnpm-lock.yaml`

**Interfaces:**
- Produces: `cn(...inputs: ClassValue[]): string` from `@/lib/utils`; `Providers` from `@/app/providers`; home page `<h1>` with text `Agenty` (E2E relies on it).

- [ ] **Step 1: Initialise shadcn**

Check https://ui.shadcn.com/docs/installation/next first. Then:

```bash
pnpm dlx shadcn@latest init -d
pnpm dlx shadcn@latest add card button
```

- [ ] **Step 2: TanStack Query provider**

```bash
pnpm add @tanstack/react-query
```

`src/app/providers.tsx`:

```tsx
"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { type ReactNode, useState } from "react";

export function Providers({ children }: { children: ReactNode }) {
  const [queryClient] = useState(
    () => new QueryClient({ defaultOptions: { queries: { staleTime: 60_000 } } }),
  );
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}
```

In `src/app/layout.tsx` wrap `{children}`: `<body ...><Providers>{children}</Providers></body>` (import from `./providers`).

- [ ] **Step 3: Home page with shadcn components**

`src/app/page.tsx`:

```tsx
import { buttonVariants } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

export default function HomePage() {
  return (
    <main className="mx-auto flex w-full max-w-2xl flex-1 flex-col justify-center gap-6 p-8">
      <div className="flex flex-col gap-2">
        <h1 className="font-semibold text-4xl tracking-tight">Agenty</h1>
        <p className="text-lg text-muted-foreground">
          Self-hosted AI agents for your organization.
        </p>
      </div>
      <Card>
        <CardHeader>
          <CardTitle>Under construction</CardTitle>
          <CardDescription>
            Sign-in, agents and chat arrive in the next milestones.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <a className={buttonVariants({ variant: "outline" })} href="/api/health">
            Check server health
          </a>
        </CardContent>
      </Card>
    </main>
  );
}
```

If the generated `button.tsx` does not export `buttonVariants`, use `<Button>` with the composition prop the generated component documents (`render` for Base UI, `asChild` for Radix) instead.

- [ ] **Step 4: Verify**

```bash
task format && task lint && task typecheck && task build
```

Expected: all pass. Generated shadcn files are formatted by Biome and stay committed formatted.

- [ ] **Step 5: Commit**

```bash
git add components.json src/components src/lib src/app package.json pnpm-lock.yaml
git commit -m "feat: add shadcn/ui, TanStack Query provider and home page"
```

---

### Task 3: Vitest, lazy env validation, startup check

**Files:**
- Create: `vitest.config.ts`, `tests/support/empty.ts`, `src/server/env.ts`, `src/server/env.test.ts`, `src/lib/utils.test.ts`, `src/instrumentation.ts`
- Modify: `Taskfile.yml`, `package.json`, `pnpm-lock.yaml`

**Interfaces:**
- Produces: `parseEnv(source: Record<string, string | undefined>): Env`, `getEnv(): Env`, `type Env = { DATABASE_URL: string; NODE_ENV: "development" | "test" | "production" }` from `@/server/env`. Test file conventions: `*.test.ts` = unit, `*.int.test.ts` = integration.

- [ ] **Step 1: Install**

```bash
pnpm add zod server-only
pnpm add -D vitest vite-tsconfig-paths
```

- [ ] **Step 2: Vitest config**

`tests/support/empty.ts`:

```ts
// Stand-in for `server-only` under Vitest: its real entry throws outside the react-server condition.
export {};
```

`vitest.config.ts`:

```ts
import { fileURLToPath } from "node:url";
import tsconfigPaths from "vite-tsconfig-paths";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [tsconfigPaths()],
  resolve: {
    alias: {
      "server-only": fileURLToPath(new URL("./tests/support/empty.ts", import.meta.url)),
    },
  },
  test: {
    projects: [
      {
        extends: true,
        test: {
          name: "unit",
          environment: "node",
          include: ["src/**/*.test.ts"],
          exclude: ["src/**/*.int.test.ts"],
        },
      },
      {
        extends: true,
        test: {
          name: "integration",
          environment: "node",
          include: ["src/**/*.int.test.ts"],
          testTimeout: 15_000,
        },
      },
    ],
  },
});
```

Check https://vitest.dev/guide/projects for the current `projects` syntax.

- [ ] **Step 3: Write failing tests**

`src/lib/utils.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { cn } from "./utils";

describe("cn", () => {
  it("lets later Tailwind classes win", () => {
    expect(cn("px-2 py-1", "px-4")).toBe("py-1 px-4");
  });

  it("drops falsy values", () => {
    expect(cn("a", false, undefined, null, "b")).toBe("a b");
  });
});
```

`src/server/env.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { parseEnv } from "./env";

const valid = { DATABASE_URL: "postgres://agenty_app:s3cret@localhost:5432/agenty" };

describe("parseEnv", () => {
  it("accepts a postgres URL and defaults NODE_ENV", () => {
    expect(parseEnv(valid)).toEqual({ ...valid, NODE_ENV: "development" });
  });

  it("accepts the postgresql:// scheme", () => {
    const env = { DATABASE_URL: "postgresql://u:p@db:5432/agenty" };
    expect(parseEnv(env).DATABASE_URL).toBe(env.DATABASE_URL);
  });

  it("rejects a missing DATABASE_URL with a readable message", () => {
    expect(() => parseEnv({})).toThrow(/DATABASE_URL/);
  });

  it("rejects a non-postgres URL", () => {
    expect(() => parseEnv({ DATABASE_URL: "mysql://u:p@db/agenty" })).toThrow(/DATABASE_URL/);
  });

  it("never echoes the value (it contains a password) in the error", () => {
    const secret = "mysql://agenty_app:hunter2-very-secret@db/agenty";
    let message = "";
    try {
      parseEnv({ DATABASE_URL: secret });
    } catch (error) {
      message = (error as Error).message;
    }
    expect(message).not.toBe("");
    expect(message).not.toContain("hunter2");
    expect(message).not.toContain(secret);
  });

  it("rejects an unknown NODE_ENV", () => {
    expect(() => parseEnv({ ...valid, NODE_ENV: "staging" })).toThrow(/NODE_ENV/);
  });
});
```

- [ ] **Step 4: Add the test task and run to see failures**

Add to `Taskfile.yml`:

```yaml
  test:
    desc: Run unit and integration tests (integration needs `task db:up`)
    cmds:
      - pnpm exec vitest run {{.CLI_ARGS}}
```

Run: `task test -- --project unit`
Expected: FAIL — `./env` cannot be resolved (the `cn` tests pass, since shadcn created `utils.ts`).

- [ ] **Step 5: Implement `src/server/env.ts`**

```ts
import "server-only";
import { z } from "zod";

const envSchema = z.object({
  DATABASE_URL: z.url({ protocol: /^postgres(ql)?$/, error: "must be a postgres:// URL" }),
  NODE_ENV: z.enum(["development", "test", "production"]).default("development"),
});

export type Env = z.infer<typeof envSchema>;

/** Parses configuration. The error lists field paths and messages only, never values. */
export function parseEnv(source: Record<string, string | undefined>): Env {
  const result = envSchema.safeParse(source);
  if (!result.success) {
    const problems = result.error.issues
      .map((issue) => `  - ${issue.path.join(".")}: ${issue.message}`)
      .join("\n");
    throw new Error(`Invalid environment configuration:\n${problems}`);
  }
  return result.data;
}

let cached: Env | undefined;

/** Parsed lazily so `next build` never needs runtime configuration. */
export function getEnv(): Env {
  cached ??= parseEnv(process.env);
  return cached;
}
```

(Zod issue messages may embed the received input for some checks; building the message from `path` + `message` and the test in Step 3 guard against leaking it. If a Zod message ever contains the value, replace it with a fixed message per field.)

- [ ] **Step 6: Run tests**

Run: `task test -- --project unit`
Expected: PASS (8 tests).

- [ ] **Step 7: Startup validation**

`src/instrumentation.ts`:

```ts
export async function register() {
  // Validate configuration when the Node.js server starts, but not while `next build` runs.
  if (process.env.NEXT_RUNTIME === "nodejs" && process.env.NEXT_PHASE !== "phase-production-build") {
    const { getEnv } = await import("./server/env");
    getEnv();
  }
}
```

- [ ] **Step 8: Verify the build needs no env and the server fails fast**

```bash
task build
env -u DATABASE_URL node .next/standalone/server.js; echo "exit=$?"
```

Expected: build passes without `DATABASE_URL`; the server exits non-zero printing `Invalid environment configuration:` and `DATABASE_URL`. If it instead starts, check https://nextjs.org/docs/app/guides/instrumentation and make `register()` failures abort startup (e.g. log and `process.exit(1)`).

- [ ] **Step 9: Lint, typecheck, commit**

```bash
task lint && task typecheck
git add vitest.config.ts tests/support src/server/env.ts src/server/env.test.ts src/lib/utils.test.ts src/instrumentation.ts Taskfile.yml package.json pnpm-lock.yaml
git commit -m "feat: add Vitest and lazily validated environment"
```

---

### Task 4: Postgres, roles, Drizzle migrations and role guarantees

**Files:**
- Create: `docker-compose.yml`, `docker/postgres/init.sql`, `.env.example`, `drizzle.config.ts`, `src/server/db/schema.ts`, `src/server/db/client.ts`, `src/server/db/migrations/*` (generated), `scripts/migrate.mjs`, `scripts/check-db.mjs`, `src/server/db/roles.int.test.ts`
- Modify: `Taskfile.yml`, `package.json`, `pnpm-lock.yaml`

**Interfaces:**
- Consumes: `getEnv()` from Task 3.
- Produces: `getDb()` returning a Drizzle `PostgresJsDatabase` bound to `DATABASE_URL`; `app = pgSchema("app")` from `@/server/db/schema`; tasks `db:up`, `db:down`, `db:reset`, `db:generate`, `db:migrate`, `db:studio`, `db:check`; `scripts/migrate.mjs` resolves migrations at `../src/server/db/migrations` relative to itself (Task 7 relies on this layout inside `.next/standalone`).

- [ ] **Step 1: Install**

```bash
pnpm add drizzle-orm postgres
pnpm add -D drizzle-kit
```

- [ ] **Step 2: Compose, init script, env example**

`docker/postgres/init.sql`:

```sql
-- Creates Agenty's database roles. Runs once as the superuser: by the postgres image on an
-- empty data directory, and by CI via psql. Passwords are local-development values.
-- Schemas and grants are created by migrations (src/server/db/migrations), not here.
DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'agenty_owner') THEN
    CREATE ROLE agenty_owner LOGIN PASSWORD 'agenty_owner'
      NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'agenty_app') THEN
    CREATE ROLE agenty_app LOGIN PASSWORD 'agenty_app'
      NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;
  END IF;
END
$$;

-- The migrator runs CREATE SCHEMA IF NOT EXISTS, which needs CREATE on the database.
SELECT format('GRANT CREATE ON DATABASE %I TO agenty_owner', current_database()) \gexec
```

`docker-compose.yml`:

```yaml
services:
  postgres:
    image: postgres:18
    environment:
      POSTGRES_USER: ${POSTGRES_USER:-postgres}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-postgres}
      POSTGRES_DB: ${POSTGRES_DB:-agenty}
    ports:
      - "127.0.0.1:${POSTGRES_PORT:-5432}:5432"
    volumes:
      - postgres-data:/var/lib/postgresql
      - ./docker/postgres/init.sql:/docker-entrypoint-initdb.d/10-roles.sql:ro
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U $${POSTGRES_USER} -d $${POSTGRES_DB}"]
      interval: 2s
      timeout: 5s
      retries: 30

volumes:
  postgres-data:
```

`.env.example`:

```dotenv
# Local development defaults (not secrets). `task setup` copies this file to .env.

# Superuser of the local/CI Postgres; only used by docker compose and docker/postgres/init.sql.
POSTGRES_USER=postgres
POSTGRES_PASSWORD=postgres
POSTGRES_DB=agenty
POSTGRES_PORT=5432

# Runtime connection: role agenty_app (no superuser, no BYPASSRLS, owns nothing).
DATABASE_URL=postgres://agenty_app:agenty_app@localhost:5432/agenty
# Migrations: role agenty_owner (owns schema app and its tables).
DATABASE_MIGRATION_URL=postgres://agenty_owner:agenty_owner@localhost:5432/agenty
```

Then `cp .env.example .env`.

- [ ] **Step 3: Drizzle config, schema, client**

`drizzle.config.ts`:

```ts
import { defineConfig } from "drizzle-kit";

export default defineConfig({
  dialect: "postgresql",
  schema: "./src/server/db/schema.ts",
  out: "./src/server/db/migrations",
  schemaFilter: ["app"],
  dbCredentials: { url: process.env.DATABASE_MIGRATION_URL ?? "" },
  strict: true,
  verbose: true,
});
```

`src/server/db/schema.ts`:

```ts
import { pgSchema } from "drizzle-orm/pg-core";

/** All Agenty tables live in schema `app`, owned by role agenty_owner. */
export const app = pgSchema("app");
```

`src/server/db/client.ts`:

```ts
import "server-only";
import { drizzle } from "drizzle-orm/postgres-js";
import postgres from "postgres";
import { getEnv } from "@/server/env";
import * as schema from "./schema";

function createDb() {
  const client = postgres(getEnv().DATABASE_URL, { max: 10, onnotice: () => {} });
  return drizzle({ client, schema });
}

let db: ReturnType<typeof createDb> | undefined;

/** The shared connection pool, as role agenty_app. Created on first use. */
export function getDb() {
  db ??= createDb();
  return db;
}
```

- [ ] **Step 4: Scripts**

`scripts/migrate.mjs`:

```js
// Applies Drizzle migrations as the owner role. Used by `task db:migrate` and, bundled, in the
// Docker image (`node scripts/migrate.mjs`). Migrations are found relative to this file.
import { fileURLToPath } from "node:url";
import { drizzle } from "drizzle-orm/postgres-js";
import { migrate } from "drizzle-orm/postgres-js/migrator";
import postgres from "postgres";

const url = process.env.DATABASE_MIGRATION_URL;
if (!url) {
  console.error("DATABASE_MIGRATION_URL is not set.");
  process.exit(1);
}

const migrationsFolder = fileURLToPath(new URL("../src/server/db/migrations", import.meta.url));
const client = postgres(url, { max: 1, onnotice: () => {} });

try {
  await migrate(drizzle({ client }), { migrationsFolder });
  console.log("Migrations applied.");
} catch (error) {
  console.error("Migration failed:", error instanceof Error ? error.message : error);
  process.exitCode = 1;
} finally {
  await client.end();
}
```

`scripts/check-db.mjs`:

```js
// Fails fast with a hint when the database behind DATABASE_URL is unreachable.
import postgres from "postgres";

const url = process.env.DATABASE_URL;
if (!url) {
  console.error("DATABASE_URL is not set. Run `task setup` to create .env from .env.example.");
  process.exit(1);
}

const sql = postgres(url, { max: 1, connect_timeout: 5, onnotice: () => {} });
try {
  await sql`select 1`;
} catch (error) {
  console.error(
    `Database not reachable (${error instanceof Error ? error.message : "unknown error"}).`,
    "Start it with `task db:up`.",
  );
  process.exitCode = 1;
} finally {
  await sql.end();
}
```

- [ ] **Step 5: Taskfile database tasks**

```yaml
  db:up:
    desc: Start local Postgres and wait until healthy
    cmds:
      - docker compose up --detach --wait

  db:down:
    desc: Stop local Postgres (keeps data)
    cmds:
      - docker compose down

  db:reset:
    desc: Stop local Postgres and delete its data (needed after changing docker/postgres/init.sql)
    cmds:
      - docker compose down --volumes

  db:check:
    desc: Fail fast if DATABASE_URL is unreachable
    cmds:
      - node scripts/check-db.mjs

  db:generate:
    desc: Generate a migration from src/server/db/schema.ts (pass `-- --custom --name x` for SQL)
    cmds:
      - pnpm exec drizzle-kit generate {{.CLI_ARGS}}

  db:migrate:
    desc: Apply migrations as agenty_owner
    cmds:
      - node scripts/migrate.mjs

  db:studio:
    desc: Open Drizzle Studio (as agenty_owner)
    cmds:
      - pnpm exec drizzle-kit studio
```

Also extend `setup` (after `pnpm install`):

```yaml
      - cmd: test -f .env || cp .env.example .env
```

And make `dev` start the database first:

```yaml
  dev:
    desc: Run the app in development mode (starts Postgres, applies migrations)
    cmds:
      - task: db:up
      - task: db:migrate
      - pnpm exec next dev
```

- [ ] **Step 6: Generate migrations**

```bash
task db:generate -- --name app_schema
task db:generate -- --custom --name app_grants
```

Expected: `0000_app_schema.sql` contains exactly `CREATE SCHEMA "app";`. Fill `0001_app_grants.sql`:

```sql
-- The runtime role may use schema app and read/write tables and sequences agenty_owner creates
-- there. It cannot create objects, and owns nothing, so RLS always applies to it.
GRANT USAGE ON SCHEMA "app" TO agenty_app;
--> statement-breakpoint
ALTER DEFAULT PRIVILEGES FOR ROLE agenty_owner IN SCHEMA "app"
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO agenty_app;
--> statement-breakpoint
ALTER DEFAULT PRIVILEGES FOR ROLE agenty_owner IN SCHEMA "app"
  GRANT USAGE, SELECT ON SEQUENCES TO agenty_app;
```

Run `task db:generate` once more. Expected: "No schema changes, nothing to migrate".

- [ ] **Step 7: Write the failing integration test**

`src/server/db/roles.int.test.ts`:

```ts
import postgres from "postgres";
import { afterAll, describe, expect, it } from "vitest";

function requireEnv(name: string): string {
  const value = process.env[name];
  if (!value) throw new Error(`${name} must be set (run tests via \`task test\`)`);
  return value;
}

const appSql = postgres(requireEnv("DATABASE_URL"), { max: 1, onnotice: () => {} });
const ownerSql = postgres(requireEnv("DATABASE_MIGRATION_URL"), { max: 1, onnotice: () => {} });

afterAll(async () => {
  await Promise.all([appSql.end(), ownerSql.end()]);
});

class Rollback extends Error {}

describe("runtime role (DATABASE_URL)", () => {
  it("is agenty_app, neither superuser nor BYPASSRLS", async () => {
    const rows = await appSql`
      select rolname, rolsuper, rolbypassrls from pg_roles where rolname = current_user`;
    expect(rows).toEqual([{ rolname: "agenty_app", rolsuper: false, rolbypassrls: false }]);
  });

  it("cannot create objects in schema app", async () => {
    const [row] = await appSql`select has_schema_privilege(current_user, 'app', 'CREATE') as ok`;
    expect(row?.ok).toBe(false);
  });

  it("cannot read the migration bookkeeping", async () => {
    await expect(appSql`select * from drizzle.__drizzle_migrations`).rejects.toThrow(
      /permission denied/,
    );
  });
});

describe("schema app ownership", () => {
  it("schema app is owned by agenty_owner", async () => {
    const [row] = await appSql`
      select nspowner::regrole::text as owner from pg_namespace where nspname = 'app'`;
    expect(row?.owner).toBe("agenty_owner");
  });

  it("every table in app is owned by agenty_owner", async () => {
    const foreign = await appSql`
      select tablename, tableowner from pg_tables
      where schemaname = 'app' and tableowner <> 'agenty_owner'`;
    expect(foreign).toEqual([]);
  });

  it("tables agenty_owner creates are readable and writable by agenty_app", async () => {
    const privileges = ownerSql.begin(async (tx) => {
      await tx`create table app.__grant_probe (id integer)`;
      const [row] = await tx`
        select
          has_table_privilege('agenty_app', 'app.__grant_probe', 'SELECT') as can_select,
          has_table_privilege('agenty_app', 'app.__grant_probe', 'INSERT') as can_insert,
          has_table_privilege('agenty_app', 'app.__grant_probe', 'UPDATE') as can_update,
          has_table_privilege('agenty_app', 'app.__grant_probe', 'DELETE') as can_delete,
          has_table_privilege('agenty_app', 'app.__grant_probe', 'TRUNCATE') as can_truncate`;
      throw new Rollback(JSON.stringify(row));
    });
    const error = await privileges.catch((e: unknown) => e);
    expect(error).toBeInstanceOf(Rollback);
    expect(JSON.parse((error as Rollback).message)).toEqual({
      can_select: true,
      can_insert: true,
      can_update: true,
      can_delete: true,
      can_truncate: false,
    });
  });
});
```

(`has_table_privilege` with a comma list returns true if *any* privilege is held, hence one column per privilege. This functional probe replaces a direct `pg_default_acl` assertion: it tests the effect, not the catalog encoding.)

- [ ] **Step 8: Run against a migrated database**

```bash
task db:up
task db:check
task test -- --project integration
```

Expected before migrating: FAIL (schema `app` does not exist). Then:

```bash
task db:migrate && task db:migrate   # second run must be a no-op
task test -- --project integration
```

Expected: PASS (6 tests).

- [ ] **Step 9: Mutation check**

Temporarily comment out the `GRANT ... ON TABLES` statement in `0001_app_grants.sql`, run `task db:reset && task db:up && task db:migrate && task test -- --project integration`; expected: the probe test FAILS. Restore the file, `task db:reset && task db:up && task db:migrate`, re-run: PASS.

- [ ] **Step 10: Lint, typecheck, commit**

```bash
task lint && task typecheck
git add docker-compose.yml docker/postgres/init.sql .env.example drizzle.config.ts src/server/db scripts/migrate.mjs scripts/check-db.mjs Taskfile.yml package.json pnpm-lock.yaml
git commit -m "feat: add Postgres roles, Drizzle migrations and role guarantee tests"
```

---

### Task 5: Health check route

**Files:**
- Create: `src/server/health.ts`, `src/server/health.test.ts`, `src/server/health.int.test.ts`, `src/app/api/health/route.ts`

**Interfaces:**
- Consumes: `getDb()` from Task 4.
- Produces: `GET /api/health` → `200 {"status":"ok","db":"ok"}` or `503 {"status":"error","db":"unavailable"}`, both with `cache-control: no-store`. `checkHealth(ping?: () => Promise<unknown>): Promise<{ httpStatus: 200 | 503; body: HealthBody }>`.

- [ ] **Step 1: Write failing unit tests**

`src/server/health.test.ts`:

```ts
import { afterEach, describe, expect, it, vi } from "vitest";
import { checkHealth } from "./health";

afterEach(() => {
  vi.restoreAllMocks();
});

describe("checkHealth", () => {
  it("reports ok when the database answers", async () => {
    await expect(checkHealth(async () => [{ "?column?": 1 }])).resolves.toEqual({
      httpStatus: 200,
      body: { status: "ok", db: "ok" },
    });
  });

  it("reports 503 without leaking error details when the database fails", async () => {
    const log = vi.spyOn(console, "error").mockImplementation(() => {});
    const result = await checkHealth(async () => {
      throw new Error("password authentication failed for user agenty_app at 10.0.0.5");
    });
    expect(result).toEqual({ httpStatus: 503, body: { status: "error", db: "unavailable" } });
    expect(JSON.stringify(result)).not.toContain("password");
    expect(log).toHaveBeenCalledOnce();
  });
});
```

Run: `task test -- --project unit` → FAIL (`./health` missing).

- [ ] **Step 2: Implement**

`src/server/health.ts`:

```ts
import "server-only";
import { sql } from "drizzle-orm";
import { z } from "zod";
import { getDb } from "@/server/db/client";

export const healthBodySchema = z.discriminatedUnion("status", [
  z.object({ status: z.literal("ok"), db: z.literal("ok") }),
  z.object({ status: z.literal("error"), db: z.literal("unavailable") }),
]);

export type HealthBody = z.infer<typeof healthBodySchema>;

async function pingDatabase(): Promise<unknown> {
  return getDb().execute(sql`select 1`);
}

/** Checks the database as the runtime role. Error details are logged, never returned. */
export async function checkHealth(
  ping: () => Promise<unknown> = pingDatabase,
): Promise<{ httpStatus: 200 | 503; body: HealthBody }> {
  try {
    await ping();
    return { httpStatus: 200, body: healthBodySchema.parse({ status: "ok", db: "ok" }) };
  } catch (error) {
    console.error("Health check: database unavailable", error);
    return {
      httpStatus: 503,
      body: healthBodySchema.parse({ status: "error", db: "unavailable" }),
    };
  }
}
```

`src/app/api/health/route.ts`:

```ts
import { connection, NextResponse } from "next/server";
import { checkHealth } from "@/server/health";

export async function GET() {
  // Always run at request time; never prerender at build (no database there).
  await connection();
  const { httpStatus, body } = await checkHealth();
  return NextResponse.json(body, { status: httpStatus, headers: { "cache-control": "no-store" } });
}
```

Run: `task test -- --project unit` → PASS.

- [ ] **Step 3: Integration test against the real database**

`src/server/health.int.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { checkHealth } from "./health";

describe("checkHealth against Postgres", () => {
  it("reaches the database as the runtime role", async () => {
    await expect(checkHealth()).resolves.toEqual({
      httpStatus: 200,
      body: { status: "ok", db: "ok" },
    });
  });
});
```

`getEnv()` reads `DATABASE_URL` from the process env, which `task test` provides via `.env`.

Run: `task test` → PASS (all unit and integration tests).

- [ ] **Step 4: Verify build does not touch the database**

```bash
task db:down
task build
task db:up
```

Expected: build succeeds with the database stopped.

- [ ] **Step 5: Lint, typecheck, commit**

```bash
task lint && task typecheck
git add src/server/health.ts src/server/health.test.ts src/server/health.int.test.ts src/app/api/health
git commit -m "feat: add database health check route"
```

---

### Task 6: OPA policy toolchain

**Files:**
- Create: `policies/agenty/main.rego`, `policies/agenty/main_test.rego`, `scripts/build-policy.sh`, `scripts/install-opa.sh`, `src/server/policy/evaluate.ts`, `src/server/policy/evaluate.test.ts`
- Modify: `Taskfile.yml`, `next.config.ts`, `package.json`, `pnpm-lock.yaml`

**Interfaces:**
- Produces: `build/policy/policy.wasm` (entrypoint `agenty/authz/decision`); `evaluatePolicy(input: unknown): Promise<PolicyDecision>`; `parseResultSet(raw: unknown): PolicyDecision`; `type PolicyDecision = { allow: boolean; reason: string; require_approval: boolean }`; tasks `opa:install`, `policy:build`, `policy:test`.

- [ ] **Step 1: opa installer**

Verify asset names and checksum files on https://github.com/open-policy-agent/opa/releases/tag/v1.21.1 (expected: `opa_linux_amd64_static`, `opa_linux_arm64_static`, `opa_darwin_arm64_static`, `opa_darwin_amd64`, each with `.sha256`).

`scripts/install-opa.sh`:

```sh
#!/bin/sh
# Downloads a pinned opa binary into .tools/bin and verifies its checksum.
# Usage: scripts/install-opa.sh <version> <dest-dir>
set -eu

version="$1"
dest="$2"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

asset="opa_${os}_${arch}_static"
if [ "$os" = darwin ] && [ "$arch" = amd64 ]; then asset="opa_darwin_amd64"; fi

base="https://github.com/open-policy-agent/opa/releases/download/${version}"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

curl -fsSL -o "$tmp/$asset" "$base/$asset"
curl -fsSL -o "$tmp/$asset.sha256" "$base/$asset.sha256"
(cd "$tmp" && if command -v sha256sum >/dev/null; then sha256sum -c "$asset.sha256"; else shasum -a 256 -c "$asset.sha256"; fi)

mkdir -p "$dest"
install -m 0755 "$tmp/$asset" "$dest/opa"
"$dest/opa" version | head -n 1
```

If the `.sha256` file format is not `<hash>  <filename>`, adapt the check (compare the hash string directly).

- [ ] **Step 2: Rego policy and tests**

`policies/agenty/main.rego`:

```rego
# Agenty's base authorization policy. M0 denies everything; M4 adds the real rules.
package agenty.authz

default decision := {
	"allow": false,
	"reason": "no policy matches",
	"require_approval": false,
}
```

`policies/agenty/main_test.rego`:

```rego
package agenty.authz_test

import data.agenty.authz

deny := {"allow": false, "reason": "no policy matches", "require_approval": false}

test_denies_without_input if {
	authz.decision == deny
}

test_denies_any_tool_call if {
	authz.decision == deny with input as {"tool": {"name": "http.get"}, "args": {}}
}
```

- [ ] **Step 3: Build script and tasks**

`scripts/build-policy.sh`:

```sh
#!/bin/sh
# Compiles policies/ to WebAssembly: build/policy/policy.wasm (entrypoint agenty/authz/decision).
# The output lies outside policies/, so later opa runs never load the bundle's data.json.
set -eu

out=build/policy
rm -rf "$out"
mkdir -p "$out"
opa build -t wasm -e agenty/authz/decision --ignore '*_test.rego' -o "$out/bundle.tar.gz" policies
tar -xzf "$out/bundle.tar.gz" -C "$out" /policy.wasm
test -s "$out/policy.wasm"
```

If `tar` rejects the leading slash (BSD tar), extract with `tar -xzf ... -C "$out" policy.wasm` or list the member name with `tar -tzf` first and use it.

Add to `Taskfile.yml`:

```yaml
  opa:install:
    desc: Download the pinned opa CLI into .tools/bin
    status:
      - test "$(.tools/bin/opa version 2>/dev/null | head -n 1)" = "Version: {{trimPrefix "v" .OPA_VERSION}}"
    cmds:
      - sh scripts/install-opa.sh {{.OPA_VERSION}} .tools/bin

  policy:test:
    desc: Run Rego tests
    deps: [opa:install]
    cmds:
      - opa test policies -v

  policy:build:
    desc: Compile Rego policies to build/policy/policy.wasm
    run: once
    deps: [opa:install]
    cmds:
      - sh scripts/build-policy.sh
```

Add `- task: opa:install` to `setup` after `pnpm install`, and make `test` depend on the wasm: `deps: [policy:build]`. Make `dev` run `- task: policy:build` before `next dev`.

Run: `task policy:test` → PASS (2 tests); `task policy:build` → `build/policy/policy.wasm` exists.

- [ ] **Step 4: Failing unit tests for evaluation**

```bash
pnpm add @open-policy-agent/opa-wasm
```

`src/server/policy/evaluate.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { evaluatePolicy, parseResultSet } from "./evaluate";

describe("evaluatePolicy (built policy.wasm)", () => {
  it("denies by default", async () => {
    await expect(evaluatePolicy({})).resolves.toEqual({
      allow: false,
      reason: "no policy matches",
      require_approval: false,
    });
  });

  it("denies a tool call", async () => {
    const decision = await evaluatePolicy({ tool: { name: "http.get" }, args: { url: "x" } });
    expect(decision.allow).toBe(false);
  });
});

describe("parseResultSet (fail closed)", () => {
  it("rejects an empty result set (decision undefined)", () => {
    expect(() => parseResultSet([])).toThrow();
  });

  it("rejects a result with wrong types", () => {
    expect(() =>
      parseResultSet([{ result: { allow: "yes", reason: "x", require_approval: false } }]),
    ).toThrow();
  });

  it("rejects more than one result", () => {
    const result = { allow: true, reason: "ok", require_approval: false };
    expect(() => parseResultSet([{ result }, { result }])).toThrow();
  });

  it("accepts exactly one well-formed decision", () => {
    const result = { allow: true, reason: "ok", require_approval: true };
    expect(parseResultSet([{ result }])).toEqual(result);
  });
});
```

Run: `task test -- --project unit` → FAIL (`./evaluate` missing).

- [ ] **Step 5: Implement `src/server/policy/evaluate.ts`**

```ts
import "server-only";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { loadPolicy } from "@open-policy-agent/opa-wasm";
import { z } from "zod";

export const policyDecisionSchema = z.object({
  allow: z.boolean(),
  reason: z.string(),
  require_approval: z.boolean(),
});

export type PolicyDecision = z.infer<typeof policyDecisionSchema>;

const resultSetSchema = z.tuple([z.object({ result: policyDecisionSchema })]);

/** Parses OPA's result set. Anything but exactly one well-formed decision throws (fail closed). */
export function parseResultSet(raw: unknown): PolicyDecision {
  const [entry] = resultSetSchema.parse(raw);
  return entry.result;
}

type LoadedPolicy = Awaited<ReturnType<typeof loadPolicy>>;

let policy: Promise<LoadedPolicy> | undefined;

/** The standalone server chdirs to its own directory, where the build traces this file to. */
function policyWasmPath(): string {
  return path.join(process.cwd(), "build/policy/policy.wasm");
}

function getPolicy(): Promise<LoadedPolicy> {
  policy ??= readFile(policyWasmPath())
    .then((wasm) => loadPolicy(wasm))
    .catch((error: unknown) => {
      policy = undefined; // retry on the next call instead of caching the failure
      throw error;
    });
  return policy;
}

export async function evaluatePolicy(input: unknown): Promise<PolicyDecision> {
  const loaded = await getPolicy();
  return parseResultSet(loaded.evaluate(input));
}
```

Note: the wasm instance is not re-entrant across `await`s, but `evaluate` is synchronous, so concurrent requests are safe.

Run: `task test -- --project unit` → PASS.

- [ ] **Step 6: Trace the wasm into the standalone output**

In `next.config.ts` add to `nextConfig`:

```ts
  // Read at runtime by src/server/policy/evaluate.ts; file tracing cannot see the path.
  outputFileTracingIncludes: {
    "/**": ["./build/policy/policy.wasm"],
  },
```

Check https://nextjs.org/docs/app/api-reference/config/next-config-js/output for the key syntax; the presence check in Task 7 proves it works.

- [ ] **Step 7: Lint, typecheck, commit**

```bash
task lint && task typecheck && task test
git add policies scripts/build-policy.sh scripts/install-opa.sh src/server/policy Taskfile.yml next.config.ts package.json pnpm-lock.yaml
git commit -m "feat: add OPA policy toolchain with WASM evaluation"
```

---

### Task 7: Production build script, Playwright E2E, `task ci`

**Files:**
- Create: `scripts/build.sh`, `playwright.config.ts`, `tests/e2e/home.spec.ts`, `tests/e2e/health.spec.ts`
- Modify: `Taskfile.yml`, `pnpm-workspace.yaml`, `package.json`, `pnpm-lock.yaml`

**Interfaces:**
- Consumes: `scripts/build-policy.sh` (Task 6), `scripts/migrate.mjs` (Task 4), home page `<h1>Agenty</h1>` (Task 2), `/api/health` (Task 5).
- Produces: a self-contained `.next/standalone/` with `server.js`, `.next/static`, `public/` (if any), `build/policy/policy.wasm`, `scripts/migrate.mjs` (bundled) and `src/server/db/migrations/`. The Dockerfile (Task 8) copies exactly this directory.

- [ ] **Step 1: Build script**

```bash
pnpm add -D esbuild
```

Add `esbuild: true` under `allowBuilds` in `pnpm-workspace.yaml`.

`scripts/build.sh`:

```sh
#!/bin/sh
# Production build shared by `task build` and the Dockerfile. Produces a self-contained
# .next/standalone directory: server, static assets, policy wasm and the migration runner.
set -eu

sh scripts/build-policy.sh
pnpm exec next build

out=.next/standalone
mkdir -p "$out/.next"
rm -rf "$out/.next/static" && cp -R .next/static "$out/.next/static"
if [ -d public ]; then rm -rf "$out/public" && cp -R public "$out/public"; fi

# Migration runner: bundled, because file tracing does not include drizzle's migrator.
pnpm exec esbuild scripts/migrate.mjs --bundle --platform=node --format=esm --target=node24 \
  --outfile="$out/scripts/migrate.mjs" --log-level=warning
rm -rf "$out/src/server/db/migrations" && mkdir -p "$out/src/server/db"
cp -R src/server/db/migrations "$out/src/server/db/migrations"

if [ ! -s "$out/build/policy/policy.wasm" ]; then
  echo "build: build/policy/policy.wasm is missing from $out (check outputFileTracingIncludes)" >&2
  exit 1
fi
```

If the bundled `migrate.mjs` fails at runtime with `Dynamic require of "..." is not supported`, add `--banner:js="import { createRequire } from 'node:module'; const require = createRequire(import.meta.url);"`.

Replace the `build` task:

```yaml
  build:
    desc: Production build (.next/standalone, self-contained)
    run: once
    deps: [opa:install]
    cmds:
      - sh scripts/build.sh
```

- [ ] **Step 2: Verify the standalone output**

```bash
task build
ls .next/standalone/build/policy/policy.wasm .next/standalone/scripts/migrate.mjs .next/standalone/src/server/db/migrations
DATABASE_MIGRATION_URL=postgres://agenty_owner:agenty_owner@localhost:5432/agenty node .next/standalone/scripts/migrate.mjs
```

Expected: files exist; migrate prints `Migrations applied.` (no-op on a migrated DB).

Mutation check: temporarily remove the `outputFileTracingIncludes` entry, run `task build` → fails with the "missing" message; restore.

- [ ] **Step 3: Playwright**

```bash
pnpm add -D @playwright/test
```

`playwright.config.ts`:

```ts
import { defineConfig, devices } from "@playwright/test";

// A dedicated port and no server reuse: E2E always tests the standalone production build,
// never a dev server that happens to be running.
const port = 3100;

export default defineConfig({
  testDir: "tests/e2e",
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : "list",
  use: { baseURL: `http://127.0.0.1:${port}`, trace: "retain-on-failure" },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    command: "node .next/standalone/server.js",
    url: `http://127.0.0.1:${port}/api/health`,
    reuseExistingServer: false,
    timeout: 60_000,
    env: {
      PORT: String(port),
      HOSTNAME: "127.0.0.1",
      NODE_ENV: "production",
      DATABASE_URL: process.env.DATABASE_URL ?? "",
    },
  },
});
```

`tests/e2e/home.spec.ts`:

```ts
import { expect, test } from "@playwright/test";

test("home page renders", async ({ page }) => {
  await page.goto("/");
  await expect(page).toHaveTitle("Agenty");
  await expect(page.getByRole("heading", { level: 1, name: "Agenty" })).toBeVisible();
});
```

`tests/e2e/health.spec.ts`:

```ts
import { expect, test } from "@playwright/test";

test("health endpoint reports ok and is not cached", async ({ request }) => {
  const response = await request.get("/api/health");
  expect(response.status()).toBe(200);
  expect(await response.json()).toEqual({ status: "ok", db: "ok" });
  expect(response.headers()["cache-control"]).toContain("no-store");
});
```

Add to `Taskfile.yml`, and extend `setup` with the browser install:

```yaml
  test:e2e:
    desc: Run Playwright E2E tests against the standalone production build
    deps: [build]
    cmds:
      - pnpm exec playwright test {{.CLI_ARGS}}

  playwright:install:
    desc: Install Playwright's Chromium (with OS dependencies in CI)
    cmds:
      - pnpm exec playwright install {{if eq (env "CI") "true"}}--with-deps {{end}}chromium
```

`setup` becomes:

```yaml
  setup:
    desc: Install dependencies and tools (opa, Playwright Chromium); create .env
    cmds:
      - pnpm install --frozen-lockfile
      - task: opa:install
      - task: playwright:install
      - cmd: test -f .env || cp .env.example .env
```

- [ ] **Step 4: Run E2E**

```bash
task setup && task test:e2e
```

Expected: 2 tests PASS.

Mutation check: change the `<h1>` text to `Agenty!`, run `task test:e2e` → `home page renders` FAILS; restore.

- [ ] **Step 5: `ci` task**

```yaml
  ci:
    desc: Everything CI checks (needs a reachable database; locally run `task db:up` first)
    cmds:
      - task: policy:test
      - task: lint
      - task: typecheck
      - task: db:check
      - task: db:migrate
      - task: test
      - task: build
      - task: test:e2e
```

Run: `task ci`. Expected: all steps pass; `next build` runs once (check the output).

Then `task db:down && task ci; echo "exit=$?"` → fails at `db:check` with the `task db:up` hint; `task db:up`.

- [ ] **Step 6: Commit**

```bash
git add scripts/build.sh playwright.config.ts tests/e2e Taskfile.yml pnpm-workspace.yaml package.json pnpm-lock.yaml
git commit -m "feat: add standalone build, Playwright E2E and task ci"
```

---

### Task 8: Dockerfile

**Files:**
- Create: `Dockerfile`, `.dockerignore`
- Modify: `Taskfile.yml`

**Interfaces:**
- Consumes: `scripts/build.sh`, `scripts/install-opa.sh`, `.next/standalone` layout (Task 7).
- Produces: image running `node server.js` on port 3000 as user `node`; `node scripts/migrate.mjs` inside the image applies migrations (`DATABASE_MIGRATION_URL`); task `docker:build` tags `agenty:local`.

- [ ] **Step 1: `.dockerignore`**

```
.git
.next
.tools
build
node_modules
test-results
playwright-report
.env
.env.*
!.env.example
.claude
docs
```

- [ ] **Step 2: `Dockerfile`**

```dockerfile
# syntax=docker/dockerfile:1

ARG NODE_VERSION=24

FROM node:${NODE_VERSION}-bookworm-slim AS base
ENV PNPM_HOME=/pnpm PATH=/pnpm:$PATH NEXT_TELEMETRY_DISABLED=1
RUN corepack enable
WORKDIR /app

FROM base AS deps
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN --mount=type=cache,id=pnpm,target=/pnpm/store pnpm install --frozen-lockfile

FROM base AS build
ARG OPA_VERSION=v1.21.1
RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates curl \
  && rm -rf /var/lib/apt/lists/*
COPY scripts/install-opa.sh scripts/install-opa.sh
RUN sh scripts/install-opa.sh "$OPA_VERSION" /usr/local/bin
COPY --from=deps /app/node_modules ./node_modules
COPY . .
RUN sh scripts/build.sh

FROM node:${NODE_VERSION}-bookworm-slim AS runtime
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 HOSTNAME=0.0.0.0 PORT=3000
WORKDIR /app
COPY --from=build --chown=node:node /app/.next/standalone ./
USER node
EXPOSE 3000
CMD ["node", "server.js"]
```

Keep `ARG OPA_VERSION` in sync with `OPA_VERSION` in `Taskfile.yml`; `docker:build` passes it explicitly so the Taskfile is the source of truth.

- [ ] **Step 3: Task**

```yaml
  docker:build:
    desc: Build the production image (tag agenty:local)
    cmds:
      - docker build --build-arg OPA_VERSION={{.OPA_VERSION}} -t agenty:local .
```

- [ ] **Step 4: Verify the image**

```bash
task docker:build
docker run --rm --network host -e DATABASE_MIGRATION_URL=postgres://agenty_owner:agenty_owner@localhost:5432/agenty agenty:local node scripts/migrate.mjs
docker run -d --name agenty-smoke --network host -e PORT=3200 -e DATABASE_URL=postgres://agenty_app:agenty_app@localhost:5432/agenty agenty:local
sleep 3; curl -fsS http://127.0.0.1:3200/api/health; echo; docker rm -f agenty-smoke
docker run --rm agenty:local id -un
```

Expected: `Migrations applied.`; `{"status":"ok","db":"ok"}`; `node`.

- [ ] **Step 5: Commit**

```bash
git add Dockerfile .dockerignore Taskfile.yml
git commit -m "feat: add multi-stage Dockerfile with bundled migration runner"
```

---

### Task 9: GitHub Actions

**Files:**
- Create: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: `task setup`, `task ci`, `task docker:build`, `docker/postgres/init.sql`, `.env.example` defaults.

- [ ] **Step 1: Workflow**

Check the current major versions of the actions (2026-10-09: checkout v7, setup-node v7, pnpm/action-setup v6, arduino/setup-task v3, cache v6, upload-artifact v7) and their inputs.

`.github/workflows/ci.yml`:

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:

permissions:
  contents: read

concurrency:
  group: ci-${{ github.ref }}
  cancel-in-progress: true

env:
  POSTGRES_URL_SUPERUSER: postgres://postgres:postgres@localhost:5432/agenty

jobs:
  ci:
    runs-on: ubuntu-latest
    timeout-minutes: 30
    services:
      postgres:
        image: postgres:18
        env:
          POSTGRES_USER: postgres
          POSTGRES_PASSWORD: postgres
          POSTGRES_DB: agenty
        ports: ["5432:5432"]
        options: >-
          --health-cmd "pg_isready -U postgres -d agenty"
          --health-interval 2s --health-timeout 5s --health-retries 30
    steps:
      - uses: actions/checkout@v7
      - uses: pnpm/action-setup@v6
      - uses: actions/setup-node@v7
        with:
          node-version-file: .nvmrc
          cache: pnpm
      - uses: arduino/setup-task@v3
        with:
          repo-token: ${{ secrets.GITHUB_TOKEN }}
      - uses: actions/cache@v6
        with:
          path: .tools
          key: tools-${{ runner.os }}-${{ runner.arch }}-${{ hashFiles('Taskfile.yml', 'scripts/install-opa.sh') }}
      - name: Create database roles
        run: psql "$POSTGRES_URL_SUPERUSER" -v ON_ERROR_STOP=1 -f docker/postgres/init.sql
      - run: task setup
      - run: task ci
      - if: failure()
        uses: actions/upload-artifact@v7
        with:
          name: playwright-report
          path: playwright-report
          if-no-files-found: ignore

  docker:
    runs-on: ubuntu-latest
    timeout-minutes: 30
    services:
      postgres:
        image: postgres:18
        env:
          POSTGRES_USER: postgres
          POSTGRES_PASSWORD: postgres
          POSTGRES_DB: agenty
        ports: ["5432:5432"]
        options: >-
          --health-cmd "pg_isready -U postgres -d agenty"
          --health-interval 2s --health-timeout 5s --health-retries 30
    steps:
      - uses: actions/checkout@v7
      - uses: arduino/setup-task@v3
        with:
          repo-token: ${{ secrets.GITHUB_TOKEN }}
      - name: Create database roles
        run: psql "$POSTGRES_URL_SUPERUSER" -v ON_ERROR_STOP=1 -f docker/postgres/init.sql
      - run: task docker:build
      - name: Apply migrations from the image
        run: >-
          docker run --rm --network host
          -e DATABASE_MIGRATION_URL=postgres://agenty_owner:agenty_owner@localhost:5432/agenty
          agenty:local node scripts/migrate.mjs
      - name: Smoke-test the image
        run: |
          docker run -d --name agenty --network host \
            -e DATABASE_URL=postgres://agenty_app:agenty_app@localhost:5432/agenty agenty:local
          for i in $(seq 1 30); do
            if curl -fsS http://127.0.0.1:3000/api/health; then exit 0; fi
            sleep 1
          done
          docker logs agenty
          exit 1
```

`pnpm/action-setup` reads the version from `packageManager`. `task setup` copies `.env.example` to `.env`, whose defaults match the service container.

- [ ] **Step 2: Lint the workflow locally**

If `actionlint` is available (`pnpm dlx` is not an option; use `docker run --rm -v "$PWD:/repo" -w /repo rhysd/actionlint:latest`), run it. Expected: no findings.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: run task ci and the Docker image smoke test in GitHub Actions"
```

---

### Task 10: CLAUDE.md, README, final verification, PR

**Files:**
- Create: `CLAUDE.md`, `README.md`

- [ ] **Step 1: `CLAUDE.md`**

````markdown
# Agenty

Self-hosted, multi-tenant AI agent builder with chat. Tenants (Better Auth organizations)
configure agents (model, system prompt, tools) and chat with them. Every tool call is checked by
an OPA policy; agent runs are durable (Workflow SDK, Postgres world).

Runtime components: the Next.js app (standalone Node server) and Postgres. Nothing else.

## Commands

All commands go through `Taskfile.yml` (go-task), identically locally and in CI.

| Command | Does |
|---|---|
| `task setup` | pnpm install, pinned `opa` into `.tools/bin`, Playwright Chromium, `.env` from `.env.example` |
| `task dev` | Postgres up, migrations, policy build, `next dev` |
| `task db:up` / `db:down` / `db:reset` | Local Postgres (compose); `db:reset` deletes data (needed after editing `docker/postgres/init.sql`) |
| `task db:generate` | New migration from `src/server/db/schema.ts`; `-- --custom --name x` for hand-written SQL |
| `task db:migrate` | Apply migrations as `agenty_owner` (`scripts/migrate.mjs`) |
| `task db:studio` | Drizzle Studio |
| `task policy:test` / `policy:build` | `opa test`; compile `policies/` → `build/policy/policy.wasm` |
| `task lint` / `task format` | Biome check / apply fixes incl. Tailwind class sorting |
| `task typecheck` | `next typegen` + `tsc --noEmit` |
| `task test` | Vitest: `unit` (`*.test.ts`) and `integration` (`*.int.test.ts`, needs Postgres) |
| `task test:e2e` | Playwright against the standalone build on port 3100 |
| `task build` | `scripts/build.sh`: self-contained `.next/standalone` |
| `task docker:build` | Production image `agenty:local` |
| `task ci` | Everything CI checks; locally run `task db:up` first |

Run tests through `task` (it loads `.env`); plain `pnpm exec vitest` lacks `DATABASE_URL`.

## Architecture

```
src/app/            routes, pages, route handlers (thin: parse, call src/server, respond)
src/components/ui/  shadcn/ui components
src/lib/            shared client/server utilities
src/server/         server-only code (every module imports "server-only")
  env.ts            lazily Zod-parsed configuration (getEnv)
  db/               Drizzle schema, client (getDb), migrations
  health.ts         health check
  policy/           OPA WASM loading and evaluation
policies/           Rego sources and tests
scripts/            build, migration, opa install, DB check (shared by Taskfile and Dockerfile)
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
evaluated in-process by `evaluatePolicy()`. Any result other than exactly one well-formed decision
throws (fail closed).

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
  `src/instrumentation.ts` validates it at server start.
- Route handlers that touch the database call `await connection()` first (Cache Components is on;
  without it Next may try to prerender them at build time).
- Error bodies never contain internal details; log them server-side.
- Tests are named after the guarantee they protect. Unit: `*.test.ts`; integration: `*.int.test.ts`.
- English only, no i18n.
- One branch and PR per milestone; `task ci` must pass locally before committing.
- Before using a library API, check its current docs (context7 / official site).

## Version notes

- Node 24 LTS (`.nvmrc`); OPA version pinned in `Taskfile.yml` (`OPA_VERSION`) and passed to
  the Docker build.
````

Fill "Version notes" with any deviation found during Tasks 1–9 (e.g. a TypeScript major pin).

- [ ] **Step 2: `README.md`**

```markdown
# Agenty

Self-hosted, multi-tenant AI agent builder with chat.

## Quick start

Requirements: Node 24, pnpm, [go-task](https://taskfile.dev), Docker.

    task setup     # dependencies, opa, Playwright browser, .env
    task dev       # Postgres + migrations + dev server on http://localhost:3000

`task --list` shows all commands; `CLAUDE.md` describes the architecture and conventions.

## Production image

    task docker:build
    docker run --rm -e DATABASE_MIGRATION_URL=... agenty:local node scripts/migrate.mjs
    docker run -p 3000:3000 -e DATABASE_URL=... agenty:local

The database needs the roles from `docker/postgres/init.sql`.
```

- [ ] **Step 3: Full verification from a clean state**

```bash
git status --short            # only CLAUDE.md, README.md untracked
rm -rf node_modules .next build .tools
task db:reset && task db:up
task setup && task ci
task docker:build
```

Expected: everything passes. Fix anything that fails before continuing.

- [ ] **Step 4: Commit, push, open PR**

```bash
git add CLAUDE.md README.md
git commit -m "docs: add CLAUDE.md and README"
git log --oneline origin/main..HEAD   # only M0 commits
git push -u origin chore/m0-scaffolding
```

Open a PR "M0: scaffolding" against `main` with a summary of what was built, what remains open, and the test plan. Then: reviewer subagent, fix findings, green CI, and wait for the maintainer's OK before merging.
