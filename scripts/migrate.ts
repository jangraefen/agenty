// Applies migrations as the owner role (`task db:migrate`). The app migrates itself on start; this
// exists so tests and CI can prepare the database without starting the server.
import { fileURLToPath } from "node:url";
import { migrateDatabase } from "../src/server/db/migrate.ts";

const url = process.env.DATABASE_MIGRATION_URL;
if (!url) {
  console.error("DATABASE_MIGRATION_URL is not set.");
  process.exit(1);
}

try {
  await migrateDatabase(
    url,
    fileURLToPath(new URL("../src/server/db/migrations", import.meta.url)),
  );
  console.log("Migrations applied.");
} catch (error) {
  console.error("Migration failed:", error instanceof Error ? error.message : error);
  process.exitCode = 1;
}
