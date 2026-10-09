import "server-only";
import { sql } from "drizzle-orm";
import type { TenantContext } from "@/server/auth/tenant-context";
import { type Db, getDb } from "./client";

export type Tx = Parameters<Parameters<Db["transaction"]>[0]>[0];

/**
 * Runs fn in a transaction scoped to ctx's organization: RLS on domain tables only shows and
 * accepts that organization's rows. The setting is transaction-local and vanishes at commit/rollback.
 */
export async function withTenant<T>(
  ctx: TenantContext,
  fn: (tx: Tx) => Promise<T>,
  db: Db = getDb(),
): Promise<T> {
  return db.transaction(async (tx) => {
    await tx.execute(sql`select set_config('app.tenant_id', ${ctx.organizationId}, true)`);
    return fn(tx);
  });
}
