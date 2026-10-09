import { randomUUID } from "node:crypto";
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

describe("RLS catalog guard", () => {
  it("every table in schema app is tenant-scoped by RLS", async () => {
    const violations = (await findRlsViolations(appSql, "app")).filter(
      (v) => !((v.split(":")[0] ?? "") in EXCEPTIONS),
    );
    expect(violations).toEqual([]);
  });

  it("the guard reports missing tenant_id, disabled RLS and an open policy", async () => {
    const schema = `rls_guard_${randomUUID().replaceAll("-", "").slice(0, 12)}`;
    try {
      await ownerSql.unsafe(`create schema ${schema}`);
      await ownerSql.unsafe(`create table ${schema}.plain (id int)`);
      await ownerSql.unsafe(`create table ${schema}."open" (tenant_id uuid not null)`);
      await ownerSql.unsafe(`alter table ${schema}."open" enable row level security`);
      await ownerSql.unsafe(
        `create policy open_all on ${schema}."open" as permissive for all to agenty_app using (true)`,
      );
      expect(await findRlsViolations(ownerSql, schema)).toEqual([
        "open: no tenant_isolation policy for agenty_app",
        "open: policy open_all not keyed on app.tenant_id",
        "plain: no tenant_id uuid not null column",
        "plain: no tenant_isolation policy for agenty_app",
        "plain: row-level security disabled",
      ]);
    } finally {
      await ownerSql.unsafe(`drop schema if exists ${schema} cascade`);
    }
  });
});
