import "server-only";
import { drizzle } from "drizzle-orm/postgres-js";
import postgres from "postgres";
import { getEnv } from "@/server/env";
import * as schema from "./schema";

function createDb() {
  const client = postgres(getEnv().DATABASE_URL, { max: 10, onnotice: () => {} });
  return drizzle({ client, schema });
}

let db: ReturnType<typeof createDb> | undefined;

/** The shared connection pool, as role agenty_app. Created on first use. */
export function getDb() {
  db ??= createDb();
  return db;
}
