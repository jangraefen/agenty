import { describe, expect, it } from "vitest";
import { decideSignIn } from "./claims";
import type { ValidProvider } from "./providers";

const provider: ValidProvider = {
  providerId: "corp",
  issuer: "http://localhost:8080/corp",
  domains: ["corp.test"],
  organizationClaim: "org",
  roleClaim: "groups",
  adminValues: ["agenty-admins"],
  oidc: {
    clientId: "agenty",
    clientSecret: "dev-secret",
    pkce: true,
    scopes: ["openid", "email", "profile"],
  },
  hasEndpoints: false,
};

const decide = (claims: Record<string, unknown>, email = "alice@corp.test", p = provider) =>
  decideSignIn({ provider: p, email, claims });

describe("decideSignIn", () => {
  it("maps the organization claim to the slug and defaults to member", () => {
    expect(decide({ org: "acme" })).toEqual({
      ok: true,
      decision: { organizationSlug: "acme", role: "member" },
    });
  });

  it("grants admin when the role claim contains an admin value (array or string)", () => {
    expect(decide({ org: "acme", groups: ["x", "agenty-admins"] })).toMatchObject({
      ok: true,
      decision: { role: "admin" },
    });
    expect(decide({ org: "acme", groups: "agenty-admins" })).toMatchObject({
      ok: true,
      decision: { role: "admin" },
    });
  });

  it.each([
    ["missing", undefined],
    ["empty", ""],
    ["upper-case", "Acme"],
    ["too short", "a"],
    ["too long", "a".repeat(64)],
    ["underscore", "a_b"],
    ["number", 42],
    ["array", ["acme"]],
    ["null", null],
  ])("rejects an organization claim that is %s without throwing", (_name, org) => {
    expect(decide({ org })).toEqual({ ok: false, code: "organization_claim_missing" });
  });

  it.each([
    ["non-string array", [1, null, {}]],
    ["object", { "agenty-admins": true }],
    ["other string", "x"],
  ])("treats an odd role claim (%s) as member without throwing", (_name, groups) => {
    expect(decide({ org: "acme", groups })).toMatchObject({
      ok: true,
      decision: { role: "member" },
    });
  });

  it("is member without a role claim, even when groups would match", () => {
    const { roleClaim: _, ...noRoleClaim } = provider;
    expect(
      decide({ org: "acme", groups: ["agenty-admins"] }, "alice@corp.test", noRoleClaim),
    ).toMatchObject({ ok: true, decision: { role: "member" } });
  });

  it("rejects an email outside the provider's domains", () => {
    expect(decide({ org: "acme" }, "bob@partner.test")).toEqual({
      ok: false,
      code: "email_domain_mismatch",
    });
  });

  it("accepts a mixed-case email on a subdomain of the provider's domain", () => {
    expect(decide({ org: "acme" }, "Bob@Dev.Corp.Test")).toMatchObject({ ok: true });
  });
});
