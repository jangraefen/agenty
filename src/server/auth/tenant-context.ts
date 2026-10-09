import "server-only";
import type { Role } from "./providers";

declare const brand: unique symbol;

/** Who is acting, in which organization. Only src/server/auth creates these (Biome rule). */
export type TenantContext = {
  readonly userId: string;
  readonly organizationId: string;
  readonly role: Role;
  readonly [brand]: true;
};

export function createTenantContext(values: {
  userId: string;
  organizationId: string;
  role: Role;
}): TenantContext {
  return Object.freeze({ ...values }) as TenantContext;
}
