// Only read by `task auth:generate` (Better Auth CLI), which writes src/server/db/auth-schema.ts from
// it. The schema options come from the module createAuth uses too. Nothing connects to a database.
import { sso } from "@better-auth/sso";
import { betterAuth } from "better-auth";
import { drizzleAdapter } from "better-auth/adapters/drizzle";
import { drizzle } from "drizzle-orm/postgres-js";
import postgres from "postgres";
import {
  databaseOptions,
  organizationsPlugin,
  ssoSchema,
} from "../src/server/auth/schema-options.ts";

export const auth = betterAuth({
  secret: "schema-generation-only-0123456789abcdef",
  database: drizzleAdapter(drizzle(postgres("postgres://x@localhost/x")), {
    provider: "pg",
    schemaName: "auth",
    transaction: true,
  }),
  advanced: { database: databaseOptions },
  plugins: [sso({ schema: ssoSchema }), organizationsPlugin],
});
