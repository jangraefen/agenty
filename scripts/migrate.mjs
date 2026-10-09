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
