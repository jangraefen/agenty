import { randomBytes } from "node:crypto";
import { eq, inArray } from "drizzle-orm";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { ssoProvider } from "./auth-schema";
import { createDb } from "./client";
import { type DevProvider, seedDevProviders } from "./seed";

// Never touches `corp` or `partner`: other test files sign in through them in parallel, and
// changing their rows would change the provider fingerprint and fail those callbacks.
const db = createDb(process.env.DATABASE_URL ?? "", { max: 1 });
const tag = `seed-${randomBytes(4).toString("hex")}`;
const providers: DevProvider[] = ["a", "b"].map((suffix) => ({
  providerId: `${tag}-${suffix}`,
  issuer: `http://localhost:8080/${tag}-${suffix}`,
  domain: `${tag}-${suffix}.test`,
}));
const ids = providers.map((p) => p.providerId);

async function rows() {
  return db.select().from(ssoProvider).where(inArray(ssoProvider.providerId, ids));
}

beforeAll(async () => {
  await seedDevProviders(db, providers);
});

afterAll(async () => {
  await db.delete(ssoProvider).where(inArray(ssoProvider.providerId, ids));
  await db.$client.end();
});

describe("seedDevProviders", () => {
  it("registers each provider with the mock IdP's client and claim mapping", async () => {
    const found = await rows();
    expect(found.map((r) => r.providerId).sort()).toEqual([...ids].sort());
    for (const row of found) {
      expect(row.organizationClaim).toBe("org");
      expect(row.roleClaim).toBe("groups");
      expect(row.adminValues).toBe("agenty-admins");
      expect(row.userId).toBeNull();
      expect(JSON.parse(row.oidcConfig ?? "null")).toEqual({
        clientId: "agenty",
        clientSecret: "dev-secret",
        pkce: true,
        scopes: ["openid", "email", "profile"],
      });
    }
  });

  it("never modifies an existing row (the provider fingerprint must stay stable)", async () => {
    const target = ids[0] as string;
    const [before] = await db.select().from(ssoProvider).where(eq(ssoProvider.providerId, target));
    const marked = JSON.stringify({ ...JSON.parse(before?.oidcConfig ?? "{}"), marker: true });
    await db
      .update(ssoProvider)
      .set({ oidcConfig: marked })
      .where(eq(ssoProvider.providerId, target));

    await seedDevProviders(db, providers);

    const [after] = await db.select().from(ssoProvider).where(eq(ssoProvider.providerId, target));
    expect(after?.oidcConfig).toBe(marked);
    expect(await rows()).toHaveLength(2);
  });
});
