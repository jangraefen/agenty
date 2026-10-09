import "server-only";
import { sql } from "drizzle-orm";
import { z } from "zod";
import { getDb } from "@/server/db/client";

export const healthBodySchema = z.discriminatedUnion("status", [
  z.object({ status: z.literal("ok"), db: z.literal("ok") }),
  z.object({ status: z.literal("error"), db: z.literal("unavailable") }),
]);

export type HealthBody = z.infer<typeof healthBodySchema>;

async function pingDatabase(): Promise<unknown> {
  return getDb().execute(sql`select 1`);
}

/** Checks the database as the runtime role. Error details are logged, never returned. */
export async function checkHealth(
  ping: () => Promise<unknown> = pingDatabase,
): Promise<{ httpStatus: 200 | 503; body: HealthBody }> {
  try {
    await ping();
    return { httpStatus: 200, body: healthBodySchema.parse({ status: "ok", db: "ok" }) };
  } catch (error) {
    console.error("Health check: database unavailable", error);
    return {
      httpStatus: 503,
      body: healthBodySchema.parse({ status: "error", db: "unavailable" }),
    };
  }
}
