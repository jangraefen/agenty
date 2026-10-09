import path from "node:path";
import postgres from "postgres";
import { afterAll, describe, expect, it } from "vitest";
import { migrateDatabase } from "./migrate";

function requireEnv(name: string): string {
  const value = process.env[name];
  if (!value) throw new Error(`${name} must be set (run tests via \`task test\`)`);
  return value;
}

const migrationUrl = requireEnv("DATABASE_MIGRATION_URL");
const migrationsFolder = path.join(process.cwd(), "src/server/db/migrations");
const sql = postgres(migrationUrl, { max: 1, onnotice: () => {} });

afterAll(async () => {
  await sql.end();
});

describe("migrateDatabase", () => {
  it("is safe to run from several instances at once and releases its lock", async () => {
    await Promise.all([
      migrateDatabase(migrationUrl, migrationsFolder),
      migrateDatabase(migrationUrl, migrationsFolder),
      migrateDatabase(migrationUrl, migrationsFolder),
    ]);

    const locks = await sql`select count(*)::int as n from pg_locks where locktype = 'advisory'`;
    expect(locks).toEqual([{ n: 0 }]);
    const [schema] = await sql`select count(*)::int as n from pg_namespace where nspname = 'app'`;
    expect(schema).toEqual({ n: 1 });
  });
});
