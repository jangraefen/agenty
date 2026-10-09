import "server-only";
import { betterAuth } from "better-auth";
import { drizzleAdapter } from "better-auth/adapters/drizzle";
import { APIError, createAuthMiddleware } from "better-auth/api";
import { genericOAuth } from "better-auth/plugins";
import { account, session, user, verification } from "@/server/db/auth-schema";
import { getDb } from "@/server/db/client";
import { getEnv } from "@/server/env";

export type AuthConfig = {
  baseURL: string;
  secret: string;
  oidc: { discoveryUrl: string; clientId: string; clientSecret: string };
};

/** The one OIDC connector; also the path segment of its callback (/api/auth/callback/oidc). */
export const OIDC_PROVIDER_ID = "oidc";

export function createAuth(config: AuthConfig) {
  return betterAuth({
    database: drizzleAdapter(getDb(), {
      provider: "pg",
      schema: { user, session, account, verification },
      transaction: true,
    }),
    secret: config.secret,
    baseURL: config.baseURL,
    basePath: "/api/auth",
    advanced: { database: { generateId: "uuid" } },
    rateLimit: { enabled: false },
    session: { expiresIn: 12 * 60 * 60, disableSessionRefresh: true },
    account: { encryptOAuthTokens: true },
    onAPIError: { errorURL: "/sign-in" },
    plugins: [
      genericOAuth({
        config: [
          {
            providerId: OIDC_PROVIDER_ID,
            discoveryUrl: config.oidc.discoveryUrl,
            clientId: config.oidc.clientId,
            clientSecret: config.oidc.clientSecret,
            scopes: ["openid", "email", "profile"],
            requireIdTokenVerification: true,
            // Sign-out is local only: it ends the app session, not the session at the IdP.
            disableProviderLogout: true,
          },
        ],
      }),
    ],
    hooks: {
      before: createAuthMiddleware(async (ctx) => {
        // Both endpoints would accept a bare ID token with a nonce the client chooses as a sign-in,
        // so a leaked or replayed ID token could start a session. Only the code flow may sign in.
        if (
          (ctx.path === "/sign-in/social" || ctx.path === "/link-social") &&
          ctx.body?.idToken !== undefined
        ) {
          throw new APIError("BAD_REQUEST", { code: "id_token_sign_in_disabled" });
        }
      }),
    },
  });
}

export type Auth = ReturnType<typeof createAuth>;

/** Whether discovery succeeded: Better Auth skips a provider whose discovery failed. */
export async function hasOidcProvider(auth: Auth): Promise<boolean> {
  return (await auth.$context).socialProviders.some((p) => p.id === OIDC_PROVIDER_ID);
}

/**
 * Memoizes `create()`. Concurrent callers share the pending promise. A value that is not
 * `isUsable` is still returned, but recreated by the first call after `retryAfterMs`; a rejection
 * is recreated by the next call.
 */
export function memoizeUntil<T>(
  create: () => Promise<T>,
  isUsable: (value: T) => Promise<boolean>,
  retryAfterMs: number,
  now: () => number = Date.now,
): () => Promise<T> {
  type Entry = { promise: Promise<T>; retryAt?: number };
  let cached: Entry | undefined;

  return () => {
    if (cached && (cached.retryAt === undefined || now() < cached.retryAt)) return cached.promise;

    // Created through then(): a synchronous throw in create() becomes a rejection like any other.
    const entry: Entry = {
      promise: Promise.resolve()
        .then(create)
        .then(async (value) => {
          if (!(await isUsable(value))) entry.retryAt = now() + retryAfterMs;
          return value;
        }),
    };
    entry.promise.catch(() => {
      if (cached === entry) cached = undefined;
    });
    cached = entry;
    return entry.promise;
  };
}

function configFromEnv(): AuthConfig {
  const env = getEnv();
  return {
    baseURL: env.BETTER_AUTH_URL,
    secret: env.BETTER_AUTH_SECRET,
    oidc: {
      discoveryUrl: env.OIDC_DISCOVERY_URL,
      clientId: env.OIDC_CLIENT_ID,
      clientSecret: env.OIDC_CLIENT_SECRET,
    },
  };
}

/**
 * The app's auth instance, created on first use. If the IdP's discovery failed (the provider is
 * missing and sign-in answers PROVIDER_NOT_FOUND), the instance is recreated after 30 s.
 */
export const getAuth: () => Promise<Auth> = memoizeUntil(
  () => Promise.resolve(createAuth(configFromEnv())),
  async (auth) => {
    const ok = await hasOidcProvider(auth);
    if (!ok) console.error("OIDC discovery failed; retrying in 30 s");
    return ok;
  },
  30 * 1000,
);
