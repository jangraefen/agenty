import { runWithEndpointContext } from "@better-auth/core/context";
import type { SSOOIDCUserResolutionInput } from "@better-auth/sso";
import type { DBTransactionAdapter } from "better-auth";
import { APIError } from "better-auth/api";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  type ProvisioningDecision,
  provisionMembership,
  rememberDecision,
  resolveSignIn,
  takeDecision,
} from "./provisioning";

type Row = Record<string, unknown>;
type Where = { field: string; value: unknown }[];

/** An in-memory adapter: findOne/findMany/create/update over plain rows, recording writes. */
function fakeAdapter(tables: Record<string, Row[]> = {}, fail?: { on: string; error: unknown }) {
  const calls: { op: string; model: string; data?: Row; update?: Row; where?: Where }[] = [];
  let nextId = 1;
  const matches = (row: Row, where: Where = []) =>
    where.every(({ field, value }) => row[field] === value);
  const rows = (model: string) => {
    tables[model] ??= [];
    return tables[model];
  };
  const maybeFail = (op: string, model: string) => {
    if (fail?.on === `${op}:${model}`) throw fail.error;
  };
  const adapter = {
    findOne: async ({ model, where }: { model: string; where: Where }) => {
      maybeFail("findOne", model);
      return rows(model).find((row) => matches(row, where)) ?? null;
    },
    findMany: async ({ model, where }: { model: string; where?: Where }) => {
      maybeFail("findMany", model);
      return rows(model).filter((row) => matches(row, where));
    },
    create: async ({ model, data }: { model: string; data: Row }) => {
      maybeFail("create", model);
      calls.push({ op: "create", model, data });
      const row = { id: `${model}-${nextId++}`, ...data };
      rows(model).push(row);
      return row;
    },
    update: async ({ model, where, update }: { model: string; where: Where; update: Row }) => {
      maybeFail("update", model);
      calls.push({ op: "update", model, where, update });
      const row = rows(model).find((r) => matches(r, where));
      if (row) Object.assign(row, update);
      return row ?? null;
    },
  };
  return { adapter: adapter as unknown as DBTransactionAdapter, tables, calls };
}

const decision: ProvisioningDecision = {
  organizationSlug: "acme",
  role: "member",
  providerId: "corp",
};

async function codeOf(promise: Promise<unknown>): Promise<string | undefined> {
  try {
    await promise;
  } catch (error) {
    expect(error).toBeInstanceOf(APIError);
    return (error as APIError).body?.code;
  }
  throw new Error("expected a rejection");
}

describe("decision hand-over", () => {
  it("returns a remembered decision exactly once", () => {
    const ctx = {};
    rememberDecision(ctx, decision);
    expect(takeDecision(ctx)).toEqual(decision);
    expect(takeDecision(ctx)).toBeUndefined();
  });

  it("returns nothing without a context", () => {
    expect(takeDecision(null)).toBeUndefined();
    expect(takeDecision(undefined)).toBeUndefined();
  });

  it("keeps the decisions of different contexts apart", () => {
    const a = {};
    const b = {};
    rememberDecision(a, decision);
    expect(takeDecision(b)).toBeUndefined();
    expect(takeDecision(a)).toEqual(decision);
  });
});

describe("provisionMembership", () => {
  afterEach(() => vi.restoreAllMocks());

  it("creates the organization and the membership when both are absent", async () => {
    const { adapter, tables } = fakeAdapter();
    await provisionMembership(adapter, "u1", { ...decision, role: "admin" });
    expect(tables.organization).toEqual([
      expect.objectContaining({ slug: "acme", providerId: "corp" }),
    ]);
    const orgId = tables.organization?.[0]?.id;
    expect(tables.member).toEqual([
      expect.objectContaining({ userId: "u1", organizationId: orgId, role: "admin" }),
    ]);
  });

  it("updates only role and updatedAt for an existing member of the same organization", async () => {
    const { adapter, calls } = fakeAdapter({
      organization: [{ id: "o1", slug: "acme", providerId: "corp" }],
      member: [{ id: "m1", userId: "u1", organizationId: "o1", role: "admin" }],
    });
    await provisionMembership(adapter, "u1", decision);
    expect(calls).toEqual([
      {
        op: "update",
        model: "member",
        where: [{ field: "id", value: "m1" }],
        update: { role: "member", updatedAt: expect.any(Date) },
      },
    ]);
  });

  it.each([
    ["another provider", "partner"],
    ["no provider (orphaned)", null],
  ])("refuses an organization owned by %s", async (_name, providerId) => {
    const { adapter, calls } = fakeAdapter({
      organization: [{ id: "o1", slug: "acme", providerId }],
    });
    expect(await codeOf(provisionMembership(adapter, "u1", decision))).toBe(
      "organization_owned_by_other_provider",
    );
    expect(calls).toEqual([]);
  });

  it("refuses a member whose row points to another organization", async () => {
    const { adapter, calls } = fakeAdapter({
      organization: [{ id: "o1", slug: "acme", providerId: "corp" }],
      member: [{ id: "m1", userId: "u1", organizationId: "o2", role: "member" }],
    });
    expect(await codeOf(provisionMembership(adapter, "u1", decision))).toBe("organization_changed");
    expect(calls).toEqual([]);
  });

  it.each([
    ["directly", Object.assign(new Error("duplicate key"), { code: "23505" })],
    ["as cause", new Error("query failed", { cause: { code: "23505" } })],
  ])("maps a unique violation (%s) to try_again", async (_name, error) => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    const { adapter } = fakeAdapter({}, { on: "create:organization", error });
    expect(await codeOf(provisionMembership(adapter, "u1", decision))).toBe("try_again");
  });

  it("maps any other failure to provisioning_failed, logging the error class only", async () => {
    const log = vi.spyOn(console, "error").mockImplementation(() => {});
    const { adapter } = fakeAdapter(
      {},
      { on: "findOne:organization", error: new TypeError("secret-detail") },
    );
    expect(await codeOf(provisionMembership(adapter, "u1", decision))).toBe("provisioning_failed");
    const logged = log.mock.calls.map((args) => args.join(" ")).join("\n");
    expect(logged).toContain("TypeError");
    expect(logged).not.toContain("secret-detail");
  });
});

const providerRow = {
  id: "p1",
  providerId: "corp",
  issuer: "http://localhost:8080/corp",
  domain: "corp.test",
  oidcConfig: JSON.stringify({
    clientId: "agenty",
    clientSecret: "dev-secret",
    pkce: true,
    scopes: ["openid", "email", "profile"],
  }),
  samlConfig: null,
  organizationId: null,
  organizationClaim: "org",
  roleClaim: "groups",
  adminValues: "agenty-admins",
};

function input(over: Partial<SSOOIDCUserResolutionInput> = {}): SSOOIDCUserResolutionInput {
  return {
    protocol: "oidc",
    providerId: "corp",
    accountKey: { issuer: "http://localhost:8080/corp", accountId: "sub-1" },
    providerUser: { email: "Alice@CORP.Test", emailVerified: false, name: "Alice" },
    providerReference: {
      providerId: "corp",
      source: { type: "persisted", recordId: "p1" },
      authenticationConfigurationFingerprint: "x",
    },
    providerClaims: { org: "acme" },
    verifiedIdTokenClaims: { org: "acme" },
    ...over,
  };
}

/** Runs resolveSignIn as the plugin does: inside an endpoint context. */
async function resolve(tables: Record<string, Row[]>, over: Partial<SSOOIDCUserResolutionInput>) {
  const ctx = { context: {} } as Parameters<typeof runWithEndpointContext>[0];
  const { adapter } = fakeAdapter({ ssoProvider: [providerRow], ...tables });
  const result = await runWithEndpointContext(ctx, () => resolveSignIn(input(over), adapter));
  return { result, remembered: takeDecision(ctx) };
}

describe("resolveSignIn", () => {
  it("remembers the decision from the ID token and continues", async () => {
    const { result, remembered } = await resolve(
      {},
      { verifiedIdTokenClaims: { org: "acme", groups: ["agenty-admins"] } },
    );
    expect(result).toEqual({ action: "continue" });
    expect(remembered).toEqual({ organizationSlug: "acme", role: "admin", providerId: "corp" });
  });

  it("takes the role from the ID token, not from userinfo", async () => {
    const { result, remembered } = await resolve(
      {},
      {
        providerClaims: { org: "acme", groups: ["agenty-admins"] },
        verifiedIdTokenClaims: { org: "acme" },
      },
    );
    expect(result).toEqual({ action: "continue" });
    expect(remembered?.role).toBe("member");
  });

  it("rejects an account bound to another provider", async () => {
    const { result, remembered } = await resolve(
      {
        user: [{ id: "u1", email: "alice@corp.test" }],
        account: [{ id: "a1", userId: "u1", providerId: "partner", accountId: "x" }],
      },
      {},
    );
    expect(result).toEqual({ action: "reject", code: "account_bound_to_other_provider" });
    expect(remembered).toBeUndefined();
  });

  it("continues for an existing user whose accounts are all of this provider", async () => {
    const { result, remembered } = await resolve(
      {
        user: [{ id: "u1", email: "alice@corp.test" }],
        account: [{ id: "a1", userId: "u1", providerId: "corp", accountId: "sub-1" }],
      },
      {},
    );
    expect(result).toEqual({ action: "continue" });
    expect(remembered?.organizationSlug).toBe("acme");
  });

  it.each([
    ["organization_claim_missing", { verifiedIdTokenClaims: {} }],
    [
      "email_domain_mismatch",
      { providerUser: { email: "alice@partner.test", emailVerified: false, name: "A" } },
    ],
  ])("rejects with %s", async (code, over) => {
    const { result, remembered } = await resolve({}, over);
    expect(result).toEqual({ action: "reject", code });
    expect(remembered).toBeUndefined();
  });

  it("rejects an invalid provider row as idp_unavailable", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    const { result } = await resolve(
      { ssoProvider: [{ ...providerRow, pkce: undefined, oidcConfig: "{}" }] },
      {},
    );
    expect(result).toEqual({ action: "reject", code: "idp_unavailable" });
    vi.restoreAllMocks();
  });

  it("rejects an unknown provider as idp_unavailable", async () => {
    const { result } = await resolve({}, { providerId: "nope" });
    expect(result).toEqual({ action: "reject", code: "idp_unavailable" });
  });
});
