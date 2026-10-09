import { randomBytes } from "node:crypto";
import { eq, inArray } from "drizzle-orm";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { member, organization, ssoProvider, user } from "@/server/db/auth-schema";
import { createDb } from "@/server/db/client";
import { seedDevProviders } from "@/server/db/seed";
import { signInViaMock } from "../../../tests/support/sso";
import { createAuth } from "./auth";
import { listMembers } from "./members";
import { ForbiddenError, getTenantContext, UnauthorizedError } from "./tenant";

const baseURL = "http://localhost:3000";
const db = createDb(process.env.DATABASE_URL ?? "", { max: 2 });
const auth = createAuth({ db, baseURL });
const emails: string[] = [];
const slugs: string[] = [];
const providerIds: string[] = [];

const random = () => randomBytes(4).toString("hex");

async function signedInMember(opts: { domain?: string; claims?: Record<string, unknown> } = {}) {
  const email = `t-${random()}@${opts.domain ?? "corp.test"}`;
  emails.push(email);
  const slug = `org-${random()}`;
  slugs.push(slug);
  const result = await signInViaMock(auth, {
    baseURL,
    email,
    name: `Name ${random()}`,
    claims: { org: slug, ...opts.claims },
  });
  expect(result.location).toBe("/");
  return { email, slug, headers: new Headers({ cookie: result.cookie }) };
}

async function userId(email: string) {
  const [row] = await db.select({ id: user.id }).from(user).where(eq(user.email, email));
  return row?.id ?? "";
}

beforeAll(async () => {
  await seedDevProviders(db);
});

afterAll(async () => {
  if (emails.length) await db.delete(user).where(inArray(user.email, emails));
  if (slugs.length) await db.delete(organization).where(inArray(organization.slug, slugs));
  if (providerIds.length)
    await db.delete(ssoProvider).where(inArray(ssoProvider.providerId, providerIds));
  await db.$client.end();
});

describe("tenant context", () => {
  it("rejects a request without a session cookie", async () => {
    await expect(getTenantContext({ auth, db, headers: new Headers() })).rejects.toThrow(
      UnauthorizedError,
    );
  });

  it("rejects a garbage session cookie", async () => {
    const headers = new Headers({ cookie: "better-auth.session_token=garbage.garbage" });
    await expect(getTenantContext({ auth, db, headers })).rejects.toThrow(UnauthorizedError);
  });

  it("returns the organization and role of the signed-in member", async () => {
    const { email, slug, headers } = await signedInMember({
      claims: { groups: ["agenty-admins"] },
    });
    const ctx = await getTenantContext({ auth, db, headers });
    const [org] = await db.select().from(organization).where(eq(organization.slug, slug));
    expect(ctx).toMatchObject({
      userId: await userId(email),
      organizationId: org?.id,
      role: "admin",
    });
  });

  it("refuses a signed-in user whose membership was deleted", async () => {
    const { email, headers } = await signedInMember();
    await db.delete(member).where(eq(member.userId, await userId(email)));
    await expect(getTenantContext({ auth, db, headers })).rejects.toThrow(ForbiddenError);
  });

  it("refuses a member whose organization lost its identity provider", async () => {
    const id = `tmp-${random()}`;
    providerIds.push(id);
    await seedDevProviders(db, [
      { providerId: id, issuer: `http://localhost:8080/${id}`, domain: `${id}.test` },
    ]);
    const { headers } = await signedInMember({ domain: `${id}.test` });
    await expect(getTenantContext({ auth, db, headers })).resolves.toBeDefined();
    await db.delete(ssoProvider).where(eq(ssoProvider.providerId, id));
    await expect(getTenantContext({ auth, db, headers })).rejects.toThrow(ForbiddenError);
  });
});

describe("listMembers", () => {
  it("never returns members of another organization", async () => {
    const a = await signedInMember();
    const b = await signedInMember();
    const ctxA = await getTenantContext({ auth, db, headers: a.headers });
    const ctxB = await getTenantContext({ auth, db, headers: b.headers });
    expect((await listMembers(ctxA, db)).map((m) => m.email)).toEqual([a.email]);
    expect((await listMembers(ctxB, db)).map((m) => m.email)).toEqual([b.email]);
  });
});
