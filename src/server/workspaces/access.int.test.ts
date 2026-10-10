import { randomUUID } from "node:crypto";
import { inArray } from "drizzle-orm";
import { afterAll, beforeEach, describe, expect, it, vi } from "vitest";
import { type AuthConfig, createAuth } from "@/server/auth/auth";
import { user } from "@/server/db/auth-schema";
import { getDb } from "@/server/db/client";
import { getEnv } from "@/server/env";
import { signInViaMock } from "../../../tests/support/oidc";
import { testUsers } from "../../../tests/support/users";
import { getWorkspaceAccess, requireWorkspaceAdmin, requireWorkspaceMember } from "./access";
import { acceptInvitation, inviteUser, listInvitationsForUser } from "./invitations";
import { createWorkspace, ensurePersonalWorkspace } from "./workspaces";

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
const auth = createAuth(config);
const users = testUsers();
const signedInEmails: string[] = [];

afterAll(async () => {
  await users.cleanup();
  if (signedInEmails.length > 0) {
    await getDb().delete(user).where(inArray(user.email, signedInEmails));
  }
});
beforeEach(() => {
  request.cookie = "";
});

/** Signs a fresh user in through the mock IdP and makes their session the request's. */
async function signIn() {
  const id = randomUUID();
  const email = `a-${id}@example.test`;
  signedInEmails.push(email);
  const result = await signInViaMock(auth, { baseURL: config.baseURL, sub: `sub-${id}`, email });
  request.cookie = result.cookie;
  const [row] = await getDb()
    .select({ id: user.id })
    .from(user)
    .where(inArray(user.email, [email]));
  if (!row) throw new Error("signed-in user missing");
  return row.id;
}

const notFoundDigest = { digest: expect.stringMatching(/^NEXT_HTTP_ERROR_FALLBACK;404/) };

describe("workspace access", () => {
  it("gives members their role", async () => {
    const userId = await signIn();
    const personal = await ensurePersonalWorkspace(userId);

    expect(await getWorkspaceAccess(personal)).toEqual({
      workspace: { id: personal, name: "Personal", personal: true },
      role: "admin",
    });
    expect(await requireWorkspaceMember(personal)).toMatchObject({
      role: "admin",
      user: { id: userId },
    });
    expect(await requireWorkspaceAdmin(personal)).toMatchObject({ role: "admin" });
  });

  it("shows non-members the not-found page, also for malformed ids", async () => {
    await signIn();
    const owner = await users.create();
    const foreign = await createWorkspace(owner.id, "Foreign");

    expect(await getWorkspaceAccess(foreign)).toBeNull();
    await expect(requireWorkspaceMember(foreign)).rejects.toMatchObject(notFoundDigest);
    await expect(requireWorkspaceMember("not-a-uuid")).rejects.toMatchObject(notFoundDigest);
  });

  it("shows members the not-found page for admin-only pages", async () => {
    const userId = await signIn();
    const owner = await users.create();
    const team = await createWorkspace(owner.id, "Team");
    await inviteUser(owner.id, team, userId);
    const [invitation] = await listInvitationsForUser(userId);
    if (!invitation) throw new Error("no invitation");
    await acceptInvitation(userId, invitation.id);

    expect(await requireWorkspaceMember(team)).toMatchObject({ role: "member" });
    await expect(requireWorkspaceAdmin(team)).rejects.toMatchObject(notFoundDigest);
  });

  it("sends signed-out visitors to the sign-in page", async () => {
    await expect(requireWorkspaceMember(randomUUID())).rejects.toMatchObject({
      digest: expect.stringMatching(/^NEXT_REDIRECT;[a-z]+;\/sign-in;/),
    });
    expect(await getWorkspaceAccess(randomUUID())).toBeNull();
  });
});
