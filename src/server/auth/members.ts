import "server-only";
import { asc, eq } from "drizzle-orm";
import { member, organization, user } from "@/server/db/auth-schema";
import { type Db, getDb } from "@/server/db/client";
import type { Role } from "./providers";
import type { TenantContext } from "./tenant-context";

export async function listMembers(
  ctx: TenantContext,
  db: Db = getDb(),
): Promise<{ name: string; email: string; role: Role }[]> {
  return db
    .select({ name: user.name, email: user.email, role: member.role })
    .from(member)
    .innerJoin(user, eq(user.id, member.userId))
    .where(eq(member.organizationId, ctx.organizationId))
    .orderBy(asc(user.name));
}

export async function getOrganizationSlug(ctx: TenantContext, db: Db = getDb()): Promise<string> {
  const [row] = await db
    .select({ slug: organization.slug })
    .from(organization)
    .where(eq(organization.id, ctx.organizationId));
  if (!row) throw new Error("Organization not found");
  return row.slug;
}
