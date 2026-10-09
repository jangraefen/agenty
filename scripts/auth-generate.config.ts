// Only read by `task auth:generate` (Better Auth CLI): produces the reference schema in build/ that
// src/server/db/auth-schema.ts is diffed against after upgrades. Nothing connects to a database.
import { sso } from "@better-auth/sso";
import { betterAuth } from "better-auth";
import { drizzleAdapter } from "better-auth/adapters/drizzle";
import { drizzle } from "drizzle-orm/postgres-js";
import postgres from "postgres";

export const auth = betterAuth({
  secret: "reference-schema-only-0123456789abcdef",
  database: drizzleAdapter(drizzle(postgres("postgres://x@localhost/x")), {
    provider: "pg",
    schemaName: "auth",
    transaction: true,
  }),
  advanced: { database: { generateId: "uuid" } },
  plugins: [sso()],
});
