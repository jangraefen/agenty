// No "server-only" import and relative `.ts` imports: drizzle-kit and plain Node load this file.
import { type SQL, sql } from "drizzle-orm";
import { type AnyPgColumn, pgPolicy, pgRole, uuid } from "drizzle-orm/pg-core";
import { organization } from "./auth-schema.ts";

/** The runtime role; declared as existing so drizzle-kit never creates it. */
export const appRole = pgRole("agenty_app").existing();

/** The current transaction's tenant, or NULL when none is set (then no row matches). */
export const tenantIdSetting: SQL = sql`nullif(current_setting('app.tenant_id', true), '')::uuid`;

/** Every domain table's tenant column. */
export function tenantId() {
  return uuid("tenant_id")
    .notNull()
    .references(() => organization.id, { onDelete: "cascade" });
}

/**
 * Every domain table's only policy: rows of the current tenant, for reads and writes.
 * Usage: app.table("agents", { id: …, tenantId: tenantId() }, (t) => [tenantIsolation(t)])
 */
export function tenantIsolation(table: { tenantId: AnyPgColumn }) {
  return pgPolicy("tenant_isolation", {
    as: "permissive",
    for: "all",
    to: appRole,
    using: sql`${table.tenantId} = ${tenantIdSetting}`,
    withCheck: sql`${table.tenantId} = ${tenantIdSetting}`,
  });
}
