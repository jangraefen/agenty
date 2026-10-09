import "server-only";
import { computeDiscoveryUrl, DiscoveryError, discoverOIDCConfig, sso } from "@better-auth/sso";
import { type BetterAuthPlugin, betterAuth } from "better-auth";
import { drizzleAdapter } from "better-auth/adapters/drizzle";
import { APIError, createAuthMiddleware } from "better-auth/api";
import { and, eq } from "drizzle-orm";
import { z } from "zod";
import {
  account,
  member,
  organization,
  session,
  ssoProvider,
  user,
  verification,
} from "@/server/db/auth-schema";
import { type Db, getDb } from "@/server/db/client";
import { getEnv } from "@/server/env";
import {
  emailDomain,
  idpHostResolvesPublic,
  isAllowedIdpUrl,
  parseProvider,
  selectProvider,
} from "./providers";

/** The only Better Auth endpoints reachable at all; every other path answers 404. */
export const ALLOWED_ENDPOINTS: ReadonlySet<string> = new Set([
  "/sign-in/sso",
  "/sso/callback/:providerId",
  "/get-session",
  "/sign-out",
]);

// No `//host` or `/\host`: both would leave the site.
const relativePath = z.string().regex(/^\/(?![/\\])/);

/** The whole client body of /sign-in/sso. The provider is chosen here, never by the client. */
const signInBodySchema = z.strictObject({
  email: z.email(),
  callbackURL: relativePath,
  errorCallbackURL: relativePath.optional(),
});

/** Declares our tables so Better Auth's adapter can read and write them. */
const organizationsPlugin = {
  id: "agenty-organizations",
  schema: {
    organization: {
      fields: {
        slug: { type: "string", required: true, unique: true },
        providerId: { type: "string", required: false },
        createdAt: { type: "date", required: true },
        updatedAt: { type: "date", required: true },
      },
    },
    member: {
      fields: {
        userId: { type: "string", required: true, unique: true },
        organizationId: { type: "string", required: true },
        role: { type: "string", required: true },
        createdAt: { type: "date", required: true },
        updatedAt: { type: "date", required: true },
      },
    },
  },
} satisfies BetterAuthPlugin;

const idpUnavailable = () => new APIError("SERVICE_UNAVAILABLE", { code: "idp_unavailable" });

/** The error's class or code only: an IdP's error text is untrusted and may echo anything. */
function errorKind(error: unknown): string {
  if (error instanceof DiscoveryError) return error.code;
  return error instanceof Error ? error.name : typeof error;
}

export function createAuth(
  deps: { db?: Db; baseURL?: string; secret?: string; trustedOrigins?: string[] } = {},
) {
  const db = deps.db ?? getDb();
  const baseURL = deps.baseURL ?? getEnv().BETTER_AUTH_URL;
  const secret = deps.secret ?? getEnv().BETTER_AUTH_SECRET;
  const trustedOrigins = deps.trustedOrigins ?? getEnv().BETTER_AUTH_TRUSTED_ORIGINS;

  return betterAuth({
    database: drizzleAdapter(db, {
      provider: "pg",
      schema: { user, session, account, verification, ssoProvider, organization, member },
      transaction: true,
    }),
    secret,
    baseURL,
    basePath: "/api/auth",
    trustedOrigins: [baseURL, ...trustedOrigins],
    advanced: {
      database: { generateId: "uuid" },
      useSecureCookies: baseURL.startsWith("https://"),
    },
    session: {
      expiresIn: 43_200,
      disableSessionRefresh: true,
      cookieCache: { enabled: false },
    },
    account: {
      encryptOAuthTokens: true,
      accountLinking: { disableImplicitLinking: true },
    },
    rateLimit: { enabled: false },
    onAPIError: { errorURL: "/sign-in" },
    plugins: [
      sso({
        providersLimit: 0,
        schema: {
          ssoProvider: {
            additionalFields: {
              organizationClaim: { type: "string", required: true },
              roleClaim: { type: "string", required: false },
              adminValues: { type: "string", required: false },
            },
          },
        },
        // Placeholder until organizations are provisioned at sign-in: nobody gets in yet.
        resolveUser: () => ({ action: "reject", code: "not_ready" }),
      }),
      organizationsPlugin,
    ],
    hooks: {
      before: createAuthMiddleware(async (ctx) => {
        if (!ALLOWED_ENDPOINTS.has(ctx.path)) throw new APIError("NOT_FOUND");

        if (ctx.path === "/sso/callback/:providerId") {
          const providerId = ctx.params?.providerId;
          const row =
            typeof providerId === "string"
              ? await ctx.context.adapter.findOne({
                  model: "ssoProvider",
                  where: [{ field: "providerId", value: providerId }],
                })
              : null;
          if (!row) throw new APIError("NOT_FOUND");
          return;
        }

        if (ctx.path !== "/sign-in/sso") return;

        const body = signInBodySchema.safeParse(ctx.body);
        if (!body.success || !emailDomain(body.data.email)) {
          throw new APIError("BAD_REQUEST", { code: "invalid_request" });
        }

        // All rows, ordered: the adapter's findMany stops at 100 rows in no defined order.
        const rows = await db.select().from(ssoProvider).orderBy(ssoProvider.providerId);
        const row = selectProvider(rows, body.data.email);
        if (!row) throw new APIError("BAD_REQUEST", { code: "unknown_domain" });

        const parsed = parseProvider(row);
        if (!parsed.ok) {
          console.error(
            `Identity provider "${row.providerId}" is misconfigured: ${parsed.fields.join(", ")}`,
          );
          throw idpUnavailable();
        }
        const { provider } = parsed;

        if (!provider.hasEndpoints) {
          // The plugin checks DNS only for URLs its isTrustedOrigin rejects, and ours accepts
          // every public name: resolve the discovery host here, unless its origin is trusted.
          const discoveryUrl =
            provider.oidc.discoveryEndpoint ?? computeDiscoveryUrl(provider.issuer);
          const trusted = (u: string) => ctx.context.isTrustedOrigin(u);
          if (!(await idpHostResolvesPublic(discoveryUrl, trusted))) {
            console.error(
              `Identity provider "${provider.providerId}" discovery refused: private_host`,
            );
            throw idpUnavailable();
          }
          try {
            const discovered = await discoverOIDCConfig({
              issuer: provider.issuer,
              discoveryEndpoint: provider.oidc.discoveryEndpoint,
              isTrustedOrigin: (url) => isAllowedIdpUrl(url, trusted),
            });
            const written = JSON.stringify({
              ...provider.oidc,
              authorizationEndpoint: discovered.authorizationEndpoint,
              tokenEndpoint: discovered.tokenEndpoint,
              jwksEndpoint: discovered.jwksEndpoint,
            });
            // Only if the row is unchanged, so a concurrent first sign-in cannot clobber it.
            await db
              .update(ssoProvider)
              .set({ oidcConfig: written })
              .where(
                and(
                  eq(ssoProvider.providerId, provider.providerId),
                  eq(ssoProvider.oidcConfig, row.oidcConfig ?? ""),
                ),
              );
          } catch (error) {
            console.error(
              `Identity provider "${provider.providerId}" discovery failed: ${errorKind(error)}`,
            );
            throw idpUnavailable();
          }
        }

        return { context: { body: { ...body.data, providerId: provider.providerId } } };
      }),
    },
  });
}

export type Auth = ReturnType<typeof createAuth>;

let auth: Auth | undefined;

/** The shared instance, configured from the environment. Created on first use. */
export function getAuth(): Auth {
  auth ??= createAuth();
  return auth;
}
