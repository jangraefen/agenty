# M2 – Workspaces Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Users collaborate in workspaces: a personal workspace per user, shared workspaces with admin/member roles, invitations for existing users, and an access check later milestones reuse.

**Architecture:** Own Drizzle tables in schema `app` (no Better Auth organization plugin, no RLS). Server functions in `src/server/workspaces/` take the acting user's id explicitly and enforce rules R1–R9 inside transactions that lock the workspace row. Pages are server components; mutations are server actions behind plain `<form>`s that redirect, with fixed error messages via `?error=`. The personal workspace is ensured in Better Auth's `databaseHooks.session.create.before`.

**Tech Stack:** Next.js 16.4 (App Router, Cache Components, Partial Prefetching), React 19.3, Better Auth 1.7.7, Drizzle ORM 0.45 + postgres-js, Zod 4, Vitest, Playwright, Biome, shadcn/ui (Base UI preset `base-nova`).

**Spec:** `docs/specs/2026-10-10-m2-workspaces-design.md` (rules R1–R9 are numbered there).

## Global Constraints

- Every `src/server/**` module starts with `import "server-only";` (except files used by plain Node scripts; none here).
- Configuration only via `getEnv()`; database only via `getDb()`.
- Role values exactly `"admin"` and `"member"`. Personal workspace default name exactly `"Personal"`.
- Workspace names: trimmed, 1–80 characters (Zod `.trim().min(1).max(80)`); DB check `char_length("name") between 1 and 80`.
- User search query: trimmed, 2–100 characters; at most 10 results, ordered by name then email.
- Ids from requests are parsed with `z.uuid()` before any query; a malformed id is `not_found`.
- Non-members get `not_found` (pages: `notFound()`), never a "forbidden" page.
- Every mutating service function runs in one transaction: lock the workspace row (`for update`), then read the actor's membership, then check rules.
- Error codes (exact): `not_found`, `forbidden`, `personal_workspace`, `last_admin`, `already_member`, `already_invited`, `user_not_found`, `invalid_name`, `invalid_query`, `invalid_role`, `confirmation_mismatch`; UI fallback code `unexpected`.
- Errors never carry internal details; unexpected errors are logged by error class only (`error.name`).
- Biome for lint/format (`task format`), TypeScript strict + `noUncheckedIndexedAccess`.
- Tests named after the guarantee they protect; unit `*.test.ts`, integration `*.int.test.ts`; Playwright queries use `exact: true` where a name could match twice.
- Run tests through `task` (it loads `.env`); `task db:up` must be running for integration and E2E tests.
- `task ci` passes before every commit. Commit trailer exactly: `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Never edit `src/server/db/auth-schema.ts` by hand; never look at git branch `legacy`.
- Don't stage with `git add -A` / `git add .` (agent worktrees live under `.claude/worktrees`); add paths explicitly.

## Review Focus

1. **Stale role after demotion:** an admin demoted (or removed) by another admin submits an old settings form → must get `forbidden`/`not_found`, never act. Test in Task 2 (`changeRole` then `renameWorkspace` by the demoted user).
2. **Accepting an invitation that was cancelled meanwhile** → `not_found`, no membership created. Test in Task 3.
3. **Search input with SQL wildcards** (`%`, `_`, `\`) → matched literally, not as patterns. Unit test in Task 1, integration test in Task 3.
4. **Tampered bound action arguments** (a workspace id the user is not a member of, or not a uuid) → not-found page, no change. Covered by the service checks (Tasks 2–3) and the E2E non-member test (Task 5).
5. **Sign-in when workspace setup fails** → redirect to `/sign-in?error=sign_in_failed`, no session row. Test in Task 4.

---

### Task 1: Tables, migration and input validation

**Files:**
- Modify: `src/server/db/schema.ts`
- Create: `src/server/db/migrations/0003_*.sql` (generated) and its `meta/` snapshot (generated)
- Create: `src/server/workspaces/validation.ts`
- Test: `src/server/workspaces/validation.test.ts`

**Interfaces:**
- Produces: tables `workspace`, `workspaceMember`, `workspaceInvitation`; `workspaceRoles`, `type WorkspaceRole`; `workspaceNameSchema`, `workspaceIdSchema`, `userSearchQuerySchema`, `workspaceRoleSchema`, `likePattern(query: string): string`, `PERSONAL_WORKSPACE_NAME`.

- [ ] **Step 1: Write the failing unit test** — `src/server/workspaces/validation.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import {
  likePattern,
  userSearchQuerySchema,
  workspaceIdSchema,
  workspaceNameSchema,
  workspaceRoleSchema,
} from "./validation";

describe("workspace names", () => {
  it("are trimmed", () => {
    expect(workspaceNameSchema.parse("  Team  ")).toBe("Team");
  });

  it("must not be empty after trimming", () => {
    expect(workspaceNameSchema.safeParse("   ").success).toBe(false);
  });

  it("allow at most 80 characters", () => {
    expect(workspaceNameSchema.safeParse("a".repeat(80)).success).toBe(true);
    expect(workspaceNameSchema.safeParse("a".repeat(81)).success).toBe(false);
  });
});

describe("user search queries", () => {
  it("need 2 to 100 characters after trimming", () => {
    expect(userSearchQuerySchema.safeParse(" a ").success).toBe(false);
    expect(userSearchQuerySchema.parse(" ab ")).toBe("ab");
    expect(userSearchQuerySchema.safeParse("a".repeat(101)).success).toBe(false);
  });

  it("match wildcards literally", () => {
    expect(likePattern("50%_off\\")).toBe("%50\\%\\_off\\\\%");
  });
});

describe("ids and roles", () => {
  it("reject malformed workspace ids before any query", () => {
    expect(workspaceIdSchema.safeParse("not-a-uuid").success).toBe(false);
    expect(workspaceIdSchema.safeParse("6f1c1f7e-3d4b-4c55-9a43-1b2a5c6d7e8f").success).toBe(true);
  });

  it("know only admin and member", () => {
    expect(workspaceRoleSchema.safeParse("admin").success).toBe(true);
    expect(workspaceRoleSchema.safeParse("owner").success).toBe(false);
  });
});
```

- [ ] **Step 2: Run it to see it fail**

Run: `task test -- --project unit src/server/workspaces/validation.test.ts`
Expected: FAIL (cannot resolve `./validation`).

- [ ] **Step 3: Add the tables** — replace `src/server/db/schema.ts` with:

```ts
import { sql } from "drizzle-orm";
import {
  check,
  index,
  pgSchema,
  primaryKey,
  text,
  timestamp,
  unique,
  uuid,
} from "drizzle-orm/pg-core";
import { user } from "./auth-schema";

/** All Agenty tables live in schema `app`, owned by role agenty_owner. */
export const app = pgSchema("app");

export const workspaceRoles = ["admin", "member"] as const;
export type WorkspaceRole = (typeof workspaceRoles)[number];

/** A workspace; `personalUserId` is set only for the personal workspace of that user (R1). */
export const workspace = app.table(
  "workspace",
  {
    id: uuid("id").defaultRandom().primaryKey(),
    name: text("name").notNull(),
    personalUserId: uuid("personal_user_id")
      .unique()
      .references(() => user.id, { onDelete: "cascade" }),
    createdAt: timestamp("created_at", { withTimezone: true }).defaultNow().notNull(),
  },
  // Backstop for the Zod check (which counts UTF-16 code units and is the stricter one).
  () => [check("workspace_name_length", sql`char_length("name") between 1 and 80`)],
);

export const workspaceMember = app.table(
  "workspace_member",
  {
    workspaceId: uuid("workspace_id")
      .notNull()
      .references(() => workspace.id, { onDelete: "cascade" }),
    userId: uuid("user_id")
      .notNull()
      .references(() => user.id, { onDelete: "cascade" }),
    role: text("role", { enum: workspaceRoles }).notNull(),
    createdAt: timestamp("created_at", { withTimezone: true }).defaultNow().notNull(),
  },
  (table) => [
    primaryKey({ columns: [table.workspaceId, table.userId] }),
    index("workspace_member_user_id_idx").on(table.userId),
    check("workspace_member_role", sql`"role" in ('admin', 'member')`),
  ],
);

export const workspaceInvitation = app.table(
  "workspace_invitation",
  {
    id: uuid("id").defaultRandom().primaryKey(),
    workspaceId: uuid("workspace_id")
      .notNull()
      .references(() => workspace.id, { onDelete: "cascade" }),
    userId: uuid("user_id")
      .notNull()
      .references(() => user.id, { onDelete: "cascade" }),
    invitedByUserId: uuid("invited_by_user_id").references(() => user.id, {
      onDelete: "set null",
    }),
    createdAt: timestamp("created_at", { withTimezone: true }).defaultNow().notNull(),
  },
  (table) => [
    unique("workspace_invitation_workspace_user").on(table.workspaceId, table.userId),
    index("workspace_invitation_user_id_idx").on(table.userId),
  ],
);
```

- [ ] **Step 4: Generate the migration**

Run: `task db:generate -- --name workspaces`
Expected: a new `src/server/db/migrations/0003_workspaces.sql` with three `CREATE TABLE "app".…` statements, both check constraints, the unique constraints, indexes and foreign keys (`ON DELETE cascade` / `set null`). Open it and confirm the check constraints read `char_length("name") between 1 and 80` and `"role" in ('admin', 'member')`. It must not touch the auth tables or create schema `app` again. Then apply it: `task db:migrate`.

- [ ] **Step 5: Write the validation module** — `src/server/workspaces/validation.ts`:

```ts
import "server-only";
import { z } from "zod";
import { workspaceRoles } from "@/server/db/schema";

export const PERSONAL_WORKSPACE_NAME = "Personal";

export const workspaceNameSchema = z.string().trim().min(1).max(80);
export const workspaceIdSchema = z.uuid();
export const workspaceRoleSchema = z.enum(workspaceRoles);
export const userSearchQuerySchema = z.string().trim().min(2).max(100);

/** An ILIKE pattern matching `query` anywhere, with `\`, `%` and `_` taken literally. */
export function likePattern(query: string): string {
  return `%${query.replace(/[\\%_]/g, (char) => `\\${char}`)}%`;
}
```

- [ ] **Step 6: Run the unit test**

Run: `task test -- --project unit src/server/workspaces/validation.test.ts`
Expected: PASS (7 tests).

- [ ] **Step 7: Full check and commit**

Run: `task format`, then `task ci` → all green.

```bash
git add src/server/db/schema.ts src/server/db/migrations src/server/workspaces/validation.ts src/server/workspaces/validation.test.ts
git commit -m "feat(workspaces): add workspace tables and input validation

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Workspace and membership service

**Files:**
- Create: `src/server/workspaces/errors.ts`
- Create: `src/server/workspaces/internal.ts`
- Create: `src/server/workspaces/workspaces.ts`
- Create: `tests/support/users.ts`
- Test: `src/server/workspaces/workspaces.int.test.ts`

**Interfaces:**
- Consumes (Task 1): `workspace`, `workspaceMember`, `WorkspaceRole`, `workspaceNameSchema`, `workspaceIdSchema`, `workspaceRoleSchema`, `PERSONAL_WORKSPACE_NAME`.
- Produces:
  - `class WorkspaceError extends Error { readonly code: WorkspaceErrorCode }`, `type WorkspaceErrorCode`, `WORKSPACE_ERROR_CODES`.
  - `type WorkspaceSummary = { id: string; name: string; personal: boolean }`, `type WorkspaceAccess = { workspace: WorkspaceSummary; role: WorkspaceRole }`.
  - internal: `type Tx`, `parseId(value: unknown, code?: WorkspaceErrorCode): string`, `lockForActor(tx, workspaceId, actorId): Promise<WorkspaceAccess>`, `lockForAdmin(tx, workspaceId, actorId): Promise<WorkspaceAccess>`, `getMembership(userId, workspaceId): Promise<WorkspaceAccess | null>`, `requireMembership(userId, workspaceId): Promise<WorkspaceAccess>`.
  - `ensurePersonalWorkspace(userId: string): Promise<string>` (workspace id)
  - `createWorkspace(actorId: string, name: string): Promise<string>`
  - `renameWorkspace(actorId: string, workspaceId: string, name: string): Promise<void>`
  - `deleteWorkspace(actorId: string, workspaceId: string, confirmation: string): Promise<void>`
  - `listWorkspaces(userId: string): Promise<(WorkspaceSummary & { role: WorkspaceRole })[]>` (personal first, then by name)
  - `listMembers(actorId: string, workspaceId: string): Promise<{ userId: string; name: string; email: string; role: WorkspaceRole }[]>`
  - `changeRole(actorId: string, workspaceId: string, memberUserId: string, role: string): Promise<void>`
  - `removeMember(actorId: string, workspaceId: string, memberUserId: string): Promise<void>`
  - `leaveWorkspace(actorId: string, workspaceId: string): Promise<void>`
  - test support: `testUsers()` → `{ create(name?): Promise<{ id; name; email }>, cleanup(): Promise<void> }`.

- [ ] **Step 1: Test support for users** — `tests/support/users.ts`:

```ts
// Creates users directly in the database for workspace tests and removes them (and every
// workspace they belong to) afterwards.
import { randomUUID } from "node:crypto";
import { inArray } from "drizzle-orm";
import { user } from "@/server/db/auth-schema";
import { getDb } from "@/server/db/client";
import { workspace, workspaceMember } from "@/server/db/schema";

export function testUsers() {
  const ids: string[] = [];
  return {
    async create(name = "Test User") {
      const id = randomUUID();
      const email = `w-${id}@example.test`;
      await getDb().insert(user).values({ id, name, email });
      ids.push(id);
      return { id, name, email };
    },
    async cleanup() {
      if (ids.length === 0) return;
      const db = getDb();
      await db.delete(workspace).where(
        inArray(
          workspace.id,
          db
            .select({ id: workspaceMember.workspaceId })
            .from(workspaceMember)
            .where(inArray(workspaceMember.userId, ids)),
        ),
      );
      await db.delete(user).where(inArray(user.id, ids));
    },
  };
}
```

- [ ] **Step 2: Write the failing integration test** — `src/server/workspaces/workspaces.int.test.ts`:

```ts
import { and, eq } from "drizzle-orm";
import { afterAll, describe, expect, it } from "vitest";
import { getDb } from "@/server/db/client";
import { workspace, workspaceMember } from "@/server/db/schema";
import { testUsers } from "../../../tests/support/users";
import { WorkspaceError } from "./errors";
import { getMembership } from "./internal";
import {
  changeRole,
  createWorkspace,
  deleteWorkspace,
  ensurePersonalWorkspace,
  leaveWorkspace,
  listMembers,
  listWorkspaces,
  removeMember,
  renameWorkspace,
} from "./workspaces";

const users = testUsers();
const db = getDb();
afterAll(() => users.cleanup());

const rejectsWith = (promise: Promise<unknown>, code: string) =>
  expect(promise).rejects.toSatisfy((e) => e instanceof WorkspaceError && e.code === code);

/** A shared workspace with an admin and a member (added directly; invitations are Task 3). */
async function sharedWorkspace() {
  const admin = await users.create("Admin");
  const member = await users.create("Member");
  const id = await createWorkspace(admin.id, "Team");
  await db.insert(workspaceMember).values({ workspaceId: id, userId: member.id, role: "member" });
  return { id, admin, member };
}

const adminCount = async (id: string) =>
  db.$count(workspaceMember, and(eq(workspaceMember.workspaceId, id), eq(workspaceMember.role, "admin")));

describe("R1: personal workspace", () => {
  it("is created once with the user as admin", async () => {
    const u = await users.create();
    const first = await ensurePersonalWorkspace(u.id);
    const second = await ensurePersonalWorkspace(u.id);

    expect(second).toBe(first);
    expect(await getMembership(u.id, first)).toEqual({
      workspace: { id: first, name: "Personal", personal: true },
      role: "admin",
    });
  });

  it("is created once under concurrent calls", async () => {
    const u = await users.create();
    const ids = await Promise.all(Array.from({ length: 5 }, () => ensurePersonalWorkspace(u.id)));

    expect(new Set(ids).size).toBe(1);
    expect(await db.$count(workspace, eq(workspace.personalUserId, u.id))).toBe(1);
  });
});

describe("R2: personal workspace limits", () => {
  it("cannot be deleted or left, and its member's role cannot change", async () => {
    const u = await users.create();
    const id = await ensurePersonalWorkspace(u.id);

    await rejectsWith(deleteWorkspace(u.id, id, "Personal"), "personal_workspace");
    await rejectsWith(leaveWorkspace(u.id, id), "last_admin");
    await rejectsWith(changeRole(u.id, id, u.id, "member"), "last_admin");
    await rejectsWith(removeMember(u.id, id, u.id), "last_admin");
  });

  it("can be renamed", async () => {
    const u = await users.create();
    const id = await ensurePersonalWorkspace(u.id);
    await renameWorkspace(u.id, id, "  Mine  ");
    expect((await getMembership(u.id, id))?.workspace.name).toBe("Mine");
  });
});

describe("R3/R4: creating and managing", () => {
  it("makes the creator admin of a new, non-personal workspace", async () => {
    const u = await users.create();
    const id = await createWorkspace(u.id, " Team ");
    expect(await getMembership(u.id, id)).toEqual({
      workspace: { id, name: "Team", personal: false },
      role: "admin",
    });
  });

  it("rejects invalid names", async () => {
    const u = await users.create();
    await rejectsWith(createWorkspace(u.id, "   "), "invalid_name");
    const id = await createWorkspace(u.id, "Team");
    await rejectsWith(renameWorkspace(u.id, id, "x".repeat(81)), "invalid_name");
  });

  it("lets members do none of the admin operations", async () => {
    const { id, admin, member } = await sharedWorkspace();
    await rejectsWith(renameWorkspace(member.id, id, "Mine"), "forbidden");
    await rejectsWith(deleteWorkspace(member.id, id, "Team"), "forbidden");
    await rejectsWith(changeRole(member.id, id, admin.id, "member"), "forbidden");
    await rejectsWith(removeMember(member.id, id, admin.id), "forbidden");
  });

  it("answers not_found to non-members, for reads too", async () => {
    const { id } = await sharedWorkspace();
    const stranger = await users.create();
    await rejectsWith(listMembers(stranger.id, id), "not_found");
    await rejectsWith(renameWorkspace(stranger.id, id, "Mine"), "not_found");
    await rejectsWith(leaveWorkspace(stranger.id, id), "not_found");
    await rejectsWith(deleteWorkspace(stranger.id, id, "Team"), "not_found");
    await rejectsWith(changeRole(stranger.id, id, stranger.id, "admin"), "not_found");
    await rejectsWith(removeMember(stranger.id, id, stranger.id), "not_found");
    expect(await getMembership(stranger.id, id)).toBeNull();
  });

  it("treats malformed ids as not found without a query error", async () => {
    const u = await users.create();
    await rejectsWith(renameWorkspace(u.id, "not-a-uuid", "Team"), "not_found");
    expect(await getMembership(u.id, "not-a-uuid")).toBeNull();
  });

  it("lets an admin promote, demote and remove other members", async () => {
    const { id, admin, member } = await sharedWorkspace();
    await changeRole(admin.id, id, member.id, "admin");
    expect((await getMembership(member.id, id))?.role).toBe("admin");
    await changeRole(member.id, id, admin.id, "member");
    await removeMember(member.id, id, admin.id);
    expect(await getMembership(admin.id, id)).toBeNull();
  });

  it("rejects unknown roles", async () => {
    const { id, admin, member } = await sharedWorkspace();
    await rejectsWith(changeRole(admin.id, id, member.id, "owner"), "invalid_role");
  });

  it("lists workspaces with role, personal first", async () => {
    const u = await users.create();
    const personal = await ensurePersonalWorkspace(u.id);
    const team = await createWorkspace(u.id, "A team");
    expect(await listWorkspaces(u.id)).toEqual([
      { id: personal, name: "Personal", personal: true, role: "admin" },
      { id: team, name: "A team", personal: false, role: "admin" },
    ]);
  });

  it("lists members to members", async () => {
    const { id, admin, member } = await sharedWorkspace();
    expect(await listMembers(member.id, id)).toEqual([
      { userId: admin.id, name: "Admin", email: admin.email, role: "admin" },
      { userId: member.id, name: "Member", email: member.email, role: "member" },
    ]);
  });
});

describe("R5: at least one admin", () => {
  it("keeps the last admin from leaving, being removed or demoted", async () => {
    const { id, admin } = await sharedWorkspace();
    await rejectsWith(leaveWorkspace(admin.id, id), "last_admin");
    await rejectsWith(removeMember(admin.id, id, admin.id), "last_admin");
    await rejectsWith(changeRole(admin.id, id, admin.id, "member"), "last_admin");
  });

  it("leaves exactly one admin when two admins demote each other at once", async () => {
    const { id, admin, member } = await sharedWorkspace();
    await changeRole(admin.id, id, member.id, "admin");

    const results = await Promise.allSettled([
      changeRole(admin.id, id, member.id, "member"),
      changeRole(member.id, id, admin.id, "member"),
    ]);

    expect(results.filter((r) => r.status === "fulfilled")).toHaveLength(1);
    expect(await adminCount(id)).toBe(1);
  });

  it("stops a demoted admin from acting on a stale page", async () => {
    const { id, admin, member } = await sharedWorkspace();
    await changeRole(admin.id, id, member.id, "admin");
    await changeRole(admin.id, id, member.id, "member");
    await rejectsWith(renameWorkspace(member.id, id, "Mine"), "forbidden");
  });

  it("lets a member leave", async () => {
    const { id, member } = await sharedWorkspace();
    await leaveWorkspace(member.id, id);
    expect(await getMembership(member.id, id)).toBeNull();
  });
});

describe("R8: deleting a workspace", () => {
  it("requires the exact name and removes the memberships", async () => {
    const { id, admin } = await sharedWorkspace();
    await rejectsWith(deleteWorkspace(admin.id, id, "team"), "confirmation_mismatch");
    await deleteWorkspace(admin.id, id, "  Team ");
    expect(await db.$count(workspace, eq(workspace.id, id))).toBe(0);
    expect(await db.$count(workspaceMember, eq(workspaceMember.workspaceId, id))).toBe(0);
  });
});
```

- [ ] **Step 3: Run it to see it fail**

Run: `task test -- --project integration src/server/workspaces/workspaces.int.test.ts`
Expected: FAIL (cannot resolve `./errors`, `./internal`, `./workspaces`).

- [ ] **Step 4: Errors** — `src/server/workspaces/errors.ts`:

```ts
import "server-only";

export const WORKSPACE_ERROR_CODES = [
  "not_found",
  "forbidden",
  "personal_workspace",
  "last_admin",
  "already_member",
  "already_invited",
  "user_not_found",
  "invalid_name",
  "invalid_query",
  "invalid_role",
  "confirmation_mismatch",
] as const;
export type WorkspaceErrorCode = (typeof WORKSPACE_ERROR_CODES)[number];

/** A broken workspace rule. Carries only a fixed code, never internal details. */
export class WorkspaceError extends Error {
  constructor(readonly code: WorkspaceErrorCode) {
    super(code);
    this.name = "WorkspaceError";
  }
}
```

- [ ] **Step 5: Shared internals** — `src/server/workspaces/internal.ts`:

```ts
import "server-only";
import { and, eq } from "drizzle-orm";
import { getDb } from "@/server/db/client";
import { type WorkspaceRole, workspace, workspaceMember } from "@/server/db/schema";
import { WorkspaceError, type WorkspaceErrorCode } from "./errors";
import { workspaceIdSchema } from "./validation";

export type Tx = Parameters<Parameters<ReturnType<typeof getDb>["transaction"]>[0]>[0];
export type WorkspaceSummary = { id: string; name: string; personal: boolean };
export type WorkspaceAccess = { workspace: WorkspaceSummary; role: WorkspaceRole };

/** A uuid from request input; anything else is `code` (default not_found), before any query. */
export function parseId(value: unknown, code: WorkspaceErrorCode = "not_found"): string {
  const parsed = workspaceIdSchema.safeParse(value);
  if (!parsed.success) throw new WorkspaceError(code);
  return parsed.data;
}

const summaryColumns = {
  id: workspace.id,
  name: workspace.name,
  personalUserId: workspace.personalUserId,
};

function toSummary(row: { id: string; name: string; personalUserId: string | null }) {
  return { id: row.id, name: row.name, personal: row.personalUserId !== null };
}

/**
 * Locks the workspace row, then reads the actor's role. Every mutating operation starts here, so
 * concurrent changes to one workspace run one after another and see each other's results.
 */
export async function lockForActor(
  tx: Tx,
  workspaceId: unknown,
  actorId: string,
): Promise<WorkspaceAccess> {
  const id = parseId(workspaceId);
  const [row] = await tx
    .select(summaryColumns)
    .from(workspace)
    .where(eq(workspace.id, id))
    .for("update");
  if (!row) throw new WorkspaceError("not_found");
  const [member] = await tx
    .select({ role: workspaceMember.role })
    .from(workspaceMember)
    .where(and(eq(workspaceMember.workspaceId, id), eq(workspaceMember.userId, actorId)));
  if (!member) throw new WorkspaceError("not_found");
  return { workspace: toSummary(row), role: member.role };
}

export async function lockForAdmin(
  tx: Tx,
  workspaceId: unknown,
  actorId: string,
): Promise<WorkspaceAccess> {
  const access = await lockForActor(tx, workspaceId, actorId);
  if (access.role !== "admin") throw new WorkspaceError("forbidden");
  return access;
}

/** The user's membership, or null for non-members and malformed ids. For reads; no lock. */
export async function getMembership(
  userId: string,
  workspaceId: unknown,
): Promise<WorkspaceAccess | null> {
  const parsed = workspaceIdSchema.safeParse(workspaceId);
  if (!parsed.success) return null;
  const [row] = await getDb()
    .select({ ...summaryColumns, role: workspaceMember.role })
    .from(workspace)
    .innerJoin(workspaceMember, eq(workspaceMember.workspaceId, workspace.id))
    .where(and(eq(workspace.id, parsed.data), eq(workspaceMember.userId, userId)));
  return row ? { workspace: toSummary(row), role: row.role } : null;
}

export async function requireMembership(
  userId: string,
  workspaceId: unknown,
): Promise<WorkspaceAccess> {
  const access = await getMembership(userId, workspaceId);
  if (!access) throw new WorkspaceError("not_found");
  return access;
}

/** Number of admins; call only with the workspace locked. */
export async function countAdmins(tx: Tx, workspaceId: string): Promise<number> {
  return tx.$count(
    workspaceMember,
    and(eq(workspaceMember.workspaceId, workspaceId), eq(workspaceMember.role, "admin")),
  );
}
```

- [ ] **Step 6: Workspace operations** — `src/server/workspaces/workspaces.ts`:

```ts
import "server-only";
import { and, asc, desc, eq, sql } from "drizzle-orm";
import { user } from "@/server/db/auth-schema";
import { getDb } from "@/server/db/client";
import { type WorkspaceRole, workspace, workspaceMember } from "@/server/db/schema";
import { WorkspaceError } from "./errors";
import {
  countAdmins,
  lockForActor,
  lockForAdmin,
  parseId,
  requireMembership,
  type Tx,
  type WorkspaceSummary,
} from "./internal";
import { PERSONAL_WORKSPACE_NAME, workspaceNameSchema, workspaceRoleSchema } from "./validation";

function parseName(name: unknown): string {
  const parsed = workspaceNameSchema.safeParse(name);
  if (!parsed.success) throw new WorkspaceError("invalid_name");
  return parsed.data;
}

/**
 * The user's personal workspace id, creating it (with the user as admin) if missing (R1). Safe to
 * call concurrently: the unique index on personal_user_id lets one insert win.
 */
export async function ensurePersonalWorkspace(userId: string): Promise<string> {
  const db = getDb();
  const find = async () => {
    const [row] = await db
      .select({ id: workspace.id })
      .from(workspace)
      .where(eq(workspace.personalUserId, userId));
    return row?.id;
  };
  const existing = await find();
  if (existing) return existing;

  const created = await db.transaction(async (tx) => {
    const [row] = await tx
      .insert(workspace)
      .values({ name: PERSONAL_WORKSPACE_NAME, personalUserId: userId })
      .onConflictDoNothing({ target: workspace.personalUserId })
      .returning({ id: workspace.id });
    if (row) await tx.insert(workspaceMember).values({ workspaceId: row.id, userId, role: "admin" });
    return row?.id;
  });
  // Lost the race: another call committed the workspace (and its membership) first.
  const id = created ?? (await find());
  if (!id) throw new Error("personal workspace missing after insert");
  return id;
}

export async function createWorkspace(actorId: string, name: string): Promise<string> {
  const validName = parseName(name);
  return getDb().transaction(async (tx) => {
    const [row] = await tx
      .insert(workspace)
      .values({ name: validName })
      .returning({ id: workspace.id });
    if (!row) throw new Error("workspace insert returned nothing");
    await tx.insert(workspaceMember).values({ workspaceId: row.id, userId: actorId, role: "admin" });
    return row.id;
  });
}

export async function renameWorkspace(
  actorId: string,
  workspaceId: string,
  name: string,
): Promise<void> {
  const validName = parseName(name);
  await getDb().transaction(async (tx) => {
    const { workspace: ws } = await lockForAdmin(tx, workspaceId, actorId);
    await tx.update(workspace).set({ name: validName }).where(eq(workspace.id, ws.id));
  });
}

/** R2, R8: not for personal workspaces; `confirmation` must equal the name after trimming. */
export async function deleteWorkspace(
  actorId: string,
  workspaceId: string,
  confirmation: string,
): Promise<void> {
  await getDb().transaction(async (tx) => {
    const { workspace: ws } = await lockForAdmin(tx, workspaceId, actorId);
    if (ws.personal) throw new WorkspaceError("personal_workspace");
    if (String(confirmation).trim() !== ws.name) throw new WorkspaceError("confirmation_mismatch");
    await tx.delete(workspace).where(eq(workspace.id, ws.id));
  });
}

export async function listWorkspaces(
  userId: string,
): Promise<(WorkspaceSummary & { role: WorkspaceRole })[]> {
  const personal = sql<boolean>`${workspace.personalUserId} is not null`;
  return getDb()
    .select({ id: workspace.id, name: workspace.name, personal, role: workspaceMember.role })
    .from(workspace)
    .innerJoin(workspaceMember, eq(workspaceMember.workspaceId, workspace.id))
    .where(eq(workspaceMember.userId, userId))
    .orderBy(desc(personal), asc(workspace.name), asc(workspace.id));
}

export async function listMembers(
  actorId: string,
  workspaceId: string,
): Promise<{ userId: string; name: string; email: string; role: WorkspaceRole }[]> {
  const { workspace: ws } = await requireMembership(actorId, workspaceId);
  return getDb()
    .select({
      userId: workspaceMember.userId,
      name: user.name,
      email: user.email,
      role: workspaceMember.role,
    })
    .from(workspaceMember)
    .innerJoin(user, eq(user.id, workspaceMember.userId))
    .where(eq(workspaceMember.workspaceId, ws.id))
    .orderBy(asc(user.name), asc(user.email));
}

/** The target's current role; not_found if they are not a member. */
async function memberRole(tx: Tx, workspaceId: string, userId: string): Promise<WorkspaceRole> {
  const [row] = await tx
    .select({ role: workspaceMember.role })
    .from(workspaceMember)
    .where(and(eq(workspaceMember.workspaceId, workspaceId), eq(workspaceMember.userId, userId)));
  if (!row) throw new WorkspaceError("not_found");
  return row.role;
}

/** R5: the last admin may not stop being admin. Call with the workspace locked. */
async function assertNotLastAdmin(tx: Tx, workspaceId: string, role: WorkspaceRole) {
  if (role === "admin" && (await countAdmins(tx, workspaceId)) <= 1) {
    throw new WorkspaceError("last_admin");
  }
}

export async function changeRole(
  actorId: string,
  workspaceId: string,
  memberUserId: string,
  role: string,
): Promise<void> {
  const parsedRole = workspaceRoleSchema.safeParse(role);
  if (!parsedRole.success) throw new WorkspaceError("invalid_role");
  const targetId = parseId(memberUserId);
  await getDb().transaction(async (tx) => {
    const { workspace: ws } = await lockForAdmin(tx, workspaceId, actorId);
    const current = await memberRole(tx, ws.id, targetId);
    if (current === parsedRole.data) return;
    await assertNotLastAdmin(tx, ws.id, current);
    await tx
      .update(workspaceMember)
      .set({ role: parsedRole.data })
      .where(and(eq(workspaceMember.workspaceId, ws.id), eq(workspaceMember.userId, targetId)));
  });
}

/** Removing oneself is leaving (allowed for members too). */
export async function removeMember(
  actorId: string,
  workspaceId: string,
  memberUserId: string,
): Promise<void> {
  if (memberUserId === actorId) return leaveWorkspace(actorId, workspaceId);
  const targetId = parseId(memberUserId);
  await getDb().transaction(async (tx) => {
    const { workspace: ws } = await lockForAdmin(tx, workspaceId, actorId);
    await assertNotLastAdmin(tx, ws.id, await memberRole(tx, ws.id, targetId));
    await tx
      .delete(workspaceMember)
      .where(and(eq(workspaceMember.workspaceId, ws.id), eq(workspaceMember.userId, targetId)));
  });
}

/** R2 needs no own check here: a personal workspace's only member is its last admin (R5). */
export async function leaveWorkspace(actorId: string, workspaceId: string): Promise<void> {
  await getDb().transaction(async (tx) => {
    const { workspace: ws, role } = await lockForActor(tx, workspaceId, actorId);
    await assertNotLastAdmin(tx, ws.id, role);
    await tx
      .delete(workspaceMember)
      .where(and(eq(workspaceMember.workspaceId, ws.id), eq(workspaceMember.userId, actorId)));
  });
}
```

Note on the R2 test for `changeRole(u.id, id, u.id, "member")` and `removeMember(u.id, id, u.id)` in a personal workspace: both answer `last_admin` through R5, as the spec says.

- [ ] **Step 7: Run the integration test**

Run: `task test -- --project integration src/server/workspaces/workspaces.int.test.ts`
Expected: PASS (all tests). If the concurrent-demotion test fails with a deadlock or serialization error, the lock order is wrong: every function must lock the workspace row before reading or writing member rows.

- [ ] **Step 8: Full check and commit**

Run: `task format`, then `task ci` → all green.

```bash
git add src/server/workspaces/errors.ts src/server/workspaces/internal.ts src/server/workspaces/workspaces.ts src/server/workspaces/workspaces.int.test.ts tests/support/users.ts
git commit -m "feat(workspaces): workspace and membership rules

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Invitations and user search

**Files:**
- Create: `src/server/workspaces/invitations.ts`
- Test: `src/server/workspaces/invitations.int.test.ts`

**Interfaces:**
- Consumes (Tasks 1–2): `workspaceInvitation`, `workspaceMember`, `user`; `lockForAdmin`, `requireMembership`, `parseId`, `WorkspaceError`, `likePattern`, `userSearchQuerySchema`; `createWorkspace`, `ensurePersonalWorkspace`, `testUsers` (tests).
- Produces:
  - `searchUsersToInvite(actorId: string, workspaceId: string, query: string): Promise<{ id: string; name: string; email: string }[]>`
  - `inviteUser(actorId: string, workspaceId: string, inviteeId: string): Promise<void>`
  - `cancelInvitation(actorId: string, workspaceId: string, invitationId: string): Promise<void>`
  - `listInvitationsForWorkspace(actorId: string, workspaceId: string): Promise<{ id: string; userId: string; name: string; email: string }[]>`
  - `listInvitationsForUser(userId: string): Promise<{ id: string; workspaceId: string; workspaceName: string; invitedBy: string | null }[]>` (`invitedBy` = inviter's name, or email if the name is empty, or null if the inviter is gone)
  - `acceptInvitation(actorId: string, invitationId: string): Promise<string>` (workspace id)
  - `declineInvitation(actorId: string, invitationId: string): Promise<void>`

- [ ] **Step 1: Write the failing integration test** — `src/server/workspaces/invitations.int.test.ts`:

```ts
import { eq } from "drizzle-orm";
import { afterAll, describe, expect, it } from "vitest";
import { getDb } from "@/server/db/client";
import { workspaceInvitation } from "@/server/db/schema";
import { testUsers } from "../../../tests/support/users";
import { WorkspaceError } from "./errors";
import { getMembership } from "./internal";
import {
  acceptInvitation,
  cancelInvitation,
  declineInvitation,
  inviteUser,
  listInvitationsForUser,
  listInvitationsForWorkspace,
  searchUsersToInvite,
} from "./invitations";
import { changeRole, createWorkspace, deleteWorkspace, ensurePersonalWorkspace } from "./workspaces";

const users = testUsers();
const db = getDb();
afterAll(() => users.cleanup());

const rejectsWith = (promise: Promise<unknown>, code: string) =>
  expect(promise).rejects.toSatisfy((e) => e instanceof WorkspaceError && e.code === code);

async function setup() {
  const admin = await users.create("Admin");
  const invitee = await users.create("Invitee");
  const id = await createWorkspace(admin.id, "Team");
  return { id, admin, invitee };
}

const invitationIdFor = async (userId: string) => {
  const [first] = await listInvitationsForUser(userId);
  if (!first) throw new Error("no invitation");
  return first.id;
};

describe("R6: inviting existing users", () => {
  it("finds users by name or email, without members, invitees or more than 10 results", async () => {
    const { id, admin, invitee } = await setup();
    const tag = invitee.id.slice(0, 8);
    await Promise.all(Array.from({ length: 11 }, (_, i) => users.create(`Bulk ${tag} ${i}`)));

    expect(await searchUsersToInvite(admin.id, id, invitee.email)).toEqual([
      { id: invitee.id, name: "Invitee", email: invitee.email },
    ]);
    expect(await searchUsersToInvite(admin.id, id, admin.email)).toEqual([]);
    expect(await searchUsersToInvite(admin.id, id, `Bulk ${tag}`)).toHaveLength(10);

    await inviteUser(admin.id, id, invitee.id);
    expect(await searchUsersToInvite(admin.id, id, invitee.email)).toEqual([]);
  });

  it("matches wildcards literally", async () => {
    const { id, admin } = await setup();
    const odd = await users.create(`100%_${admin.id.slice(0, 8)}`);
    expect(await searchUsersToInvite(admin.id, id, `100%_${admin.id.slice(0, 8)}`)).toEqual([
      { id: odd.id, name: odd.name, email: odd.email },
    ]);
    expect(await searchUsersToInvite(admin.id, id, "%%")).toEqual([]);
    const tag = admin.id.slice(0, 8);
    await users.create(`ab_cd ${tag}`);
    expect(await searchUsersToInvite(admin.id, id, `abXcd ${tag}`)).toEqual([]);
  });

  it("is only for admins of shared workspaces", async () => {
    const { id, admin, invitee } = await setup();
    await inviteUser(admin.id, id, invitee.id);
    await acceptInvitation(invitee.id, await invitationIdFor(invitee.id));
    await rejectsWith(searchUsersToInvite(invitee.id, id, "Admin"), "forbidden");

    const personal = await ensurePersonalWorkspace(admin.id);
    await rejectsWith(searchUsersToInvite(admin.id, personal, "Invitee"), "personal_workspace");
    await rejectsWith(inviteUser(admin.id, personal, invitee.id), "personal_workspace");
  });

  it("rejects short queries and unknown users", async () => {
    const { id, admin } = await setup();
    await rejectsWith(searchUsersToInvite(admin.id, id, " a "), "invalid_query");
    await rejectsWith(
      inviteUser(admin.id, id, "6f1c1f7e-3d4b-4c55-9a43-1b2a5c6d7e8f"),
      "user_not_found",
    );
    await rejectsWith(inviteUser(admin.id, id, "nope"), "user_not_found");
  });

  it("rejects duplicates and members", async () => {
    const { id, admin, invitee } = await setup();
    await inviteUser(admin.id, id, invitee.id);
    await rejectsWith(inviteUser(admin.id, id, invitee.id), "already_invited");
    await rejectsWith(inviteUser(admin.id, id, admin.id), "already_member");
  });
});

describe("R4/R5: who may invite", () => {
  it("lets members neither invite nor cancel", async () => {
    const { id, admin, invitee } = await setup();
    const member = await users.create();
    const other = await users.create();
    await inviteUser(admin.id, id, member.id);
    await acceptInvitation(member.id, await invitationIdFor(member.id));
    await inviteUser(admin.id, id, invitee.id);
    const invitationId = await invitationIdFor(invitee.id);

    await rejectsWith(inviteUser(member.id, id, other.id), "forbidden");
    await rejectsWith(cancelInvitation(member.id, id, invitationId), "forbidden");
  });

  it("answers not_found to non-members", async () => {
    const { id, admin, invitee } = await setup();
    const stranger = await users.create();
    await inviteUser(admin.id, id, invitee.id);
    const invitationId = await invitationIdFor(invitee.id);

    await rejectsWith(searchUsersToInvite(stranger.id, id, "Invitee"), "not_found");
    await rejectsWith(inviteUser(stranger.id, id, invitee.id), "not_found");
    await rejectsWith(cancelInvitation(stranger.id, id, invitationId), "not_found");
  });

  it("stops a demoted admin from inviting", async () => {
    const { id, admin, invitee } = await setup();
    const second = await users.create();
    await inviteUser(admin.id, id, second.id);
    await acceptInvitation(second.id, await invitationIdFor(second.id));
    await changeRole(admin.id, id, second.id, "admin");
    await changeRole(admin.id, id, second.id, "member");

    await rejectsWith(inviteUser(second.id, id, invitee.id), "forbidden");
  });
});

describe("R7: answering invitations", () => {
  it("makes the invitee a member and removes the invitation", async () => {
    const { id, admin, invitee } = await setup();
    await inviteUser(admin.id, id, invitee.id);
    expect(await listInvitationsForUser(invitee.id)).toEqual([
      { id: expect.any(String), workspaceId: id, workspaceName: "Team", invitedBy: "Admin" },
    ]);

    expect(await acceptInvitation(invitee.id, await invitationIdFor(invitee.id))).toBe(id);
    expect((await getMembership(invitee.id, id))?.role).toBe("member");
    expect(await listInvitationsForUser(invitee.id)).toEqual([]);
  });

  it("can only be answered by the invitee", async () => {
    const { id, admin, invitee } = await setup();
    await inviteUser(admin.id, id, invitee.id);
    const invitationId = await invitationIdFor(invitee.id);
    await rejectsWith(acceptInvitation(admin.id, invitationId), "not_found");
    await rejectsWith(declineInvitation(admin.id, invitationId), "not_found");
  });

  it("declining deletes the invitation, so the user can be invited again", async () => {
    const { id, admin, invitee } = await setup();
    await inviteUser(admin.id, id, invitee.id);
    await declineInvitation(invitee.id, await invitationIdFor(invitee.id));
    expect(await listInvitationsForWorkspace(admin.id, id)).toEqual([]);
    await inviteUser(admin.id, id, invitee.id);
  });

  it("accepting a cancelled invitation fails without a membership", async () => {
    const { id, admin, invitee } = await setup();
    await inviteUser(admin.id, id, invitee.id);
    const invitationId = await invitationIdFor(invitee.id);
    await cancelInvitation(admin.id, id, invitationId);

    await rejectsWith(acceptInvitation(invitee.id, invitationId), "not_found");
    expect(await getMembership(invitee.id, id)).toBeNull();
  });

  it("lists a workspace's invitations to admins only", async () => {
    const { id, admin, invitee } = await setup();
    await inviteUser(admin.id, id, invitee.id);
    expect(await listInvitationsForWorkspace(admin.id, id)).toEqual([
      { id: expect.any(String), userId: invitee.id, name: "Invitee", email: invitee.email },
    ]);
    const stranger = await users.create();
    await rejectsWith(listInvitationsForWorkspace(stranger.id, id), "not_found");
  });
});

describe("R8: deleting a workspace", () => {
  it("removes its invitations", async () => {
    const { id, admin, invitee } = await setup();
    await inviteUser(admin.id, id, invitee.id);
    await deleteWorkspace(admin.id, id, "Team");
    expect(await db.$count(workspaceInvitation, eq(workspaceInvitation.workspaceId, id))).toBe(0);
  });
});
```

- [ ] **Step 2: Run it to see it fail**

Run: `task test -- --project integration src/server/workspaces/invitations.int.test.ts`
Expected: FAIL (cannot resolve `./invitations`).

- [ ] **Step 3: Implement** — `src/server/workspaces/invitations.ts`:

```ts
import "server-only";
import { and, asc, eq, ilike, notExists, or } from "drizzle-orm";
import { alias } from "drizzle-orm/pg-core";
import { user } from "@/server/db/auth-schema";
import { getDb } from "@/server/db/client";
import { workspace, workspaceInvitation, workspaceMember } from "@/server/db/schema";
import { WorkspaceError } from "./errors";
import { lockForAdmin, parseId, requireMembership } from "./internal";
import { likePattern, userSearchQuerySchema } from "./validation";

const SEARCH_LIMIT = 10;

/** R6: admins of shared workspaces search all users who are neither members nor invited. */
export async function searchUsersToInvite(
  actorId: string,
  workspaceId: string,
  query: string,
): Promise<{ id: string; name: string; email: string }[]> {
  const parsed = userSearchQuerySchema.safeParse(query);
  if (!parsed.success) throw new WorkspaceError("invalid_query");
  const { workspace: ws, role } = await requireMembership(actorId, workspaceId);
  if (role !== "admin") throw new WorkspaceError("forbidden");
  if (ws.personal) throw new WorkspaceError("personal_workspace");

  const db = getDb();
  const pattern = likePattern(parsed.data);
  return db
    .select({ id: user.id, name: user.name, email: user.email })
    .from(user)
    .where(
      and(
        or(ilike(user.name, pattern), ilike(user.email, pattern)),
        notExists(
          db
            .select({ userId: workspaceMember.userId })
            .from(workspaceMember)
            .where(and(eq(workspaceMember.workspaceId, ws.id), eq(workspaceMember.userId, user.id))),
        ),
        notExists(
          db
            .select({ userId: workspaceInvitation.userId })
            .from(workspaceInvitation)
            .where(
              and(eq(workspaceInvitation.workspaceId, ws.id), eq(workspaceInvitation.userId, user.id)),
            ),
        ),
      ),
    )
    .orderBy(asc(user.name), asc(user.email))
    .limit(SEARCH_LIMIT);
}

export async function inviteUser(
  actorId: string,
  workspaceId: string,
  inviteeId: string,
): Promise<void> {
  const targetId = parseId(inviteeId, "user_not_found");
  await getDb().transaction(async (tx) => {
    const { workspace: ws } = await lockForAdmin(tx, workspaceId, actorId);
    if (ws.personal) throw new WorkspaceError("personal_workspace");
    const [invitee] = await tx.select({ id: user.id }).from(user).where(eq(user.id, targetId));
    if (!invitee) throw new WorkspaceError("user_not_found");
    const [member] = await tx
      .select({ userId: workspaceMember.userId })
      .from(workspaceMember)
      .where(and(eq(workspaceMember.workspaceId, ws.id), eq(workspaceMember.userId, targetId)));
    if (member) throw new WorkspaceError("already_member");
    const [created] = await tx
      .insert(workspaceInvitation)
      .values({ workspaceId: ws.id, userId: targetId, invitedByUserId: actorId })
      .onConflictDoNothing()
      .returning({ id: workspaceInvitation.id });
    if (!created) throw new WorkspaceError("already_invited");
  });
}

/** Any admin may cancel any invitation of the workspace. */
export async function cancelInvitation(
  actorId: string,
  workspaceId: string,
  invitationId: string,
): Promise<void> {
  const id = parseId(invitationId);
  await getDb().transaction(async (tx) => {
    const { workspace: ws } = await lockForAdmin(tx, workspaceId, actorId);
    const deleted = await tx
      .delete(workspaceInvitation)
      .where(and(eq(workspaceInvitation.id, id), eq(workspaceInvitation.workspaceId, ws.id)))
      .returning({ id: workspaceInvitation.id });
    if (deleted.length === 0) throw new WorkspaceError("not_found");
  });
}

export async function listInvitationsForWorkspace(
  actorId: string,
  workspaceId: string,
): Promise<{ id: string; userId: string; name: string; email: string }[]> {
  const { workspace: ws, role } = await requireMembership(actorId, workspaceId);
  if (role !== "admin") throw new WorkspaceError("forbidden");
  return getDb()
    .select({
      id: workspaceInvitation.id,
      userId: workspaceInvitation.userId,
      name: user.name,
      email: user.email,
    })
    .from(workspaceInvitation)
    .innerJoin(user, eq(user.id, workspaceInvitation.userId))
    .where(eq(workspaceInvitation.workspaceId, ws.id))
    .orderBy(asc(user.name), asc(user.email));
}

const inviter = alias(user, "inviter");

export async function listInvitationsForUser(
  userId: string,
): Promise<{ id: string; workspaceId: string; workspaceName: string; invitedBy: string | null }[]> {
  const rows = await getDb()
    .select({
      id: workspaceInvitation.id,
      workspaceId: workspace.id,
      workspaceName: workspace.name,
      inviterName: inviter.name,
      inviterEmail: inviter.email,
    })
    .from(workspaceInvitation)
    .innerJoin(workspace, eq(workspace.id, workspaceInvitation.workspaceId))
    .leftJoin(inviter, eq(inviter.id, workspaceInvitation.invitedByUserId))
    .where(eq(workspaceInvitation.userId, userId))
    .orderBy(asc(workspace.name), asc(workspaceInvitation.id));
  return rows.map(({ inviterName, inviterEmail, ...row }) => ({
    ...row,
    invitedBy: inviterName || inviterEmail || null,
  }));
}

/**
 * R7: the invitee joins as member. The workspace is locked before the invitation is read again,
 * in the same order as every other operation, so a concurrent cancel either wins (not_found) or
 * waits.
 */
export async function acceptInvitation(actorId: string, invitationId: string): Promise<string> {
  const id = parseId(invitationId);
  const db = getDb();
  const [found] = await db
    .select({ workspaceId: workspaceInvitation.workspaceId })
    .from(workspaceInvitation)
    .where(and(eq(workspaceInvitation.id, id), eq(workspaceInvitation.userId, actorId)));
  if (!found) throw new WorkspaceError("not_found");

  return db.transaction(async (tx) => {
    await tx
      .select({ id: workspace.id })
      .from(workspace)
      .where(eq(workspace.id, found.workspaceId))
      .for("update");
    const deleted = await tx
      .delete(workspaceInvitation)
      .where(and(eq(workspaceInvitation.id, id), eq(workspaceInvitation.userId, actorId)))
      .returning({ workspaceId: workspaceInvitation.workspaceId });
    const accepted = deleted[0];
    if (!accepted) throw new WorkspaceError("not_found");
    await tx
      .insert(workspaceMember)
      .values({ workspaceId: accepted.workspaceId, userId: actorId, role: "member" })
      .onConflictDoNothing();
    return accepted.workspaceId;
  });
}

/** A single delete of the user's own invitation; needs no lock. */
export async function declineInvitation(actorId: string, invitationId: string): Promise<void> {
  const id = parseId(invitationId);
  const deleted = await getDb()
    .delete(workspaceInvitation)
    .where(and(eq(workspaceInvitation.id, id), eq(workspaceInvitation.userId, actorId)))
    .returning({ id: workspaceInvitation.id });
  if (deleted.length === 0) throw new WorkspaceError("not_found");
}
```

- [ ] **Step 4: Run the integration test**

Run: `task test -- --project integration src/server/workspaces/invitations.int.test.ts`
Expected: PASS.

- [ ] **Step 5: Full check and commit**

Run: `task format`, then `task ci` → all green.

```bash
git add src/server/workspaces/invitations.ts src/server/workspaces/invitations.int.test.ts
git commit -m "feat(workspaces): invitations and user search

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Access helpers and the personal workspace at sign-in

**Files:**
- Create: `src/server/workspaces/access.ts`
- Modify: `src/server/auth/auth.ts` (`createAuth` options and `databaseHooks`)
- Modify: `src/server/auth/auth.int.test.ts` (two tests)
- Test: `src/server/workspaces/access.int.test.ts`

**Interfaces:**
- Consumes: `getCurrentUser`, `requireUser`, `CurrentUser` (`src/server/auth/session.ts`); `getMembership`, `WorkspaceAccess` (Task 2); `ensurePersonalWorkspace` (Task 2); `inviteUser`, `acceptInvitation`, `listInvitationsForUser` (Task 3, tests).
- Produces:
  - `getWorkspaceAccess(workspaceId: string): Promise<WorkspaceAccess | null>` (signed-in user; React `cache`)
  - `requireWorkspaceMember(workspaceId: string): Promise<WorkspaceAccess & { user: CurrentUser }>` (redirects to `/sign-in` signed out; `notFound()` for non-members)
  - `requireWorkspaceAdmin(workspaceId: string): Promise<WorkspaceAccess & { user: CurrentUser }>` (`notFound()` for non-members and members)
  - `createAuth(config, { withOidc?: boolean; ensureWorkspace?: (userId: string) => Promise<unknown> })`

- [ ] **Step 1: Write the failing access test** — `src/server/workspaces/access.int.test.ts`:

```ts
import { randomUUID } from "node:crypto";
import { inArray } from "drizzle-orm";
import { afterAll, beforeEach, describe, expect, it, vi } from "vitest";
import { type AuthConfig, createAuth } from "@/server/auth/auth";
import { user } from "@/server/db/auth-schema";
import { getDb } from "@/server/db/client";
import { getEnv } from "@/server/env";
import { signInViaMock } from "../../../tests/support/oidc";
import { testUsers } from "../../../tests/support/users";
import { getWorkspaceAccess, requireWorkspaceAdmin, requireWorkspaceMember } from "./access";
import { acceptInvitation, inviteUser, listInvitationsForUser } from "./invitations";
import { createWorkspace, ensurePersonalWorkspace } from "./workspaces";

const request = vi.hoisted(() => ({ cookie: "" }));
vi.mock("next/headers", () => ({
  headers: async () => new Headers(request.cookie ? { cookie: request.cookie } : {}),
}));

const env = getEnv();
const config: AuthConfig = {
  baseURL: env.BETTER_AUTH_URL,
  secret: env.BETTER_AUTH_SECRET,
  oidc: {
    discoveryUrl: env.OIDC_DISCOVERY_URL,
    clientId: env.OIDC_CLIENT_ID,
    clientSecret: env.OIDC_CLIENT_SECRET,
  },
};
const auth = createAuth(config);
const users = testUsers();
const signedInEmails: string[] = [];

afterAll(async () => {
  await users.cleanup();
  if (signedInEmails.length > 0) {
    await getDb().delete(user).where(inArray(user.email, signedInEmails));
  }
});
beforeEach(() => {
  request.cookie = "";
});

/** Signs a fresh user in through the mock IdP and makes their session the request's. */
async function signIn() {
  const id = randomUUID();
  const email = `a-${id}@example.test`;
  signedInEmails.push(email);
  const result = await signInViaMock(auth, { baseURL: config.baseURL, sub: `sub-${id}`, email });
  request.cookie = result.cookie;
  const [row] = await getDb().select({ id: user.id }).from(user).where(inArray(user.email, [email]));
  if (!row) throw new Error("signed-in user missing");
  return row.id;
}

const notFoundDigest = { digest: expect.stringMatching(/^NEXT_HTTP_ERROR_FALLBACK;404/) };

describe("workspace access", () => {
  it("gives members their role", async () => {
    const userId = await signIn();
    const personal = await ensurePersonalWorkspace(userId);

    expect(await getWorkspaceAccess(personal)).toEqual({
      workspace: { id: personal, name: "Personal", personal: true },
      role: "admin",
    });
    expect(await requireWorkspaceMember(personal)).toMatchObject({ role: "admin", user: { id: userId } });
    expect(await requireWorkspaceAdmin(personal)).toMatchObject({ role: "admin" });
  });

  it("shows non-members the not-found page, also for malformed ids", async () => {
    await signIn();
    const owner = await users.create();
    const foreign = await createWorkspace(owner.id, "Foreign");

    expect(await getWorkspaceAccess(foreign)).toBeNull();
    await expect(requireWorkspaceMember(foreign)).rejects.toMatchObject(notFoundDigest);
    await expect(requireWorkspaceMember("not-a-uuid")).rejects.toMatchObject(notFoundDigest);
  });

  it("shows members the not-found page for admin-only pages", async () => {
    const userId = await signIn();
    const owner = await users.create();
    const team = await createWorkspace(owner.id, "Team");
    await inviteUser(owner.id, team, userId);
    const [invitation] = await listInvitationsForUser(userId);
    if (!invitation) throw new Error("no invitation");
    await acceptInvitation(userId, invitation.id);

    expect(await requireWorkspaceMember(team)).toMatchObject({ role: "member" });
    await expect(requireWorkspaceAdmin(team)).rejects.toMatchObject(notFoundDigest);
  });

  it("sends signed-out visitors to the sign-in page", async () => {
    await expect(requireWorkspaceMember(randomUUID())).rejects.toMatchObject({
      digest: expect.stringMatching(/^NEXT_REDIRECT;[a-z]+;\/sign-in;/),
    });
    expect(await getWorkspaceAccess(randomUUID())).toBeNull();
  });
});
```

- [ ] **Step 2: Add two failing tests to `src/server/auth/auth.int.test.ts`**

Add the imports `workspace, workspaceMember` from `@/server/db/schema` (and `and` from `drizzle-orm` next to `eq, inArray`). Inside `describe("OIDC sign-in", …)`, after the test "signs the same IdP subject in again as the same user with the same account", add:

```ts
  it("creates the personal workspace at the first sign-in, and only once", async () => {
    const identity = testIdentity();
    await signIn(identity);
    await signIn(identity);
    const [created] = await usersWithEmail(identity.email);
    if (!created) throw new Error("user missing");

    const personal = await db
      .select({ id: workspace.id, name: workspace.name })
      .from(workspace)
      .where(eq(workspace.personalUserId, created.id));
    expect(personal).toEqual([{ id: expect.any(String), name: "Personal" }]);
    const [membership] = await db
      .select({ role: workspaceMember.role })
      .from(workspaceMember)
      .where(
        and(eq(workspaceMember.workspaceId, personal[0]?.id ?? ""), eq(workspaceMember.userId, created.id)),
      );
    expect(membership).toEqual({ role: "admin" });
  });

  it("ends at the sign-in page without a session when the workspace setup fails", async () => {
    const failing = createAuth(config, {
      ensureWorkspace: async () => {
        throw new Error("database down");
      },
    });
    const identity = testIdentity();
    const result = await signInViaMock(failing, { baseURL, ...identity, onState: trackState });

    expect(result.status).toBe(302);
    expect(result.location).toMatch(/\/sign-in\?error=sign_in_failed/);
    const [created] = await usersWithEmail(identity.email);
    if (!created) throw new Error("user missing");
    expect(await db.$count(session, eq(session.userId, created.id))).toBe(0);
  });
```

(`personal[0]?.id ?? ""` is only there for `noUncheckedIndexedAccess`; the `toEqual` above already proved one row exists.)

- [ ] **Step 3: Run both to see them fail**

Run: `task test -- --project integration src/server/workspaces/access.int.test.ts src/server/auth/auth.int.test.ts`
Expected: FAIL (`./access` missing; no personal workspace created; `ensureWorkspace` is not an option and the failing-setup sign-in succeeds).

- [ ] **Step 4: Access helpers** — `src/server/workspaces/access.ts`:

```ts
import "server-only";
import { notFound } from "next/navigation";
import { cache } from "react";
import { type CurrentUser, getCurrentUser, requireUser } from "@/server/auth/session";
import { getMembership, type WorkspaceAccess } from "./internal";

export type { WorkspaceAccess } from "./internal";

/**
 * The signed-in user's access to a workspace, or null (signed out, not a member, malformed id).
 * Looked up once per request and workspace. Every page under /w/[workspaceId] and every later
 * workspace-owned read goes through this or the require* helpers below.
 */
export const getWorkspaceAccess = cache(
  async (workspaceId: string): Promise<WorkspaceAccess | null> => {
    const user = await getCurrentUser();
    return user ? getMembership(user.id, workspaceId) : null;
  },
);

/** Members only; everyone else sees the not-found page (signed-out visitors go to sign-in). */
export async function requireWorkspaceMember(
  workspaceId: string,
): Promise<WorkspaceAccess & { user: CurrentUser }> {
  const user = await requireUser();
  const access = await getWorkspaceAccess(workspaceId);
  if (!access) notFound();
  return { ...access, user };
}

/** Admins only; members and non-members see the not-found page. */
export async function requireWorkspaceAdmin(
  workspaceId: string,
): Promise<WorkspaceAccess & { user: CurrentUser }> {
  const access = await requireWorkspaceMember(workspaceId);
  if (access.role !== "admin") notFound();
  return access;
}
```

- [ ] **Step 5: Ensure the personal workspace at sign-in** — in `src/server/auth/auth.ts`:

Add the import `import { ensurePersonalWorkspace } from "@/server/workspaces/workspaces";`.

Change the signature and doc comment of `createAuth` to:

```ts
/**
 * Without `withOidc` the instance has no OIDC provider: sessions keep working and sign-in answers
 * PROVIDER_NOT_FOUND. `ensureWorkspace` runs at every sign-in (tests replace it).
 */
export function createAuth(
  config: AuthConfig,
  {
    withOidc = true,
    ensureWorkspace = ensurePersonalWorkspace,
  }: { withOidc?: boolean; ensureWorkspace?: (userId: string) => Promise<unknown> } = {},
) {
```

Add this property to the `betterAuth({ … })` options, directly after `onAPIError: { errorURL: "/sign-in" },`:

```ts
    databaseHooks: {
      session: {
        create: {
          // Every sign-in ensures the personal workspace (R1), before the session row exists.
          // Not user.create.after: Better Auth runs that after its transaction commits, so one
          // failure would leave the user without a workspace for good. Not session.create.after:
          // a failure there leaves an orphaned session and a raw 500. The APIError's code makes
          // the callback redirect to /sign-in?error=sign_in_failed.
          before: async (session) => {
            try {
              await ensureWorkspace(session.userId);
            } catch (error) {
              console.error(
                `Personal workspace setup failed: ${error instanceof Error ? error.name : typeof error}`,
              );
              throw new APIError("INTERNAL_SERVER_ERROR", { code: "sign_in_failed" });
            }
          },
        },
      },
    },
```

- [ ] **Step 6: Run the tests**

Run: `task test -- --project integration src/server/workspaces/access.int.test.ts src/server/auth/auth.int.test.ts`
Expected: PASS. If the failing-setup test sees status 500 instead of 302, Better Auth did not get an `APIError` with `body.code`: check that `APIError` is imported from `better-auth/api` (already imported in auth.ts) and the code is passed as `{ code: "sign_in_failed" }`.

- [ ] **Step 7: Full check and commit**

Run: `task format`, then `task ci` → all green.

```bash
git add src/server/workspaces/access.ts src/server/workspaces/access.int.test.ts src/server/auth/auth.ts src/server/auth/auth.int.test.ts
git commit -m "feat(workspaces): access helpers and personal workspace at sign-in

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Workspace pages and server actions

**Files:**
- Create: `src/components/ui/input.tsx`, `src/components/ui/label.tsx` (shadcn CLI)
- Create: `src/app/workspaces/error-messages.ts` + `src/app/workspaces/error-messages.test.ts`
- Create: `src/app/workspaces/run-action.ts`
- Create: `src/app/workspaces/actions.ts`
- Create: `src/app/workspaces/page.tsx`
- Create: `src/app/w/[workspaceId]/layout.tsx`
- Create: `src/app/w/[workspaceId]/page.tsx`
- Create: `src/app/w/[workspaceId]/settings/actions.ts`
- Create: `src/app/w/[workspaceId]/settings/page.tsx`
- Modify: `src/app/page.tsx` (signed in → personal workspace)
- Modify: `src/components/app-header.tsx` (Workspaces link with invitation count)
- Modify: `tests/e2e/auth.spec.ts` (home URL is now `/w/<id>`)
- Test: `tests/e2e/workspaces.spec.ts`

**Interfaces:**
- Consumes: everything from Tasks 2–4 (service functions by their exact names and signatures above), `requireUser`, `getCurrentUser`, `WorkspaceError`, `userSearchQuerySchema`.
- Produces: routes `/workspaces`, `/w/[workspaceId]`, `/w/[workspaceId]/settings`; `workspaceErrorMessage(code)`; `runWorkspaceAction(path, operation)`, `withError(path, code)`.

- [ ] **Step 1: Add the shadcn input and label components**

Run: `pnpm exec shadcn add input label`
Expected: `src/components/ui/input.tsx` and `src/components/ui/label.tsx` (Base UI preset from `components.json`). Run `task format` afterwards so Biome accepts them.

- [ ] **Step 2: Failing unit test for the error messages** — `src/app/workspaces/error-messages.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { workspaceErrorMessage } from "./error-messages";

describe("workspace error messages", () => {
  it("are fixed text per code", () => {
    expect(workspaceErrorMessage("last_admin")).toBe(
      "A workspace needs at least one admin. Make someone else an admin first, or delete the workspace.",
    );
  });

  it("fall back to a generic message for unknown codes, never echoing them", () => {
    expect(workspaceErrorMessage("<script>")).toBe("Something went wrong. Please try again.");
  });

  it("are absent without a code", () => {
    expect(workspaceErrorMessage(undefined)).toBeUndefined();
  });
});
```

Run: `task test -- --project unit src/app/workspaces/error-messages.test.ts` → FAIL (module missing).

- [ ] **Step 3: Error messages** — `src/app/workspaces/error-messages.ts`:

```ts
const MESSAGES: Record<string, string> = {
  forbidden: "Only admins can do that.",
  personal_workspace: "Your personal workspace can't be shared or deleted.",
  last_admin:
    "A workspace needs at least one admin. Make someone else an admin first, or delete the workspace.",
  already_member: "That user is already a member.",
  already_invited: "That user is already invited.",
  user_not_found: "That user doesn't exist.",
  invalid_name: "Names need 1 to 80 characters.",
  invalid_query: "Search needs 2 to 100 characters.",
  invalid_role: "Choose admin or member.",
  confirmation_mismatch: "Type the workspace name exactly to delete it.",
};
const FALLBACK = "Something went wrong. Please try again.";

/** Fixed text for an error code from the URL; unknown codes get the generic message. */
export function workspaceErrorMessage(code: string | null | undefined): string | undefined {
  if (!code) return undefined;
  return Object.hasOwn(MESSAGES, code) ? MESSAGES[code] : FALLBACK;
}
```

Run the unit test again → PASS.

- [ ] **Step 4: Action runner** — `src/app/workspaces/run-action.ts`:

```ts
import "server-only";
import { notFound, redirect } from "next/navigation";
import { WorkspaceError } from "@/server/workspaces/errors";

/** `path` with `?error=<code>` added (other query parameters, like `q`, are kept). */
export function withError(path: string, code: string): string {
  const url = new URL(path, "http://localhost");
  url.searchParams.set("error", code);
  return `${url.pathname}${url.search}`;
}

/**
 * Runs a workspace operation for a server action. Rule violations go back to `path` with their
 * code (not_found shows the not-found page); anything else is logged by class and shown as the
 * generic message.
 */
export async function runWorkspaceAction<T>(path: string, operation: () => Promise<T>): Promise<T> {
  try {
    return await operation();
  } catch (error) {
    if (error instanceof WorkspaceError) {
      if (error.code === "not_found") notFound();
      redirect(withError(path, error.code));
    }
    console.error(`Workspace action failed: ${error instanceof Error ? error.name : typeof error}`);
    redirect(withError(path, "unexpected"));
  }
}

/** A form field as a string ("" when missing, or when a tampered call sent no FormData). */
export const field = (formData: unknown, name: string) =>
  formData instanceof FormData ? String(formData.get(name) ?? "") : "";
```

- [ ] **Step 5: `/workspaces` actions** — `src/app/workspaces/actions.ts`:

```ts
"use server";

import { redirect } from "next/navigation";
import { requireUser } from "@/server/auth/session";
import { acceptInvitation, declineInvitation } from "@/server/workspaces/invitations";
import { createWorkspace } from "@/server/workspaces/workspaces";
import { field, runWorkspaceAction } from "./run-action";

const PATH = "/workspaces";

export async function createWorkspaceAction(formData: FormData): Promise<never> {
  const user = await requireUser();
  const id = await runWorkspaceAction(PATH, () => createWorkspace(user.id, field(formData, "name")));
  redirect(`/w/${id}`);
}

export async function acceptInvitationAction(formData: FormData): Promise<never> {
  const user = await requireUser();
  const id = await runWorkspaceAction(PATH, () =>
    acceptInvitation(user.id, field(formData, "invitationId")),
  );
  redirect(`/w/${id}`);
}

export async function declineInvitationAction(formData: FormData): Promise<never> {
  const user = await requireUser();
  await runWorkspaceAction(PATH, () => declineInvitation(user.id, field(formData, "invitationId")));
  redirect(PATH);
}
```

- [ ] **Step 6: `/workspaces` page** — `src/app/workspaces/page.tsx`:

```tsx
import type { Metadata } from "next";
import Link from "next/link";
import { Suspense } from "react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { requireUser } from "@/server/auth/session";
import { listInvitationsForUser } from "@/server/workspaces/invitations";
import { listWorkspaces } from "@/server/workspaces/workspaces";
import { acceptInvitationAction, createWorkspaceAction, declineInvitationAction } from "./actions";
import { workspaceErrorMessage } from "./error-messages";

export const metadata: Metadata = { title: "Workspaces · Agenty" };

export default function WorkspacesPage({ searchParams }: PageProps<"/workspaces">) {
  return (
    <Suspense>
      <Workspaces searchParams={searchParams} />
    </Suspense>
  );
}

const first = (value: string | string[] | undefined) => (Array.isArray(value) ? value[0] : value);

async function Workspaces({ searchParams }: { searchParams: PageProps<"/workspaces">["searchParams"] }) {
  const user = await requireUser();
  const [workspaces, invitations, params] = await Promise.all([
    listWorkspaces(user.id),
    listInvitationsForUser(user.id),
    searchParams,
  ]);
  const error = workspaceErrorMessage(first(params.error));

  return (
    <div className="flex flex-col gap-6">
      <h1 className="font-semibold text-2xl tracking-tight">Workspaces</h1>
      {error ? (
        <p className="text-destructive text-sm" role="alert">
          {error}
        </p>
      ) : null}

      {invitations.length > 0 ? (
        <Card>
          <CardHeader>
            <CardTitle>Invitations</CardTitle>
          </CardHeader>
          <CardContent>
            <ul className="flex flex-col gap-3">
              {invitations.map((invitation) => (
                <li className="flex items-center justify-between gap-4" key={invitation.id}>
                  <span>
                    <span className="font-medium">{invitation.workspaceName}</span>
                    {invitation.invitedBy ? (
                      <span className="text-muted-foreground"> · invited by {invitation.invitedBy}</span>
                    ) : null}
                  </span>
                  <span className="flex gap-2">
                    <form action={acceptInvitationAction}>
                      <input name="invitationId" type="hidden" value={invitation.id} />
                      <Button type="submit">Accept</Button>
                    </form>
                    <form action={declineInvitationAction}>
                      <input name="invitationId" type="hidden" value={invitation.id} />
                      <Button type="submit" variant="outline">
                        Decline
                      </Button>
                    </form>
                  </span>
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle>Your workspaces</CardTitle>
        </CardHeader>
        <CardContent>
          <ul className="flex flex-col gap-2">
            {workspaces.map((ws) => (
              <li className="flex items-center gap-3" key={ws.id}>
                <Link className="font-medium hover:underline" href={`/w/${ws.id}`}>
                  {ws.name}
                </Link>
                <span className="text-muted-foreground text-sm">
                  {ws.role === "admin" ? "Admin" : "Member"}
                </span>
                {ws.personal ? (
                  <span className="rounded-full border px-2 text-muted-foreground text-xs">Personal</span>
                ) : null}
              </li>
            ))}
          </ul>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Create a workspace</CardTitle>
        </CardHeader>
        <CardContent>
          <form action={createWorkspaceAction} className="flex items-end gap-3">
            <div className="flex flex-1 flex-col gap-2">
              <Label htmlFor="create-name">Name</Label>
              <Input id="create-name" maxLength={80} name="name" required />
            </div>
            <Button type="submit">Create</Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
```

- [ ] **Step 7: Workspace layout and home** — `src/app/w/[workspaceId]/layout.tsx`:

```tsx
import Link from "next/link";
import { Suspense } from "react";
import { getWorkspaceAccess } from "@/server/workspaces/access";

/**
 * Workspace name and navigation. Presentation only: layouts don't re-render on navigation and
 * don't protect nested pages or actions, so every page checks membership itself.
 */
export default function WorkspaceLayout({ children, params }: LayoutProps<"/w/[workspaceId]">) {
  return (
    <>
      <Suspense>
        <WorkspaceNav params={params} />
      </Suspense>
      {children}
    </>
  );
}

async function WorkspaceNav({ params }: { params: LayoutProps<"/w/[workspaceId]">["params"] }) {
  const { workspaceId } = await params;
  const access = await getWorkspaceAccess(workspaceId);
  if (!access) return null;
  const base = `/w/${access.workspace.id}`;

  return (
    <nav aria-label="Workspace" className="flex items-center justify-between gap-4 border-b pb-3">
      <h1 className="font-semibold text-2xl tracking-tight">{access.workspace.name}</h1>
      <div className="flex gap-4 text-sm">
        <Link className="hover:underline" href={base}>
          Home
        </Link>
        <Link className="hover:underline" href={`${base}/settings`}>
          Settings
        </Link>
      </div>
    </nav>
  );
}
```

`src/app/w/[workspaceId]/page.tsx`:

```tsx
import { Suspense } from "react";
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { requireWorkspaceMember } from "@/server/workspaces/access";

export default function WorkspacePage({ params }: PageProps<"/w/[workspaceId]">) {
  return (
    <Suspense>
      <WorkspaceHome params={params} />
    </Suspense>
  );
}

async function WorkspaceHome({ params }: { params: PageProps<"/w/[workspaceId]">["params"] }) {
  const { workspaceId } = await params;
  const { role } = await requireWorkspaceMember(workspaceId);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Agents arrive in the next milestone</CardTitle>
        <CardDescription>
          You are {role === "admin" ? "an admin" : "a member"} of this workspace.
        </CardDescription>
      </CardHeader>
    </Card>
  );
}
```

- [ ] **Step 8: Settings actions** — `src/app/w/[workspaceId]/settings/actions.ts`:

```ts
"use server";

import { redirect } from "next/navigation";
import { field, runWorkspaceAction } from "@/app/workspaces/run-action";
import { requireUser } from "@/server/auth/session";
import { cancelInvitation, inviteUser } from "@/server/workspaces/invitations";
import {
  changeRole,
  deleteWorkspace,
  leaveWorkspace,
  removeMember,
  renameWorkspace,
} from "@/server/workspaces/workspaces";

// workspaceId is bound in the page but still client input: the service checks membership.
function settingsPath(workspaceId: string, q = "") {
  const path = `/w/${encodeURIComponent(String(workspaceId))}/settings`;
  return q ? `${path}?q=${encodeURIComponent(q)}` : path;
}

export async function renameWorkspaceAction(workspaceId: string, formData: FormData): Promise<never> {
  const user = await requireUser();
  const path = settingsPath(workspaceId);
  await runWorkspaceAction(path, () => renameWorkspace(user.id, workspaceId, field(formData, "name")));
  redirect(path);
}

export async function inviteUserAction(workspaceId: string, formData: FormData): Promise<never> {
  const user = await requireUser();
  const path = settingsPath(workspaceId, field(formData, "q"));
  await runWorkspaceAction(path, () => inviteUser(user.id, workspaceId, field(formData, "userId")));
  redirect(path);
}

export async function cancelInvitationAction(workspaceId: string, formData: FormData): Promise<never> {
  const user = await requireUser();
  const path = settingsPath(workspaceId, field(formData, "q"));
  await runWorkspaceAction(path, () =>
    cancelInvitation(user.id, workspaceId, field(formData, "invitationId")),
  );
  redirect(path);
}

export async function changeRoleAction(workspaceId: string, formData: FormData): Promise<never> {
  const user = await requireUser();
  const path = settingsPath(workspaceId);
  await runWorkspaceAction(path, () =>
    changeRole(user.id, workspaceId, field(formData, "userId"), field(formData, "role")),
  );
  redirect(path);
}

export async function removeMemberAction(workspaceId: string, formData: FormData): Promise<never> {
  const user = await requireUser();
  const path = settingsPath(workspaceId);
  await runWorkspaceAction(path, () => removeMember(user.id, workspaceId, field(formData, "userId")));
  redirect(path);
}

export async function leaveWorkspaceAction(workspaceId: string): Promise<never> {
  const user = await requireUser();
  await runWorkspaceAction(settingsPath(workspaceId), () => leaveWorkspace(user.id, workspaceId));
  redirect("/workspaces");
}

export async function deleteWorkspaceAction(workspaceId: string, formData: FormData): Promise<never> {
  const user = await requireUser();
  await runWorkspaceAction(settingsPath(workspaceId), () =>
    deleteWorkspace(user.id, workspaceId, field(formData, "confirmation")),
  );
  redirect("/workspaces");
}
```

- [ ] **Step 9: Settings page** — `src/app/w/[workspaceId]/settings/page.tsx`:

```tsx
import type { Metadata } from "next";
import { Suspense } from "react";
import { workspaceErrorMessage } from "@/app/workspaces/error-messages";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { requireWorkspaceMember, type WorkspaceAccess } from "@/server/workspaces/access";
import { listInvitationsForWorkspace, searchUsersToInvite } from "@/server/workspaces/invitations";
import { userSearchQuerySchema } from "@/server/workspaces/validation";
import { listMembers } from "@/server/workspaces/workspaces";
import {
  cancelInvitationAction,
  changeRoleAction,
  deleteWorkspaceAction,
  inviteUserAction,
  leaveWorkspaceAction,
  removeMemberAction,
  renameWorkspaceAction,
} from "./actions";

export const metadata: Metadata = { title: "Workspace settings · Agenty" };

type Props = PageProps<"/w/[workspaceId]/settings">;

export default function SettingsPage({ params, searchParams }: Props) {
  return (
    <Suspense>
      <Settings params={params} searchParams={searchParams} />
    </Suspense>
  );
}

const first = (value: string | string[] | undefined) => (Array.isArray(value) ? value[0] : value);
const displayName = (person: { name: string; email: string }) => person.name || person.email;
const selectClass = "h-8 rounded-md border border-input bg-transparent px-2 text-sm";

async function Settings({ params, searchParams }: Pick<Props, "params" | "searchParams">) {
  const { workspaceId } = await params;
  const access = await requireWorkspaceMember(workspaceId);
  const query = await searchParams;
  const error = workspaceErrorMessage(first(query.error));
  const isAdmin = access.role === "admin";
  const { personal } = access.workspace;

  return (
    <div className="flex flex-col gap-6">
      <h2 className="font-semibold text-xl">Settings</h2>
      {error ? (
        <p className="text-destructive text-sm" role="alert">
          {error}
        </p>
      ) : null}
      {isAdmin ? <RenameCard access={access} /> : null}
      {personal ? (
        <p className="text-muted-foreground text-sm">
          This is your personal workspace. You can rename it, but not share or delete it.
        </p>
      ) : (
        <>
          <MembersCard access={access} />
          {isAdmin ? <InviteCard access={access} q={first(query.q)?.trim() ?? ""} /> : null}
          <LeaveCard access={access} />
          {isAdmin ? <DeleteCard access={access} /> : null}
        </>
      )}
    </div>
  );
}

type Access = WorkspaceAccess & { user: { id: string } };

function RenameCard({ access }: { access: Access }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Name</CardTitle>
      </CardHeader>
      <CardContent>
        <form action={renameWorkspaceAction.bind(null, access.workspace.id)} className="flex items-end gap-3">
          <div className="flex flex-1 flex-col gap-2">
            <Label htmlFor="rename-name">Workspace name</Label>
            <Input defaultValue={access.workspace.name} id="rename-name" maxLength={80} name="name" required />
          </div>
          <Button type="submit">Rename</Button>
        </form>
      </CardContent>
    </Card>
  );
}

async function MembersCard({ access }: { access: Access }) {
  const members = await listMembers(access.user.id, access.workspace.id);
  const isAdmin = access.role === "admin";
  const id = access.workspace.id;

  return (
    <Card>
      <CardHeader>
        <CardTitle>Members</CardTitle>
      </CardHeader>
      <CardContent>
        <ul className="flex flex-col gap-3">
          {members.map((member) => (
            <li className="flex flex-wrap items-center justify-between gap-3" key={member.userId}>
              <span>
                <span className="font-medium">{displayName(member)}</span>
                <span className="text-muted-foreground"> · {member.email}</span>
              </span>
              {isAdmin ? (
                <span className="flex items-center gap-2">
                  <form action={changeRoleAction.bind(null, id)} className="flex items-center gap-2">
                    <input name="userId" type="hidden" value={member.userId} />
                    <select
                      aria-label={`Role of ${displayName(member)}`}
                      className={selectClass}
                      defaultValue={member.role}
                      name="role"
                    >
                      <option value="admin">Admin</option>
                      <option value="member">Member</option>
                    </select>
                    <Button size="sm" type="submit" variant="outline">
                      Save role
                    </Button>
                  </form>
                  {member.userId === access.user.id ? null : (
                    <form action={removeMemberAction.bind(null, id)}>
                      <input name="userId" type="hidden" value={member.userId} />
                      <Button size="sm" type="submit" variant="outline">
                        Remove
                      </Button>
                    </form>
                  )}
                </span>
              ) : (
                <span className="text-muted-foreground text-sm">
                  {member.role === "admin" ? "Admin" : "Member"}
                </span>
              )}
            </li>
          ))}
        </ul>
      </CardContent>
    </Card>
  );
}

async function InviteCard({ access, q }: { access: Access; q: string }) {
  const id = access.workspace.id;
  const parsed = q ? userSearchQuerySchema.safeParse(q) : undefined;
  const [results, invitations] = await Promise.all([
    parsed?.success ? searchUsersToInvite(access.user.id, id, parsed.data) : Promise.resolve(null),
    listInvitationsForWorkspace(access.user.id, id),
  ]);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Invite</CardTitle>
        <CardDescription>Search people who have signed in to Agenty before.</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <form action={`/w/${id}/settings`} className="flex items-end gap-3">
          <div className="flex flex-1 flex-col gap-2">
            <Label htmlFor="invite-q">Search users</Label>
            <Input defaultValue={q} id="invite-q" maxLength={100} name="q" />
          </div>
          <Button type="submit" variant="outline">
            Search
          </Button>
        </form>
        {parsed && !parsed.success ? (
          <p className="text-destructive text-sm" role="alert">
            Search needs 2 to 100 characters.
          </p>
        ) : null}
        {results && results.length === 0 ? (
          <p className="text-muted-foreground text-sm">No matching users.</p>
        ) : null}
        {results && results.length > 0 ? (
          <ul aria-label="Search results" className="flex flex-col gap-2">
            {results.map((found) => (
              <li className="flex items-center justify-between gap-3" key={found.id}>
                <span>
                  <span className="font-medium">{displayName(found)}</span>
                  <span className="text-muted-foreground"> · {found.email}</span>
                </span>
                <form action={inviteUserAction.bind(null, id)}>
                  <input name="userId" type="hidden" value={found.id} />
                  <input name="q" type="hidden" value={q} />
                  <Button size="sm" type="submit">
                    Invite
                  </Button>
                </form>
              </li>
            ))}
          </ul>
        ) : null}
        {invitations.length > 0 ? (
          <div className="flex flex-col gap-2">
            <h3 className="font-medium text-sm">Pending invitations</h3>
            <ul aria-label="Pending invitations" className="flex flex-col gap-2">
              {invitations.map((invitation) => (
                <li className="flex items-center justify-between gap-3" key={invitation.id}>
                  <span>
                    {displayName(invitation)}
                    <span className="text-muted-foreground"> · {invitation.email}</span>
                  </span>
                  <form action={cancelInvitationAction.bind(null, id)}>
                    <input name="invitationId" type="hidden" value={invitation.id} />
                    <input name="q" type="hidden" value={q} />
                    <Button size="sm" type="submit" variant="outline">
                      Cancel
                    </Button>
                  </form>
                </li>
              ))}
            </ul>
          </div>
        ) : null}
      </CardContent>
    </Card>
  );
}

function LeaveCard({ access }: { access: Access }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Leave</CardTitle>
      </CardHeader>
      <CardContent>
        <form action={leaveWorkspaceAction.bind(null, access.workspace.id)}>
          <Button type="submit" variant="outline">
            Leave workspace
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}

function DeleteCard({ access }: { access: Access }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Delete</CardTitle>
        <CardDescription>Deletes the workspace for all members. This can't be undone.</CardDescription>
      </CardHeader>
      <CardContent>
        <form action={deleteWorkspaceAction.bind(null, access.workspace.id)} className="flex items-end gap-3">
          <div className="flex flex-1 flex-col gap-2">
            <Label htmlFor="delete-confirmation">Type the workspace name to confirm</Label>
            <Input autoComplete="off" id="delete-confirmation" name="confirmation" required />
          </div>
          <Button type="submit" variant="destructive">
            Delete workspace
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}
```

If `buttonVariants` has no `destructive` variant or no `sm` size in `src/components/ui/button.tsx`, use the closest existing ones (check the file; don't add variants).

- [ ] **Step 10: `/` sends signed-in users to their personal workspace** — in `src/app/page.tsx`, add imports `import { redirect } from "next/navigation";`, `import { connection } from "next/server";`, `import { ensurePersonalWorkspace } from "@/server/workspaces/workspaces";`, remove the now unused `Card, CardHeader, CardTitle` import, and replace `Home` with:

```tsx
async function Home() {
  const user = await getCurrentUser();
  if (!user) return <Landing />;

  // Sign-in already ensured the personal workspace; this covers users without one (e.g. a failed
  // setup at an earlier sign-in). After connection(): Partial Prefetching renders twice per request.
  await connection();
  redirect(`/w/${await ensurePersonalWorkspace(user.id)}`);
}
```

- [ ] **Step 11: Header link** — in `src/components/app-header.tsx`, add `import { listInvitationsForUser } from "@/server/workspaces/invitations";` and replace `Header` with:

```tsx
async function Header() {
  const user = await getCurrentUser();
  if (!user) return null;
  const invitations = await listInvitationsForUser(user.id);

  return (
    <header className="flex items-center justify-between gap-4 border-b px-6 py-3">
      <div className="flex items-center gap-6">
        <Link className="font-semibold" href="/">
          Agenty
        </Link>
        <Link className="text-sm hover:underline" href="/workspaces">
          Workspaces
          {invitations.length > 0 ? (
            <span className="ml-1 rounded-full bg-primary px-1.5 text-primary-foreground text-xs">
              {invitations.length}
              <span className="sr-only"> pending invitations</span>
            </span>
          ) : null}
        </Link>
      </div>
      <UserMenu email={user.email} name={user.name} />
    </header>
  );
}
```

- [ ] **Step 12: Update the existing E2E expectations** — in `tests/e2e/auth.spec.ts`, the signed-in home is now `/w/<id>`. Replace both `await expect(page).toHaveURL(/\/$/);` lines (in "signing in through the identity provider…" and "a signed-in user who opens the sign-in page…") with:

```ts
  await expect(page).toHaveURL(/\/w\/[0-9a-f-]{36}$/);
```

and rename the second test to `"a signed-in user who opens the sign-in page lands in their workspace"`.

- [ ] **Step 13: New E2E spec** — `tests/e2e/workspaces.spec.ts`:

```ts
import { type Browser, type BrowserContext, expect, type Page, test } from "@playwright/test";
import { loginAtMockIdp, uniqueIdpUser } from "./support/mock-idp";

type IdpUser = ReturnType<typeof uniqueIdpUser>;

const contexts: BrowserContext[] = [];
test.afterEach(async () => {
  await Promise.all(contexts.splice(0).map((context) => context.close()));
});

/** A fresh browser session signed in as `user`, on their personal workspace home. */
async function signedIn(browser: Browser, user: IdpUser): Promise<Page> {
  const context = await browser.newContext();
  contexts.push(context);
  const page = await context.newPage();
  await page.goto("/sign-in");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await loginAtMockIdp(page, user);
  await expect(page).toHaveURL(/\/w\/[0-9a-f-]{36}$/);
  await expect(page.getByText("Agents arrive in the next milestone")).toBeVisible();
  return page;
}

const alertWith = (page: Page, text: string) => page.getByRole("alert").filter({ hasText: text });
const settingsLink = (page: Page) =>
  page.getByRole("navigation", { name: "Workspace" }).getByRole("link", { name: "Settings", exact: true });

test("the first sign-in lands in a personal workspace that can only be renamed", async ({ browser }) => {
  const page = await signedIn(browser, uniqueIdpUser("wspers", "Pat Personal"));
  await expect(page.getByRole("heading", { level: 1, name: "Personal", exact: true })).toBeVisible();

  await settingsLink(page).click();
  await expect(page.getByText("This is your personal workspace.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Delete workspace", exact: true })).toHaveCount(0);
  await expect(page.getByLabel("Search users", { exact: true })).toHaveCount(0);

  await page.getByLabel("Workspace name", { exact: true }).fill("My corner");
  await page.getByRole("button", { name: "Rename", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "My corner", exact: true })).toBeVisible();
});

test("admins share a workspace with an invited user", async ({ browser }) => {
  const ada = uniqueIdpUser("wsada", "Ada Admin");
  const bob = uniqueIdpUser("wsbob", "Bob Member");
  const bobPage = await signedIn(browser, bob); // Bob needs an account before he can be invited.
  const adaPage = await signedIn(browser, ada);
  const team = `Team ${ada.sub}`;

  // Ada creates a workspace and invites Bob.
  await adaPage.goto("/workspaces");
  await adaPage.getByLabel("Name", { exact: true }).fill(team);
  await adaPage.getByRole("button", { name: "Create", exact: true }).click();
  await expect(adaPage.getByRole("heading", { level: 1, name: team, exact: true })).toBeVisible();
  const teamUrl = adaPage.url();
  await settingsLink(adaPage).click();
  await adaPage.getByLabel("Search users", { exact: true }).fill(bob.email);
  await adaPage.getByRole("button", { name: "Search", exact: true }).click();
  const results = adaPage.getByRole("list", { name: "Search results" });
  await results.getByRole("listitem").filter({ hasText: bob.email }).getByRole("button", { name: "Invite" }).click();
  await expect(
    adaPage.getByRole("list", { name: "Pending invitations" }).getByText(bob.email),
  ).toBeVisible();

  // Bob accepts and sees the workspace as a member.
  await bobPage.goto("/workspaces");
  const invitation = bobPage.getByRole("listitem").filter({ hasText: team });
  await expect(invitation).toContainText("invited by Ada Admin");
  await invitation.getByRole("button", { name: "Accept", exact: true }).click();
  await expect(bobPage.getByRole("heading", { level: 1, name: team, exact: true })).toBeVisible();
  await expect(bobPage.getByText("You are a member of this workspace.")).toBeVisible();
  await settingsLink(bobPage).click();
  await expect(bobPage.getByRole("button", { name: "Leave workspace", exact: true })).toBeVisible();
  await expect(bobPage.getByLabel("Workspace name", { exact: true })).toHaveCount(0);
  await expect(bobPage.getByLabel("Search users", { exact: true })).toHaveCount(0);

  // Ada makes Bob an admin and leaves.
  await adaPage.goto(`${teamUrl}/settings`);
  const bobRow = adaPage.getByRole("listitem").filter({ hasText: bob.email });
  await bobRow.getByLabel("Role of Bob Member", { exact: true }).selectOption("admin");
  await bobRow.getByRole("button", { name: "Save role", exact: true }).click();
  // Bob's own view proves the change was saved (the select alone keeps what the test chose).
  await bobPage.reload();
  await expect(bobPage.getByLabel("Search users", { exact: true })).toBeVisible();
  await adaPage.getByRole("button", { name: "Leave workspace", exact: true }).click();
  await expect(adaPage).toHaveURL(/\/workspaces$/);
  await expect(adaPage.getByRole("link", { name: team, exact: true })).toHaveCount(0);

  // Ada no longer gets in.
  await adaPage.goto(teamUrl);
  await expect(adaPage.getByText("This page could not be found.")).toBeVisible();

  // Bob, now the only admin, can't leave; deleting needs the exact name.
  await bobPage.goto(`${teamUrl}/settings`);
  await bobPage.getByRole("button", { name: "Leave workspace", exact: true }).click();
  await expect(alertWith(bobPage, "A workspace needs at least one admin.")).toBeVisible();
  await bobPage.getByLabel("Type the workspace name to confirm", { exact: true }).fill("wrong");
  await bobPage.getByRole("button", { name: "Delete workspace", exact: true }).click();
  await expect(alertWith(bobPage, "Type the workspace name exactly to delete it.")).toBeVisible();
  await bobPage.getByLabel("Type the workspace name to confirm", { exact: true }).fill(team);
  await bobPage.getByRole("button", { name: "Delete workspace", exact: true }).click();
  await expect(bobPage).toHaveURL(/\/workspaces$/);
  await expect(bobPage.getByRole("link", { name: team, exact: true })).toHaveCount(0);
});

test("non-members see the not-found page", async ({ browser }) => {
  const owner = await signedIn(browser, uniqueIdpUser("wsown", "Olive Owner"));
  const stranger = await signedIn(browser, uniqueIdpUser("wsstr", "Sam Stranger"));

  const settings = `${owner.url()}/settings`;
  await owner.goto(settings);
  await expect(owner.getByLabel("Workspace name", { exact: true })).toBeVisible();
  await stranger.goto(settings);
  await expect(stranger.getByText("This page could not be found.")).toBeVisible();
  await expect(stranger.getByLabel("Workspace name", { exact: true })).toHaveCount(0);
});
```

- [ ] **Step 14: Run unit, build and E2E**

Run: `task test` then `task build` then `task test:e2e`
Expected: all pass. Typical failures: a Cache Components build error "uncached data accessed outside of `<Suspense>`" means a component reads `params`, `searchParams` or the session outside a boundary: move the read into the Suspense child.

- [ ] **Step 15: Full check and commit**

Run: `task format`, then `task ci` → all green.

```bash
git add src/components/ui/input.tsx src/components/ui/label.tsx src/app/workspaces src/app/w src/app/page.tsx src/components/app-header.tsx tests/e2e/auth.spec.ts tests/e2e/workspaces.spec.ts
git commit -m "feat(workspaces): workspace pages, settings and invitations UI

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Drop TanStack Query, update docs

**Files:**
- Delete: `src/app/providers.tsx`
- Modify: `src/app/layout.tsx`, `package.json`, `pnpm-lock.yaml` (via pnpm)
- Modify: `AGENTS.md`, `README.md`

**Interfaces:**
- Consumes: the final names from Tasks 1–5 (for the docs).

- [ ] **Step 1: Remove TanStack Query** (maintainer decision: server components and server actions instead)

Run: `pnpm remove @tanstack/react-query` and delete `src/app/providers.tsx`. In `src/app/layout.tsx`, remove `import { Providers } from "./providers";` and the `<Providers>` wrapper, keeping its children:

```tsx
      <body className="flex min-h-full flex-col">
        <AppHeader />
        <main className="mx-auto flex w-full max-w-4xl flex-1 flex-col gap-6 p-8">{children}</main>
      </body>
```

Run `grep -rn "react-query\|Providers" src tests` → no matches.

- [ ] **Step 2: AGENTS.md**

Make these edits (keep the existing style: short sentences, bullets):

1. Intro paragraph: "configure agents (model, system prompt, tools) and chat with them" becomes "collaborate in workspaces, which will own agents, tools, provider keys and policies, and chat with agents (chats stay private to their user)".
2. Architecture tree: under `src/app/`, add
   - `workspaces/        workspace list, create form, invitations; shared action runner and error messages`
   - `w/[workspaceId]/   workspace home and settings (every page checks membership itself)`
   and under `src/server/`, add
   - `workspaces/        workspace rules (workspaces.ts, invitations.ts), access helpers (access.ts)`
   Under `tests/support/`, mention `users.ts` (creates users directly for workspace tests).
3. New subsection `### Workspaces` after `### Auth`:

```markdown
### Workspaces

Own tables (`workspace`, `workspace_member`, `workspace_invitation`), not Better Auth's
organization plugin. Rules (spec: `docs/specs/2026-10-10-m2-workspaces-design.md`):

- Every user has one personal workspace (`personal_user_id`), ensured at every sign-in in
  `databaseHooks.session.create.before` and by `/`. It can be renamed, not deleted or shared.
- Roles `admin` and `member`. Admins rename, delete, invite, cancel invitations, remove members
  and change roles (later: manage agents, tools, keys). Members use the workspace and can leave.
- At least one admin at all times (the last admin can't leave, be removed or be demoted).
- Invitations only for existing users (admins search all users by name/email); the invitee accepts
  or declines. No email.
- Users are never deleted for now (Better Auth's `deleteUser` is off).

Code: service functions in `src/server/workspaces/` take the acting user's id explicitly and throw
`WorkspaceError` with a fixed code. Every mutating function runs in one transaction that locks the
workspace row first, then reads the actor's role. Pages use `requireWorkspaceMember` /
`requireWorkspaceAdmin` (`access.ts`); non-members get the not-found page (a soft 404, since pages
stream). Server actions go through `runWorkspaceAction` (`src/app/workspaces/run-action.ts`):
errors redirect back with `?error=<code>`, shown as fixed text (`error-messages.ts`).
```

4. Non-negotiable rule 1 becomes:

```markdown
1. **Single-tenant for now (maintainer decision):** no `tenant_id`/RLS. Workspace isolation is
   enforced in app code: every workspace-owned table has `workspace_id` and is only read or written
   after `requireWorkspaceMember`/`requireWorkspaceAdmin` or a service check against the session
   user. Chats are private to their user: they also need an owner check. Ids in URLs and forms are
   lookup keys only. Adding tenancy later means `tenant_id` + RLS on every domain table.
```

5. Rule 4: "provider keys and tool credentials are stored AES-256-GCM encrypted" stays; add "(per workspace)" after "provider keys".
6. Conventions: replace any mention of TanStack Query (if present) and add: "UI: server components and server actions with plain `<form>`s; no client data-fetching library."
7. Auth section, "Use in code" bullet: add "Workspace pages use `src/server/workspaces/access.ts` on top of these." Add an Auth bullet: "**Personal workspace hook:** `databaseHooks.session.create.before` ensures the personal workspace at every sign-in, before the session row exists; a failure is rethrown as `APIError` code `sign_in_failed`, so the callback redirects to `/sign-in?error=sign_in_failed` without a session (`createAuth`'s `ensureWorkspace` option replaces it in tests)."

- [ ] **Step 3: README.md**

After the `### Sign-in (OIDC)` section, add:

```markdown
### Workspaces

Every user gets a personal workspace at their first sign-in. Under **Workspaces** they can create
more workspaces and invite people who have signed in before; invited users accept or decline in the
app (no email). Admins manage a workspace, members use it. Agents, tools and provider keys will
belong to workspaces; chats stay private.
```

- [ ] **Step 4: Check for stale statements**

Run: `grep -n -i "tanstack\|react-query\|organization plugin\|no organizations" AGENTS.md README.md`
Expected: only the deliberate "not Better Auth's organization plugin" line. In the Auth section, "no organizations, roles, email/password or rate limiting" becomes "no organization plugin, email/password or rate limiting (workspaces and their roles are our own, see Workspaces)".

- [ ] **Step 5: Full check and commit**

Run: `task format`, then `task ci` → all green.

```bash
git add src/app/layout.tsx package.json pnpm-lock.yaml AGENTS.md README.md
git rm src/app/providers.tsx
git commit -m "docs: workspaces in AGENTS.md and README; drop TanStack Query

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
