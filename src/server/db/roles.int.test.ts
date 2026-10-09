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

  it("is a member of no role (owner membership would bypass RLS)", async () => {
    const rows = await appSql`
      select count(*)::int as n from pg_auth_members where member = 'agenty_app'::regrole`;
    expect(rows).toEqual([{ n: 0 }]);
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

describe.each(["app", "auth"])("schema %s ownership and grants", (schema) => {
  it("the schema is owned by agenty_owner", async () => {
    const [row] = await appSql`
      select nspowner::regrole::text as owner from pg_namespace where nspname = ${schema}`;
    expect(row?.owner).toBe("agenty_owner");
  });

  it("every table is owned by agenty_owner", async () => {
    const foreign = await appSql`
      select tablename, tableowner from pg_tables
      where schemaname = ${schema} and tableowner <> 'agenty_owner'`;
    expect(foreign).toEqual([]);
  });

  it("agenty_app can use the schema but not create objects in it", async () => {
    const [row] = await appSql`
      select
        has_schema_privilege(current_user, ${schema}, 'USAGE') as can_use,
        has_schema_privilege(current_user, ${schema}, 'CREATE') as can_create`;
    expect(row).toEqual({ can_use: true, can_create: false });
  });

  it("tables agenty_owner creates are readable and writable by agenty_app", async () => {
    const privileges = ownerSql.begin(async (tx) => {
      await tx.unsafe(`create table ${schema}.__grant_probe (id integer)`);
      const [row] = await tx`
        select
          has_table_privilege('agenty_app', ${`${schema}.__grant_probe`}, 'SELECT') as can_select,
          has_table_privilege('agenty_app', ${`${schema}.__grant_probe`}, 'INSERT') as can_insert,
          has_table_privilege('agenty_app', ${`${schema}.__grant_probe`}, 'UPDATE') as can_update,
          has_table_privilege('agenty_app', ${`${schema}.__grant_probe`}, 'DELETE') as can_delete,
          has_table_privilege('agenty_app', ${`${schema}.__grant_probe`}, 'TRUNCATE') as can_truncate`;
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

  it("sequences agenty_owner creates are usable by agenty_app", async () => {
    const privileges = ownerSql.begin(async (tx) => {
      await tx.unsafe(`create sequence ${schema}.__seq_probe`);
      const [row] = await tx`
        select
          has_sequence_privilege('agenty_app', ${`${schema}.__seq_probe`}, 'USAGE') as can_use,
          has_sequence_privilege('agenty_app', ${`${schema}.__seq_probe`}, 'SELECT') as can_select`;
      throw new Rollback(JSON.stringify(row));
    });
    const error = await privileges.catch((e: unknown) => e);
    expect(error).toBeInstanceOf(Rollback);
    expect(JSON.parse((error as Rollback).message)).toEqual({ can_use: true, can_select: true });
  });
});
