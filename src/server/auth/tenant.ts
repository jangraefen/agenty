import "server-only";
import { eq } from "drizzle-orm";
import { headers } from "next/headers";
import { member, organization, ssoProvider } from "@/server/db/auth-schema";
import { type Db, getDb } from "@/server/db/client";
import { type Auth, getAuth } from "./auth";
import { createTenantContext, type TenantContext } from "./tenant-context";

export type { TenantContext } from "./tenant-context";

export class UnauthorizedError extends Error {}
export class ForbiddenError extends Error {}

type Deps = { headers?: Headers; auth?: Auth; db?: Db };

/** The signed-in user's tenant context and display data, from one session lookup. */
export async function getSignedIn(deps: Deps = {}) {
  const auth = deps.auth ?? getAuth();
  const session = await auth.api.getSession({ headers: deps.headers ?? (await headers()) });
  if (!session) throw new UnauthorizedError("Not signed in");
  const db = deps.db ?? getDb();
  const [row] = await db
    .select({
      organizationId: member.organizationId,
      role: member.role,
      providerId: ssoProvider.providerId,
    })
    .from(member)
    .innerJoin(organization, eq(organization.id, member.organizationId))
    .leftJoin(ssoProvider, eq(ssoProvider.providerId, organization.providerId))
    .where(eq(member.userId, session.user.id));
  if (!row) throw new ForbiddenError("No organization membership");
  if (!row.providerId) throw new ForbiddenError("The organization has no identity provider");
  const ctx = createTenantContext({
    userId: session.user.id,
    organizationId: row.organizationId,
    role: row.role,
  });
  return { ctx, user: { name: session.user.name, email: session.user.email } };
}

export async function getTenantContext(deps: Deps = {}): Promise<TenantContext> {
  return (await getSignedIn(deps)).ctx;
}
