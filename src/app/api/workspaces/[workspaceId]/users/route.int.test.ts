import { afterAll, beforeEach, describe, expect, it, vi } from "vitest";
import {
  acceptInvitation,
  inviteUser,
  listInvitationsForUser,
} from "@/server/workspaces/invitations";
import { createWorkspace, ensurePersonalWorkspace } from "@/server/workspaces/workspaces";
import { testUsers } from "../../../../../../tests/support/users";

const session = vi.hoisted(() => ({ user: null as { id: string } | null }));
vi.mock("@/server/auth/session", () => ({ getCurrentUser: async () => session.user }));
// Lets a test make the search fail with a code the route doesn't map.
const failure = vi.hoisted(() => ({ error: null as Error | null }));
vi.mock("@/server/workspaces/invitations", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/server/workspaces/invitations")>();
  return {
    ...actual,
    searchUsers: (...args: Parameters<typeof actual.searchUsers>) => {
      if (failure.error) throw failure.error;
      return actual.searchUsers(...args);
    },
  };
});

const { WorkspaceError } = await import("@/server/workspaces/errors");

const { GET } = await import("./route");

const users = testUsers();
afterAll(() => users.cleanup());
beforeEach(() => {
  session.user = null;
  failure.error = null;
});

async function search(workspaceId: string, q: string) {
  const url = `http://localhost/api/workspaces/${workspaceId}/users?q=${encodeURIComponent(q)}`;
  const response = await GET(new Request(url), { params: Promise.resolve({ workspaceId }) });
  return { status: response.status, body: await response.json() };
}

async function setup() {
  const admin = await users.create("Admin");
  const member = await users.create("Member");
  const id = await createWorkspace(admin.id, "Team");
  await inviteUser(admin.id, id, member.id);
  const [invitation] = await listInvitationsForUser(member.id);
  if (!invitation) throw new Error("no invitation");
  await acceptInvitation(member.id, invitation.id);
  return { id, admin, member };
}

describe("GET /api/workspaces/[workspaceId]/users", () => {
  it("answers admins with matches and their status", async () => {
    const { id, admin, member } = await setup();
    session.user = admin;
    expect(await search(id, member.email)).toEqual({
      status: 200,
      body: { users: [{ id: member.id, name: "Member", email: member.email, status: "member" }] },
    });
  });

  it("rejects signed-out requests", async () => {
    const { id, member } = await setup();
    expect(await search(id, member.email)).toEqual({
      status: 401,
      body: { error: "unauthorized" },
    });
  });

  it("answers non-members not found, without results", async () => {
    const { id, member } = await setup();
    session.user = await users.create("Stranger");
    expect(await search(id, member.email)).toEqual({ status: 404, body: { error: "not_found" } });
    expect(await search("not-a-uuid", member.email)).toEqual({
      status: 404,
      body: { error: "not_found" },
    });
  });

  it("forbids members who are not admins", async () => {
    const { id, admin, member } = await setup();
    session.user = member;
    expect(await search(id, admin.email)).toEqual({ status: 403, body: { error: "forbidden" } });
  });

  it("refuses the personal workspace and invalid queries with fixed codes", async () => {
    const { id, admin } = await setup();
    session.user = admin;
    const personal = await ensurePersonalWorkspace(admin.id);
    expect(await search(personal, "Member")).toEqual({
      status: 403,
      body: { error: "personal_workspace" },
    });
    expect(await search(id, " a ")).toEqual({ status: 400, body: { error: "invalid_query" } });
  });

  it("answers an unexpected workspace error as internal, without its code", async () => {
    const { id, admin } = await setup();
    session.user = admin;
    failure.error = new WorkspaceError("last_admin");
    const log = vi.spyOn(console, "error").mockImplementation(() => {});
    try {
      expect(await search(id, "Member")).toEqual({ status: 500, body: { error: "internal" } });
      expect(log).toHaveBeenCalledWith("User search failed:", "WorkspaceError");
    } finally {
      log.mockRestore();
    }
  });
});
