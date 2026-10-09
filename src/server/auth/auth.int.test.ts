import { randomBytes } from "node:crypto";
import { eq, inArray } from "drizzle-orm";
import { afterAll, beforeAll, describe, expect, it, onTestFinished, vi } from "vitest";
import { ssoProvider } from "@/server/db/auth-schema";
import { createDb } from "@/server/db/client";
import { seedDevProviders } from "@/server/db/seed";
import { signInViaMock } from "../../../tests/support/sso";
import { ALLOWED_ENDPOINTS, type Auth, createAuth } from "./auth";

// Never modifies `corp` or `partner`: other test files sign in through them in parallel. Tests
// that need a broken or changing provider create their own row and delete it afterwards.
const baseURL = "http://localhost:3000";
const db = createDb(process.env.DATABASE_URL ?? "", { max: 2 });
const auth = createAuth({ db, baseURL });
const created: string[] = [];

const oidcConfig = (extra: object = {}) =>
  JSON.stringify({
    clientId: "agenty",
    clientSecret: "dev-secret",
    pkce: true,
    scopes: ["openid", "email", "profile"],
    ...extra,
  });

/** A provider row of its own: random provider id, mock issuer path and domain. */
async function createProvider(over: { issuer?: string; oidcConfig?: string } = {}) {
  const tag = `auth-${randomBytes(4).toString("hex")}`;
  created.push(tag);
  await db.insert(ssoProvider).values({
    providerId: tag,
    issuer: over.issuer ?? `http://localhost:8080/${tag}`,
    domain: `${tag}.test`,
    oidcConfig: over.oidcConfig ?? oidcConfig(),
    organizationClaim: "org",
  });
  return { providerId: tag, email: `alice@${tag}.test` };
}

async function storedConfig(providerId: string) {
  const [row] = await db
    .select({ oidcConfig: ssoProvider.oidcConfig })
    .from(ssoProvider)
    .where(eq(ssoProvider.providerId, providerId));
  return row?.oidcConfig ?? null;
}

function signIn(body: unknown, instance: Auth = auth) {
  return instance.handler(
    new Request(`${baseURL}/api/auth/sign-in/sso`, {
      method: "POST",
      headers: { "content-type": "application/json", origin: baseURL },
      body: JSON.stringify(body),
    }),
  );
}

const valid = { email: "alice@corp.test", callbackURL: "/", errorCallbackURL: "/sign-in" };

beforeAll(async () => {
  await seedDevProviders(db);
});

afterAll(async () => {
  if (created.length) await db.delete(ssoProvider).where(inArray(ssoProvider.providerId, created));
  await db.$client.end();
});

describe("Better Auth instance", () => {
  it("answers 404 for every endpoint outside the allowlist", async () => {
    // The callback without state logs Better Auth's state error; keep the output clean.
    const log = vi.spyOn(console, "error").mockImplementation(() => {});
    onTestFinished(() => log.mockRestore());
    for (const endpoint of Object.values(auth.api) as unknown[]) {
      const { path, options } = endpoint as { path?: string; options?: { method?: unknown } };
      if (!path) continue;
      const methods = options?.method;
      const method = String(Array.isArray(methods) ? methods[0] : methods);
      const url = `${baseURL}/api/auth${path.replace(":providerId", "corp").replace(/:\w+/g, "x")}`;
      const response = await auth.handler(
        new Request(url, {
          method: method === "*" ? "GET" : method,
          headers: { "content-type": "application/json", origin: baseURL },
          ...(method === "GET" || method === "*" ? {} : { body: "{}" }),
        }),
      );
      if (ALLOWED_ENDPOINTS.has(path)) expect.soft(response.status, path).not.toBe(404);
      else expect.soft(response.status, path).toBe(404);
    }
  });

  it("is wired with the SSO plugin and our organization tables only, no domain verification", () => {
    expect(auth.options.plugins.map((p) => p.id).sort()).toEqual(["agenty-organizations", "sso"]);
    const ssoPlugin = auth.options.plugins.find((p) => p.id === "sso") as {
      options?: { domainVerification?: unknown };
    };
    expect(ssoPlugin.options?.domainVerification).toBeUndefined();
  });
});

describe("sign-in request", () => {
  it.each([
    ["scopes", { scopes: ["openid", "offline_access"] }],
    ["additionalParams", { additionalParams: { prompt: "none" } }],
    ["providerId", { providerId: "partner" }],
    ["organizationSlug", { organizationSlug: "acme" }],
    ["requestSignUp", { requestSignUp: true }],
    ["an absolute callbackURL", { callbackURL: "https://evil.test/" }],
    ["a protocol-relative callbackURL", { callbackURL: "//evil.test" }],
    ["a backslash callbackURL", { callbackURL: "/\\evil.test" }],
    ["an email with two @", { email: "a@b@corp.test" }],
  ])("rejects a body with %s as invalid_request", async (_name, extra) => {
    const response = await signIn({ ...valid, ...extra });
    expect(response.status).toBe(400);
    expect(await response.json()).toMatchObject({ code: "invalid_request" });
  });

  it("rejects a JSON array body with 400, not 500", async () => {
    const response = await signIn([valid]);
    expect(response.status).toBe(400);
    expect(await response.json()).toMatchObject({ code: "invalid_request" });
  });

  it("rejects an email domain without a provider as unknown_domain", async () => {
    const response = await signIn({ ...valid, email: "alice@nowhere.test" });
    expect(response.status).toBe(400);
    expect(await response.json()).toMatchObject({ code: "unknown_domain" });
  });

  it("hands the plugin the provider chosen by the hook, not a lookup of its own", async () => {
    const { adapter } = await auth.$context;
    const findOne = vi.spyOn(adapter, "findOne");
    onTestFinished(() => findOne.mockRestore());
    const response = await signIn(valid);
    expect(response.status).toBe(200);
    const ssoLookups = findOne.mock.calls
      .map(([query]) => query)
      .filter((query) => query.model === "ssoProvider");
    expect(ssoLookups).toEqual([
      expect.objectContaining({ where: [{ field: "providerId", value: "corp" }] }),
    ]);
  });

  it("redirects to the provider chosen by email domain, with PKCE and without offline_access", async () => {
    const result = await signInViaMock(auth, {
      baseURL,
      email: "alice@corp.test",
      claims: { org: "acme" },
      stopAfterIdp: true,
    });
    const url = new URL((result.body as { url: string }).url);
    expect(url.href.startsWith("http://localhost:8080/corp/authorize")).toBe(true);
    expect(url.searchParams.get("code_challenge")).toBeTruthy();
    expect(url.searchParams.get("scope")).toBe("openid email profile");
    expect(url.searchParams.get("redirect_uri")?.endsWith("/api/auth/sso/callback/corp")).toBe(
      true,
    );
    expect(result.callbackUrl?.startsWith(`${baseURL}/api/auth/sso/callback/corp?`)).toBe(true);
  });
});

describe("one-off discovery", () => {
  it("writes the discovered endpoints once and never again", async () => {
    const { providerId, email } = await createProvider();
    expect(Object.keys(JSON.parse((await storedConfig(providerId)) ?? "{}"))).not.toContain(
      "authorizationEndpoint",
    );

    const first = await signIn({ ...valid, email });
    expect(first.status).toBe(200);
    const written = await storedConfig(providerId);
    const config = JSON.parse(written ?? "{}");
    expect(config).toMatchObject({
      authorizationEndpoint: `http://localhost:8080/${providerId}/authorize`,
      tokenEndpoint: `http://localhost:8080/${providerId}/token`,
      jwksEndpoint: `http://localhost:8080/${providerId}/jwks`,
    });
    expect(config).not.toHaveProperty("userInfoEndpoint");

    const second = await signIn({ ...valid, email });
    expect(second.status).toBe(200);
    expect(await storedConfig(providerId)).toBe(written);
  });

  it("refuses a discovery document of another issuer and writes nothing", async () => {
    const other = `other-${randomBytes(4).toString("hex")}`;
    const { providerId, email } = await createProvider({
      oidcConfig: oidcConfig({
        discoveryEndpoint: `http://localhost:8080/${other}/.well-known/openid-configuration`,
      }),
    });
    const before = await storedConfig(providerId);
    const response = await signIn({ ...valid, email });
    expect(response.status).toBe(503);
    expect(await response.json()).toMatchObject({ code: "idp_unavailable" });
    expect(await storedConfig(providerId)).toBe(before);
  });

  it("reports a misconfigured provider as idp_unavailable, logging the field but not the secret", async () => {
    const config = JSON.parse(oidcConfig({ clientSecret: "misconfigured-secret" }));
    delete config.pkce;
    const { email } = await createProvider({ oidcConfig: JSON.stringify(config) });
    const log = vi.spyOn(console, "error").mockImplementation(() => {});
    try {
      const response = await signIn({ ...valid, email });
      expect(response.status).toBe(503);
      expect(await response.json()).toMatchObject({ code: "idp_unavailable" });
      const logged = log.mock.calls.map((args) => args.join(" ")).join("\n");
      expect(logged).toContain("oidcConfig.pkce");
      expect(logged).not.toContain("misconfigured-secret");
    } finally {
      log.mockRestore();
    }
  });

  it("keeps other providers working while one IdP is unreachable", async () => {
    const instance = createAuth({
      db,
      baseURL,
      trustedOrigins: ["http://localhost:8080", "http://localhost:1"],
    });
    const { email } = await createProvider({ issuer: "http://localhost:1/x" });
    const log = vi.spyOn(console, "error").mockImplementation(() => {});
    try {
      const broken = await signIn({ ...valid, email }, instance);
      expect(broken.status).toBe(503);
      expect(await broken.json()).toMatchObject({ code: "idp_unavailable" });
    } finally {
      log.mockRestore();
    }
    const working = await signIn(valid, instance);
    expect(working.status).toBe(200);
    const { url } = (await working.json()) as { url: string };
    expect(url.startsWith("http://localhost:8080/corp/authorize")).toBe(true);
  });
});

describe("callback", () => {
  it("answers 404 for an unknown provider", async () => {
    const response = await auth.handler(
      new Request(`${baseURL}/api/auth/sso/callback/no-such-provider?code=x&state=y`),
    );
    expect(response.status).toBe(404);
  });
});
