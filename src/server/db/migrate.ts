// Applies the Drizzle migrations as the owner role. Used by src/instrumentation.ts when the server
// starts and by scripts/migrate.ts (`task db:migrate`, for tests and CI). No "server-only" import:
// plain Node runs this file through scripts/migrate.ts.
import { drizzle } from "drizzle-orm/postgres-js";
import { migrate } from "drizzle-orm/postgres-js/migrator";
import postgres from "postgres";

/** Arbitrary constant; serialises migration runs of several instances starting at once. */
const MIGRATION_LOCK_KEY = 724_251_309;

export async function migrateDatabase(url: string, migrationsFolder: string): Promise<void> {
  // One connection, so the advisory lock and the migrations share a session.
  const client = postgres(url, { max: 1, onnotice: () => {} });
  try {
    await client`select pg_advisory_lock(${MIGRATION_LOCK_KEY})`;
    await migrate(drizzle({ client }), { migrationsFolder });
  } finally {
    await client.end(); // ending the session releases the advisory lock
  }
}
