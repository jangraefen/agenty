import { randomUUID } from "node:crypto";
import { eq } from "drizzle-orm";
import { getTableConfig, PgDialect, pgSchema, text, uuid } from "drizzle-orm/pg-core";
import postgres from "postgres";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { createTenantContext } from "@/server/auth/tenant-context";
import { createDb } from "./client";
import { withTenant } from "./tenant";
import { tenantId, tenantIdSetting, tenantIsolation } from "./tenant-table";

function requireEnv(name: string): string {
  const value = process.env[name];
  if (!value) throw new Error(`${name} must be set (run tests via \`task test\`)`);
  return value;
}

const suffix = randomUUID().replaceAll("-", "").slice(0, 12);
const fixtureSchemaName = `rls_fixture_${suffix}`;
const fixtureSchema = pgSchema(fixtureSchemaName);
const notes = fixtureSchema.table(
  "notes",
  {
    id: uuid("id").primaryKey().defaultRandom(),
    tenantId: tenantId(),
    body: text("body").notNull(),
  },
  (t) => [tenantIsolation(t)],
);

const ownerSql = postgres(requireEnv("DATABASE_MIGRATION_URL"), { max: 1, onnotice: () => {} });
const appDb = createDb(requireEnv("DATABASE_URL"), { max: 1 });

/** Drizzle wraps driver errors ("Failed query"); the Postgres message is on `cause`. */
async function rejectsWithPg(promise: PromiseLike<unknown>, pattern: RegExp) {
  const error = await Promise.resolve(promise).then(
    () => undefined,
    (e: unknown) => e,
  );
  expect(error).toBeInstanceOf(Error);
  expect(((error as Error).cause as Error | undefined)?.message).toMatch(pattern);
}

let orgA = "";
let orgB = "";
let ctxA: ReturnType<typeof createTenantContext>;

beforeAll(async () => {
  const orgs = await ownerSql<{ id: string }[]>`
    insert into auth.organization (slug, provider_id)
    values (${`rls-a-${suffix}`}, null), (${`rls-b-${suffix}`}, null)
    returning id, slug`;
  orgA = orgs.find((o) => (o as unknown as { slug: string }).slug.startsWith("rls-a"))?.id ?? "";
  orgB = orgs.find((o) => (o as unknown as { slug: string }).slug.startsWith("rls-b"))?.id ?? "";
  ctxA = createTenantContext({ userId: randomUUID(), organizationId: orgA, role: "member" });

  // The policy comes from the helper itself, so the behaviour tests exercise its exact expressions.
  const policy = getTableConfig(notes).policies[0];
  if (!policy?.using || !policy.withCheck) throw new Error("policy expression missing");
  const dialect = new PgDialect();
  const using = dialect.sqlToQuery(policy.using).sql;
  const withCheck = dialect.sqlToQuery(policy.withCheck).sql;
  const table = `${fixtureSchemaName}.notes`;
  await ownerSql.unsafe(`create schema ${fixtureSchemaName}`);
  await ownerSql.unsafe(`grant usage on schema ${fixtureSchemaName} to agenty_app`);
  await ownerSql.unsafe(
    `create table ${table} (id uuid primary key default gen_random_uuid(), tenant_id uuid not null references auth.organization(id) on delete cascade, body text not null)`,
  );
  await ownerSql.unsafe(`alter table ${table} enable row level security`);
  await ownerSql.unsafe(
    `create policy tenant_isolation on ${table} as permissive for all to agenty_app using (${using}) with check (${withCheck})`,
  );
  await ownerSql.unsafe(
    `grant select, insert, update, delete on all tables in schema ${fixtureSchemaName} to agenty_app`,
  );
  await ownerSql.unsafe(
    `insert into ${table} (tenant_id, body) values ('${orgA}', 'a1'), ('${orgA}', 'a2'), ('${orgB}', 'b1')`,
  );
});

afterAll(async () => {
  await ownerSql.unsafe(`drop schema if exists ${fixtureSchemaName} cascade`);
  if (orgA && orgB) await ownerSql`delete from auth.organization where id in (${orgA}, ${orgB})`;
  await ownerSql.end();
  await (appDb.$client as postgres.Sql).end();
});

describe("tenantIsolation helper", () => {
  it("defines one permissive policy for agenty_app keyed on app.tenant_id", () => {
    const policies = getTableConfig(notes).policies;
    expect(policies).toHaveLength(1);
    const policy = policies[0];
    expect(policy?.name).toBe("tenant_isolation");
    expect(policy?.as).toBe("permissive");
    expect(policy?.for).toBe("all");
    expect((policy?.to as { name: string } | undefined)?.name).toBe("agenty_app");
    const dialect = new PgDialect();
    expect(dialect.sqlToQuery(tenantIdSetting).sql).toBe(
      "nullif(current_setting('app.tenant_id', true), '')::uuid",
    );
    for (const expr of [policy?.using, policy?.withCheck]) {
      if (!expr) throw new Error("policy expression missing");
      expect(dialect.sqlToQuery(expr).sql).toBe(
        `"${fixtureSchemaName}"."notes"."tenant_id" = ${dialect.sqlToQuery(tenantIdSetting).sql}`,
      );
    }
  });
});

describe("tenant isolation by RLS", () => {
  it("shows only the current organization's rows", async () => {
    const rows = await withTenant(ctxA, (tx) => tx.select().from(notes), appDb);
    expect(rows.map((r) => r.tenantId)).toEqual([orgA, orgA]);
  });

  it("shows nothing and accepts no writes without a tenant", async () => {
    expect(await appDb.select().from(notes)).toEqual([]);
    await rejectsWithPg(
      appDb.insert(notes).values({ tenantId: orgA, body: "x" }),
      /row-level security/,
    );
  });

  it("rejects inserting a row for another organization", async () => {
    await rejectsWithPg(
      withTenant(ctxA, (tx) => tx.insert(notes).values({ tenantId: orgB, body: "x" }), appDb),
      /row-level security/,
    );
  });

  it("rejects moving a row to another organization and cannot touch foreign rows", async () => {
    await rejectsWithPg(
      withTenant(ctxA, (tx) => tx.update(notes).set({ tenantId: orgB }), appDb),
      /row-level security/,
    );
    const updated = await withTenant(
      ctxA,
      (tx) => tx.update(notes).set({ body: "y" }).where(eq(notes.tenantId, orgB)).returning(),
      appDb,
    );
    const deleted = await withTenant(
      ctxA,
      (tx) => tx.delete(notes).where(eq(notes.tenantId, orgB)).returning(),
      appDb,
    );
    expect([updated, deleted]).toEqual([[], []]);
  });

  it.each(["commit", "rollback"])(
    "does not leak the tenant to the next transaction after %s",
    async (end) => {
      const run = withTenant(
        ctxA,
        async (tx) => {
          await tx.select().from(notes);
          if (end === "rollback") throw new Error("rollback");
        },
        appDb,
      );
      if (end === "rollback") await expect(run).rejects.toThrow("rollback");
      else await run;
      expect(await appDb.select().from(notes)).toEqual([]); // same single pooled connection
    },
  );

  it("fails closed on a malformed tenant setting", async () => {
    const bad = createTenantContext({
      userId: ctxA.userId,
      organizationId: "not-a-uuid",
      role: "member",
    });
    await rejectsWithPg(
      withTenant(bad, (tx) => tx.select().from(notes), appDb),
      /uuid/,
    );
  });

  it("passes the organization id as a bind parameter", async () => {
    const injection = createTenantContext({
      userId: ctxA.userId,
      organizationId: "x', true); drop table notes; --",
      role: "member",
    });
    await rejectsWithPg(
      withTenant(injection, (tx) => tx.select().from(notes), appDb),
      /uuid/,
    );
    expect(await withTenant(ctxA, (tx) => tx.select().from(notes), appDb)).toHaveLength(2);
  });
});
