// Registers the mock IdP's development providers (`task db:seed`). Insert-if-absent, so it is safe
// to run repeatedly. Plain Node: run as `node scripts/seed.ts`.
import { drizzle } from "drizzle-orm/postgres-js";
import postgres from "postgres";
import { seedDevProviders } from "../src/server/db/seed.ts";

const url = process.env.DATABASE_URL;
if (!url) {
  console.error("DATABASE_URL is not set.");
  process.exit(1);
}

const client = postgres(url, { max: 1, onnotice: () => {} });
try {
  await seedDevProviders(drizzle({ client }));
  console.log("Seeded development identity providers (corp, partner).");
} catch (error) {
  console.error("Seeding failed:", error instanceof Error ? error.message : error);
  process.exitCode = 1;
} finally {
  await client.end();
}
