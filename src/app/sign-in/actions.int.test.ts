import { inArray } from "drizzle-orm";
import { afterAll, afterEach, describe, expect, it, vi } from "vitest";
import { type AuthConfig, createAuth } from "@/server/auth/auth";
import { verification } from "@/server/db/auth-schema";
import { getDb } from "@/server/db/client";
import { getEnv } from "@/server/env";
import { signIn } from "./actions";

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

const getAuth = vi.hoisted(() => vi.fn());
vi.mock("@/server/auth/auth", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/server/auth/auth")>()),
  getAuth,
}));
vi.mock("next/headers", () => ({
  headers: async () => new Headers({ origin: env.BETTER_AUTH_URL }),
}));

const createdStateIdentifiers: string[] = [];

afterEach(() => {
  vi.restoreAllMocks();
});

afterAll(async () => {
  if (createdStateIdentifiers.length === 0) return;
  await getDb()
    .delete(verification)
    .where(inArray(verification.identifier, createdStateIdentifiers));
});

/** The target of the NEXT_REDIRECT that signIn() throws. */
async function redirectTarget(): Promise<string> {
  const error: unknown = await signIn().catch((thrown: unknown) => thrown);
  const digest = (error as { digest?: unknown }).digest;
  if (typeof digest !== "string" || !digest.startsWith("NEXT_REDIRECT;")) throw error;
  return digest.split(";")[2] ?? "";
}

describe("sign-in server action", () => {
  it("redirects to the identity provider's authorization URL", async () => {
    getAuth.mockResolvedValue(createAuth(config));

    const target = new URL(await redirectTarget());

    const state = target.searchParams.get("state");
    if (state) createdStateIdentifiers.push(`auth-state:${state}`);
    expect(state).toBeTruthy();
    expect(target.origin).toBe(new URL(config.oidc.discoveryUrl).origin);
    expect(target.searchParams.get("client_id")).toBe(config.oidc.clientId);
  });

  it("sends the user back with sign_in_unavailable while the provider is missing", async () => {
    // Better Auth logs the missing provider; keep the test output clean.
    vi.spyOn(console, "error").mockImplementation(() => {});
    getAuth.mockResolvedValue(createAuth(config, { withOidc: false }));

    expect(await redirectTarget()).toBe("/sign-in?error=sign_in_unavailable");
  });

  it("sends the user back with sign_in_failed on any other error and logs only its class", async () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
    getAuth.mockRejectedValue(new TypeError("secret detail"));

    expect(await redirectTarget()).toBe("/sign-in?error=sign_in_failed");
    expect(consoleError).toHaveBeenCalledWith("Sign-in failed: TypeError");
    expect(JSON.stringify(consoleError.mock.calls)).not.toContain("secret detail");
  });
});
