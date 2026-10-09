import "server-only";
import { computeDiscoveryUrl, DiscoveryError, discoverOIDCConfig, sso } from "@better-auth/sso";
import { betterAuth, getCurrentAdapter } from "better-auth";
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
import { provisionMembership, resolveSignIn, takeDecision } from "./provisioning";
import { databaseOptions, organizationsPlugin, ssoSchema } from "./schema-options";

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
      database: databaseOptions,
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
    databaseHooks: {
      session: {
        create: {
          // Every session comes from an SSO sign-in whose resolveUser left a decision; anything
          // else fails closed. A throw here rolls back the user, account and session.
          before: async (session, ctx) => {
            const decision = takeDecision(ctx);
            if (!ctx || !decision) {
              throw new APIError("FORBIDDEN", { code: "provisioning_missing" });
            }
            const adapter = await getCurrentAdapter(ctx.context.adapter);
            await provisionMembership(adapter, session.userId, decision);
          },
        },
      },
    },
    onAPIError: { errorURL: "/sign-in" },
    plugins: [
      sso({
        providersLimit: 0,
        schema: ssoSchema,
        // Decides organization and role; session.create.before writes them in the same transaction.
        resolveUser: (input, { database }) => resolveSignIn(input, database),
      }),
      // After sso(): it overrides the plugin's sso_provider.user_id (see schema-options.ts).
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
