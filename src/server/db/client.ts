import "server-only";
import { drizzle } from "drizzle-orm/postgres-js";
import postgres from "postgres";
import { getEnv } from "@/server/env";
import * as authSchema from "./auth-schema";
import * as appSchema from "./schema";

const schema = { ...appSchema, ...authSchema };

/** A Drizzle instance on its own connection pool (role taken from the URL). */
export function createDb(url: string, options: { max?: number } = {}) {
  const client = postgres(url, { max: options.max ?? 10, onnotice: () => {} });
  return drizzle({ client, schema });
}

export type Db = ReturnType<typeof createDb>;

let db: Db | undefined;

/** The shared connection pool, as role agenty_app. Created on first use. */
export function getDb(): Db {
  db ??= createDb(getEnv().DATABASE_URL);
  return db;
}
