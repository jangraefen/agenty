import { randomBytes } from "node:crypto";
import { APIError } from "better-auth/api";
import { eq, inArray } from "drizzle-orm";
import { afterAll, beforeAll, describe, expect, it, onTestFinished, vi } from "vitest";
import { account, member, organization, session, ssoProvider, user } from "@/server/db/auth-schema";
import { createDb } from "@/server/db/client";
import { seedDevProviders } from "@/server/db/seed";
import { finishCallback, type SignInResult, signInViaMock } from "../../../tests/support/sso";
import { createAuth } from "./auth";

const baseURL = "http://localhost:3000";
const db = createDb(process.env.DATABASE_URL ?? "", { max: 2 });
const auth = createAuth({ db, baseURL });
const emails: string[] = [];
const slugs: string[] = [];

const random = () => randomBytes(4).toString("hex");
function newEmail(domain = "corp.test", prefix = "t") {
  const email = `${prefix}-${random()}@${domain}`;
  emails.push(email);
  return email;
}
function newSlug() {
  const slug = `org-${random()}`;
  slugs.push(slug);
  return slug;
}

function signIn(email: string, claims: Record<string, unknown>, idpEmail?: string) {
  return signInViaMock(auth, { baseURL, email, claims, ...(idpEmail ? { idpEmail } : {}) });
}

/** The `error` code of a redirect to the sign-in page, or undefined for any other location. */
function errorOf(result: SignInResult) {
  const url = new URL(result.location, baseURL);
  return url.pathname === "/sign-in" ? (url.searchParams.get("error") ?? undefined) : undefined;
}

async function userRows(email: string) {
  return db.select().from(user).where(eq(user.email, email));
}

async function membershipOf(userId: string) {
  return db
    .select({
      id: member.id,
      role: member.role,
      organizationId: member.organizationId,
      updatedAt: member.updatedAt,
      slug: organization.slug,
      providerId: organization.providerId,
    })
    .from(member)
    .innerJoin(organization, eq(organization.id, member.organizationId))
    .where(eq(member.userId, userId));
}

/** The user id each session.create.before saw, so a rolled-back user can still be looked up. */
function watchSessionCreation() {
  const hooks = auth.options.databaseHooks?.session?.create;
  if (!hooks?.before) throw new Error("session.create.before is not configured");
  const before = vi.spyOn(hooks, "before");
  onTestFinished(() => before.mockRestore());
  return () => before.mock.calls.map(([s]) => s.userId);
}

async function expectNoRows(email: string, userIds: string[]) {
  expect(await userRows(email)).toEqual([]);
  // The mock IdP's subject is the login name, i.e. the email.
  expect(await db.select().from(account).where(eq(account.accountId, email))).toEqual([]);
  if (userIds.length) {
    expect(await db.select().from(user).where(inArray(user.id, userIds))).toEqual([]);
    expect(await db.select().from(session).where(inArray(session.userId, userIds))).toEqual([]);
  }
}

beforeAll(async () => {
  await seedDevProviders(db);
});

afterAll(async () => {
  // Members, accounts and sessions cascade with their user.
  if (emails.length) await db.delete(user).where(inArray(user.email, emails));
  if (slugs.length) await db.delete(organization).where(inArray(organization.slug, slugs));
  await db.$client.end();
});

describe("first sign-in", () => {
  it("creates the organization owned by the provider, the user and the membership", async () => {
    const email = newEmail();
    const slug = newSlug();
    const result = await signIn(email, { org: slug });
    expect(result.status).toBe(302);
    expect(result.location).toBe("/");

    const [created] = await userRows(email);
    expect(created).toBeDefined();
    expect(await membershipOf(created?.id ?? "")).toEqual([
      expect.objectContaining({ slug, providerId: "corp", role: "member" }),
    ]);

    const current = await auth.api.getSession({ headers: new Headers({ cookie: result.cookie }) });
    expect(current?.user.id).toBe(created?.id);
  });

  it("stores the access token encrypted", async () => {
    const email = newEmail();
    expect((await signIn(email, { org: newSlug() })).location).toBe("/");
    const [created] = await userRows(email);
    const [row] = await db
      .select({ accessToken: account.accessToken })
      .from(account)
      .where(eq(account.userId, created?.id ?? ""));
    expect(row?.accessToken).toBeTruthy();
    expect(row?.accessToken).not.toMatch(/^ey[\w-]+\.[\w-]+\.[\w-]+$/);
  });
});

describe("roles", () => {
  it("makes an admin from the ID token's groups and demotes without them", async () => {
    const email = newEmail();
    const slug = newSlug();
    expect((await signIn(email, { org: slug, groups: ["agenty-admins"] })).location).toBe("/");
    const [created] = await userRows(email);
    expect((await membershipOf(created?.id ?? ""))[0]?.role).toBe("admin");

    expect((await signIn(email, { org: slug })).location).toBe("/");
    expect((await membershipOf(created?.id ?? ""))[0]?.role).toBe("member");
  });
});

describe("account binding", () => {
  it("maps a mixed-case email at the IdP to the existing user", async () => {
    const email = newEmail();
    const slug = newSlug();
    expect((await signIn(email, { org: slug })).location).toBe("/");
    const [first] = await userRows(email);

    const mixed = email.replace(/^t-/, "T-").replace("@corp.test", "@CORP.Test");
    // Same IdP account (subject), whose email now comes back in another case.
    const result = await signIn(mixed, { org: slug, sub: email }, mixed);
    expect(result.location).toBe("/");
    expect(await userRows(email)).toEqual([expect.objectContaining({ id: first?.id })]);
    expect(
      await db
        .select()
        .from(user)
        .where(inArray(user.email, [email, mixed])),
    ).toHaveLength(1);
  });

  it("refuses a user bound to another provider and adds no account", async () => {
    // A second provider for a subdomain of corp.test: corp also accepts that subdomain, so the
    // same email can reach both providers.
    const rand = random();
    const providerId = `sub-${rand}`;
    const subDomain = `sub-${rand}.corp.test`;
    await seedDevProviders(db, [
      { providerId, issuer: `http://localhost:8080/${rand}`, domain: subDomain },
    ]);
    // Its users are deleted in afterAll; no organization or account references the row.
    onTestFinished(async () => {
      await db.delete(ssoProvider).where(eq(ssoProvider.providerId, providerId));
    });

    // Bound to corp: the sign-in starts with a corp.test address (selection prefers the exact
    // domain, which would pick the new provider), and the IdP returns the subdomain address.
    const email = newEmail(subDomain, "u");
    const slug = newSlug();
    expect((await signIn(newEmail(), { org: slug }, email)).location).toBe("/");
    const [created] = await userRows(email);
    expect(created).toBeDefined();
    const userId = created?.id ?? "";
    const membership = await membershipOf(userId);
    expect(membership).toEqual([expect.objectContaining({ slug, providerId: "corp" })]);

    const result = await signIn(email, { org: slug });
    expect(errorOf(result)).toBe("account_bound_to_other_provider");
    expect(
      await db
        .select({ providerId: account.providerId })
        .from(account)
        .where(eq(account.userId, userId)),
    ).toEqual([{ providerId: "corp" }]);
    expect(await db.select().from(account).where(eq(account.providerId, providerId))).toEqual([]);
    expect(await membershipOf(userId)).toEqual(membership);
  });

  it("refuses a changed organization and leaves the membership as it was", async () => {
    const email = newEmail();
    expect((await signIn(email, { org: newSlug() })).location).toBe("/");
    const [created] = await userRows(email);
    const before = await membershipOf(created?.id ?? "");

    const result = await signIn(email, { org: newSlug() });
    expect(errorOf(result)).toBe("organization_changed");
    expect(await membershipOf(created?.id ?? "")).toEqual(before);
  });
});

describe("rejected sign-ins leave no rows", () => {
  it("without an organization claim", async () => {
    const userIds = watchSessionCreation();
    const email = newEmail();
    const result = await signIn(email, {});
    expect(errorOf(result)).toBe("organization_claim_missing");
    await expectNoRows(email, userIds());
  });

  it("with an IdP email outside the provider's domains", async () => {
    const email = newEmail("partner.test", "x");
    const idpEmail = newEmail("corp.test", "x");
    const result = await signIn(email, { org: newSlug() }, idpEmail);
    expect(errorOf(result)).toBe("email_domain_mismatch");
    await expectNoRows(email, []);
    await expectNoRows(idpEmail, []);
  });

  it("when session.create.before fails: a new partner user claiming a corp organization", async () => {
    const slug = newSlug();
    expect((await signIn(newEmail(), { org: slug })).location).toBe("/");

    const userIds = watchSessionCreation();
    const email = newEmail("partner.test", "n");
    const result = await signIn(email, { org: slug });
    expect(errorOf(result)).toBe("organization_owned_by_other_provider");
    // The hook ran, i.e. user and account had been written in the transaction before it threw.
    expect(userIds()).toHaveLength(1);
    await expectNoRows(email, userIds());
  });
});

describe("hand-over", () => {
  it("fails closed when no decision was handed over", async () => {
    const before = auth.options.databaseHooks?.session?.create?.before;
    if (!before) throw new Error("session.create.before is not configured");
    const fakeSession = {
      id: "s",
      userId: "u",
      token: "t",
      expiresAt: new Date(),
      createdAt: new Date(),
      updatedAt: new Date(),
    };
    for (const ctx of [{}, null]) {
      const error = await before(fakeSession, ctx as never).catch((e: unknown) => e);
      expect(error).toBeInstanceOf(APIError);
      expect((error as APIError).body?.code).toBe("provisioning_missing");
    }
  });

  it("completes a callback that arrives at a freshly started instance", async () => {
    const email = newEmail();
    const started = await signInViaMock(auth, {
      baseURL,
      email,
      claims: { org: newSlug() },
      stopAfterIdp: true,
    });
    const restarted = createAuth({ db, baseURL });
    const result = await finishCallback(restarted, started.callbackUrl ?? "", started.cookie);
    expect(result.location).toBe("/");
    expect(await userRows(email)).toHaveLength(1);
  });
});
