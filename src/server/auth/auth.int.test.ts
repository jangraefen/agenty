import { randomUUID } from "node:crypto";
import { createServer, type Socket } from "node:net";
import { eq, inArray } from "drizzle-orm";
import { afterAll, beforeEach, describe, expect, it, vi } from "vitest";
import { account, session, user, verification } from "@/server/db/auth-schema";
import { getDb } from "@/server/db/client";
import { getEnv } from "@/server/env";
import { signInViaMock, startSignIn } from "../../../tests/support/oidc";
import { type AuthConfig, createAuth, createAuthIfIdpAnswers, hasOidcProvider } from "./auth";
import { getCurrentUser, requireUser } from "./session";

const request = vi.hoisted(() => ({ cookie: "" }));
vi.mock("next/headers", () => ({
  headers: async () => new Headers(request.cookie ? { cookie: request.cookie } : {}),
}));

const env = getEnv();
const config: AuthConfig = {
  baseURL: env.BETTER_AUTH_URL,
  secret: env.BETTER_AUTH_SECRET,
  oidc: {
    discoveryUrl: env.OIDC_DISCOVERY_URL,
    clientId: env.OIDC_CLIENT_ID,
    clientSecret: env.OIDC_CLIENT_SECRET,
  },
};
const baseURL = config.baseURL;
const auth = createAuth(config);
const db = getDb();

const createdEmails: string[] = [];
// Sign-in state rows (`verification.identifier` = `auth-state:<state>`, as Better Auth stores
// them). A callback that gets to parse the state deletes its row; a test that fails or stops
// before that leaves it behind.
const createdStateIdentifiers: string[] = [];
const trackState = (state: string) => createdStateIdentifiers.push(`auth-state:${state}`);

function testIdentity() {
  const id = randomUUID();
  const email = `t-${id}@example.test`;
  createdEmails.push(email);
  return { sub: `sub-${id}`, email };
}

async function signIn(identity = testIdentity()) {
  const result = await signInViaMock(auth, {
    baseURL,
    ...identity,
    name: "Test User",
    onState: trackState,
  });
  return { ...identity, ...result };
}

function post(path: string, body: unknown, cookie = "") {
  return auth.handler(
    new Request(`${baseURL}/api/auth${path}`, {
      method: "POST",
      headers: { "content-type": "application/json", origin: baseURL, cookie },
      body: JSON.stringify(body),
    }),
  );
}

async function usersWithEmail(email: string) {
  return db.select().from(user).where(eq(user.email, email));
}

afterAll(async () => {
  if (createdEmails.length > 0) await db.delete(user).where(inArray(user.email, createdEmails));
  if (createdStateIdentifiers.length > 0) {
    await db.delete(verification).where(inArray(verification.identifier, createdStateIdentifiers));
  }
});

beforeEach(() => {
  request.cookie = "";
});

describe("OIDC sign-in", () => {
  it("creates one user, one OIDC account and a session on the first sign-in", async () => {
    const result = await signIn();

    expect(result.status).toBe(302);
    expect(result.location).toBe("/");
    const users = await usersWithEmail(result.email);
    expect(users).toHaveLength(1);
    const userId = users[0]?.id ?? "";
    const accounts = await db.select().from(account).where(eq(account.userId, userId));
    expect(accounts).toHaveLength(1);
    expect(accounts[0]).toMatchObject({ providerId: "oidc", accountId: result.sub });
    const sessions = await db.select().from(session).where(eq(session.userId, userId));
    expect(sessions).toHaveLength(1);

    const current = await auth.api.getSession({ headers: new Headers({ cookie: result.cookie }) });
    expect(current?.user).toMatchObject({ id: userId, email: result.email });
  });

  it("signs the same IdP subject in again as the same user with the same account", async () => {
    const first = await signIn();
    const second = await signIn({ sub: first.sub, email: first.email });

    expect(second.location).toBe("/");
    const users = await usersWithEmail(first.email);
    expect(users).toHaveLength(1);
    const userId = users[0]?.id ?? "";
    const current = await auth.api.getSession({ headers: new Headers({ cookie: second.cookie }) });
    expect(current?.user.id).toBe(userId);
    const accounts = await db.select().from(account).where(eq(account.userId, userId));
    expect(accounts).toHaveLength(1);
  });

  it("stores the access token encrypted", async () => {
    const result = await signIn();
    const [stored] = await db
      .select({ accessToken: account.accessToken })
      .from(account)
      .innerJoin(user, eq(account.userId, user.id))
      .where(eq(user.email, result.email));

    expect(stored?.accessToken).toBeTruthy();
    expect(stored?.accessToken).not.toMatch(/^eyJ[\w-]*\.[\w-]*\.[\w-]*$/);
  });

  it("sends an IdP error back to the sign-in page, not Better Auth's error page", async () => {
    const start = await startSignIn(auth, baseURL);
    trackState(start.state);

    const response = await auth.handler(
      new Request(`${baseURL}/api/auth/callback/oidc?error=access_denied&state=${start.state}`, {
        headers: { cookie: start.cookie },
      }),
    );

    expect(response.status).toBe(302);
    expect(response.headers.get("location")).toMatch(/^\/sign-in\?error=access_denied/);
  });

  it("sends a callback with an unknown state to the sign-in page", async () => {
    const response = await auth.handler(
      new Request(`${baseURL}/api/auth/callback/oidc?code=x&state=unknown`),
    );

    expect(response.status).toBe(302);
    expect(response.headers.get("location")).toMatch(/^\/sign-in\?error=/);
  });

  it("signs out locally, without an IdP logout URL, and ends the session", async () => {
    const result = await signIn();

    const response = await post("/sign-out", {}, result.cookie);

    expect(response.status).toBe(200);
    expect(response.headers.get("location")).toBeNull();
    const body = (await response.json()) as { success: boolean; url?: string };
    expect(body.success).toBe(true);
    expect(body.url).toBeUndefined();
    const current = await auth.api.getSession({ headers: new Headers({ cookie: result.cookie }) });
    expect(current).toBeNull();
  });
});

describe("ID token sign-in", () => {
  it("rejects an ID token at /sign-in/social without creating a session", async () => {
    const response = await post("/sign-in/social", { provider: "oidc", idToken: { token: "x" } });

    expect(response.status).toBe(400);
    expect(await response.json()).toMatchObject({ code: "id_token_sign_in_disabled" });
    expect(response.headers.getSetCookie().join(";")).not.toContain("session_token");
  });

  it("rejects an ID token at /link-social", async () => {
    const result = await signIn();

    const response = await post(
      "/link-social",
      { provider: "oidc", idToken: { token: "x" } },
      result.cookie,
    );

    expect(response.status).toBe(400);
    expect(await response.json()).toMatchObject({ code: "id_token_sign_in_disabled" });
  });
});

describe("IdP discovery failure", () => {
  it("leaves the provider out, so sign-in answers PROVIDER_NOT_FOUND", async () => {
    // Better Auth logs the failed discovery; keep the test output clean.
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
    try {
      const broken = createAuth({
        ...config,
        oidc: { ...config.oidc, discoveryUrl: "http://localhost:1/x" },
      });

      expect(await hasOidcProvider(broken)).toBe(false);
      const response = await broken.handler(
        new Request(`${baseURL}/api/auth/sign-in/social`, {
          method: "POST",
          headers: { "content-type": "application/json", origin: baseURL },
          body: JSON.stringify({ provider: "oidc", callbackURL: "/" }),
        }),
      );
      expect(response.status).toBe(404);
      expect(await response.json()).toMatchObject({ code: "PROVIDER_NOT_FOUND" });
    } finally {
      consoleError.mockRestore();
    }
  });

  it("has the provider when discovery succeeds", async () => {
    expect(await hasOidcProvider(auth)).toBe(true);
  });
});

describe("hanging IdP", () => {
  it(
    "answers getSession within seconds, without the provider",
    async () => {
      // Accepts connections and never answers.
      const sockets: Socket[] = [];
      const server = createServer((socket) => sockets.push(socket));
      await new Promise<void>((resolve) => server.listen(0, "localhost", resolve));
      const address = server.address();
      const port = typeof address === "object" && address ? address.port : 0;
      try {
        const started = Date.now();
        const hanging = await createAuthIfIdpAnswers({
          ...config,
          oidc: { ...config.oidc, discoveryUrl: `http://localhost:${port}/x` },
        });

        expect(await hanging.api.getSession({ headers: new Headers() })).toBeNull();
        expect(await hasOidcProvider(hanging)).toBe(false);
        expect(Date.now() - started).toBeLessThan(8 * 1000);
      } finally {
        for (const socket of sockets) socket.destroy();
        await new Promise((resolve) => server.close(resolve));
      }
    },
    15 * 1000,
  );

  it("keeps the provider when the IdP answers", async () => {
    expect(await hasOidcProvider(await createAuthIfIdpAnswers(config))).toBe(true);
  });
});

describe("session helpers", () => {
  it("getCurrentUser returns the signed-in user of the request", async () => {
    const result = await signIn();
    request.cookie = result.cookie;

    const current = await getCurrentUser();

    expect(current).toMatchObject({
      id: expect.any(String),
      name: "Test User",
      email: result.email,
    });
    expect(await requireUser()).toEqual(current);
  });

  it("getCurrentUser returns null without a session", async () => {
    expect(await getCurrentUser()).toBeNull();
  });

  it("requireUser redirects to /sign-in without a session", async () => {
    await expect(requireUser()).rejects.toMatchObject({
      digest: expect.stringMatching(/^NEXT_REDIRECT;[a-z]+;\/sign-in;/),
    });
  });
});
