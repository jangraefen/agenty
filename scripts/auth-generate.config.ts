// Only read by `task auth:generate` (Better Auth CLI), which writes src/server/db/auth-schema.ts.
// Keep the schema-relevant options in sync with createAuth (src/server/auth/auth.ts): uuid ids.
// genericOAuth adds no tables, so no plugins are needed here.
import { betterAuth } from "better-auth";
import { drizzleAdapter } from "better-auth/adapters/drizzle";
import { drizzle } from "drizzle-orm/postgres-js";
import postgres from "postgres";

export const auth = betterAuth({
  secret: "schema-generation-only-0123456789abcdef",
  database: drizzleAdapter(drizzle(postgres("postgres://x@localhost/x")), {
    provider: "pg",
    schemaName: "app",
    transaction: true,
  }),
  advanced: { database: { generateId: "uuid" } },
});
