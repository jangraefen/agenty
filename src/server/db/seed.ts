// Development identity providers for the mock IdP. No "server-only" import and relative `.ts`
// imports only: scripts/seed.ts runs this in plain Node.
import type { PostgresJsDatabase } from "drizzle-orm/postgres-js";
import { ssoProvider } from "./auth-schema.ts";

export type DevProvider = { providerId: string; issuer: string; domain: string };

const oidcConfig = JSON.stringify({
  clientId: "agenty",
  clientSecret: "dev-secret",
  pkce: true,
  scopes: ["openid", "email", "profile"],
});

export const DEV_PROVIDERS: readonly DevProvider[] = [
  { providerId: "corp", issuer: "http://localhost:8080/corp", domain: "corp.test" },
  { providerId: "partner", issuer: "http://localhost:8080/partner", domain: "partner.test" },
];

/**
 * Registers the mock IdP's providers. Never modifies an existing row: Better Auth binds a
 * fingerprint of the provider row into each sign-in, so changing it would fail sign-ins in flight.
 */
export async function seedDevProviders<S extends Record<string, unknown>>(
  db: PostgresJsDatabase<S>,
  providers: readonly DevProvider[] = DEV_PROVIDERS,
): Promise<void> {
  await db
    .insert(ssoProvider)
    .values(
      providers.map((p) => ({
        ...p,
        oidcConfig,
        organizationClaim: "org",
        roleClaim: "groups",
        adminValues: "agenty-admins",
      })),
    )
    .onConflictDoNothing({ target: ssoProvider.providerId });
}
