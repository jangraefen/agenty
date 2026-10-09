import { describe, expect, it, vi } from "vitest";
import {
  emailDomain,
  idpHostResolvesPublic,
  isAllowedIdpUrl,
  type ProviderRow,
  parseProvider,
  selectProvider,
} from "./providers";

const row = (over: Partial<ProviderRow> = {}): ProviderRow =>
  ({
    id: "00000000-0000-0000-0000-000000000001",
    providerId: "corp",
    issuer: "http://localhost:8080/corp",
    domain: "corp.test",
    userId: null,
    organizationId: null,
    samlConfig: null,
    oidcConfig: JSON.stringify({
      clientId: "agenty",
      clientSecret: "dev-secret",
      pkce: true,
      scopes: ["openid", "email", "profile"],
    }),
    organizationClaim: "org",
    roleClaim: "groups",
    adminValues: "agenty-admins",
    ...over,
  }) as ProviderRow;
const oidc = (o: object) =>
  JSON.stringify({
    clientId: "agenty",
    clientSecret: "dev-secret",
    pkce: true,
    scopes: ["openid", "email", "profile"],
    ...o,
  });

describe("parseProvider", () => {
  it("accepts a complete row", () => {
    expect(parseProvider(row())).toMatchObject({
      ok: true,
      provider: { domains: ["corp.test"], adminValues: ["agenty-admins"], hasEndpoints: false },
    });
  });

  it("accepts an already parsed oidcConfig (Better Auth adapter rows)", () => {
    const parsed = { ...row(), oidcConfig: JSON.parse(row().oidcConfig ?? "{}") };
    expect(parseProvider(parsed)).toMatchObject({ ok: true });
  });

  it("recognises discovered endpoints", () => {
    const r = row({
      oidcConfig: oidc({
        authorizationEndpoint: "http://localhost:8080/corp/authorize",
        tokenEndpoint: "http://localhost:8080/corp/token",
        jwksEndpoint: "http://localhost:8080/corp/jwks",
      }),
    });
    expect(parseProvider(r)).toMatchObject({ ok: true, provider: { hasEndpoints: true } });
  });

  it.each([
    ["missing pkce", { oidcConfig: oidc({ pkce: undefined }) }, "oidcConfig.pkce"],
    ["pkce false", { oidcConfig: oidc({ pkce: false }) }, "oidcConfig.pkce"],
    [
      "offline_access",
      { oidcConfig: oidc({ scopes: ["openid", "email", "profile", "offline_access"] }) },
      "oidcConfig.scopes",
    ],
    [
      "userInfoEndpoint",
      { oidcConfig: oidc({ userInfoEndpoint: "http://x/userinfo" }) },
      "oidcConfig",
    ],
    ["allowIdpInitiated", { oidcConfig: oidc({ allowIdpInitiated: true }) }, "oidcConfig"],
    ["mapping", { oidcConfig: oidc({ mapping: { email: "upn" } }) }, "oidcConfig"],
    ["overrideUserInfo", { oidcConfig: oidc({ overrideUserInfo: true }) }, "oidcConfig"],
    [
      "private_key_jwt",
      { oidcConfig: oidc({ tokenEndpointAuthentication: "private_key_jwt" }) },
      "oidcConfig",
    ],
    ["no client secret", { oidcConfig: oidc({ clientSecret: "" }) }, "oidcConfig.clientSecret"],
    ["invalid JSON", { oidcConfig: "{" }, "oidcConfig"],
    ["no oidc config", { oidcConfig: null }, "oidcConfig"],
    ["saml config", { samlConfig: "{}" }, "samlConfig"],
    ["plugin organization", { organizationId: "x" }, "organizationId"],
    ["no organization claim", { organizationClaim: "" }, "organizationClaim"],
    ["upper-case domain", { domain: "CORP.test" }, "domain"],
    ["empty domain entry", { domain: "corp.test," }, "domain"],
    ["issuer not http(s)", { issuer: "ftp://x" }, "issuer"],
  ] as const)("rejects %s, naming the field", (_name, over, field) => {
    const result = parseProvider(row(over as Partial<ProviderRow>));
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.fields.join(" ")).toContain(field);
  });

  it("never puts the client secret into the result's fields", () => {
    const result = parseProvider(
      row({ oidcConfig: oidc({ clientSecret: "super-secret", pkce: false }) }),
    );
    expect(JSON.stringify(result)).not.toContain("super-secret");
  });
});

describe("provider selection", () => {
  const rows = [
    { providerId: "corp", domain: "corp.test" },
    { providerId: "partner", domain: "partner.test,eu.partner.test" },
  ];
  it("requires exactly one @", () => {
    expect(emailDomain("a@b@corp.test")).toBeUndefined();
    expect(emailDomain("no-at")).toBeUndefined();
    expect(emailDomain("Alice@CORP.Test")).toBe("corp.test");
  });
  it("matches case-insensitively, exact before subdomain", () => {
    expect(selectProvider(rows, "Alice@CORP.Test")?.providerId).toBe("corp");
    expect(selectProvider(rows, "x@eu.partner.test")?.providerId).toBe("partner");
    expect(selectProvider(rows, "x@dev.corp.test")?.providerId).toBe("corp");
    expect(selectProvider(rows, "x@notcorp.test")).toBeUndefined();
  });
  it("prefers an exact match over another provider's subdomain match", () => {
    const overlapping = [
      { providerId: "a-parent", domain: "corp.test" },
      { providerId: "b-child", domain: "eu.corp.test" },
    ];
    expect(selectProvider(overlapping, "x@eu.corp.test")?.providerId).toBe("b-child");
  });
});

describe("isAllowedIdpUrl", () => {
  const trusting = (origin: string) => (url: string) => new URL(url).origin === origin;
  const none = () => false;

  it("allows a public https host without trusted origins", () => {
    expect(
      isAllowedIdpUrl("https://login.example.com/.well-known/openid-configuration", none),
    ).toBe(true);
  });
  it("allows a loopback IdP only when its origin is trusted", () => {
    expect(isAllowedIdpUrl("http://localhost:8080/corp", none)).toBe(false);
    expect(isAllowedIdpUrl("http://localhost:8080/corp", trusting("http://localhost:8080"))).toBe(
      true,
    );
  });
  it("allows a private-network IdP only when its origin is trusted", () => {
    expect(isAllowedIdpUrl("http://10.0.0.5/x", none)).toBe(false);
    expect(isAllowedIdpUrl("http://10.0.0.5/x", trusting("http://10.0.0.5"))).toBe(true);
  });
});

describe("idpHostResolvesPublic", () => {
  const none = () => false;
  const resolvingTo = (...addresses: string[]) =>
    vi.fn(async (_host: string) => addresses.map((address) => ({ address })));

  it("accepts a host that resolves to public addresses only", async () => {
    const lookup = resolvingTo("93.184.215.14", "2606:2800:21f:cb07:6820:80da:af6b:8b2c");
    expect(await idpHostResolvesPublic("https://idp.example.com/x", none, lookup)).toBe(true);
    expect(lookup).toHaveBeenCalledWith("idp.example.com");
  });

  it.each([
    ["private", "10.0.0.5"],
    ["loopback", "127.0.0.1"],
    ["link-local (cloud metadata)", "169.254.169.254"],
    ["IPv6 unique local", "fd00::1"],
    ["IPv6 loopback", "::1"],
  ])("rejects a public name that resolves to a %s address", async (_name, address) => {
    const lookup = resolvingTo("93.184.215.14", address);
    expect(await idpHostResolvesPublic("https://idp.example.com/x", none, lookup)).toBe(false);
  });

  it.each([
    ["IPv4", "http://10.0.0.5/x"],
    ["IPv6", "http://[fd00::1]/x"],
  ])("rejects a private %s literal without a lookup", async (_name, url) => {
    const lookup = resolvingTo("93.184.215.14");
    expect(await idpHostResolvesPublic(url, none, lookup)).toBe(false);
    expect(lookup).not.toHaveBeenCalled();
  });

  it("rejects a host that does not resolve", async () => {
    const lookup = vi.fn(async () => {
      throw new Error("ENOTFOUND");
    });
    expect(await idpHostResolvesPublic("https://idp.example.com/x", none, lookup)).toBe(false);
  });

  it("skips the lookup for a trusted origin", async () => {
    const lookup = resolvingTo("10.0.0.5");
    const trusted = (url: string) => new URL(url).origin === "http://localhost:8080";
    expect(await idpHostResolvesPublic("http://localhost:8080/corp", trusted, lookup)).toBe(true);
    expect(lookup).not.toHaveBeenCalled();
  });
});
