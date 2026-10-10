import { eq } from "drizzle-orm";
import { afterAll, describe, expect, it } from "vitest";
import { getDb } from "@/server/db/client";
import { workspaceInvitation } from "@/server/db/schema";
import { testUsers } from "../../../tests/support/users";
import { WorkspaceError } from "./errors";
import { getMembership } from "./internal";
import {
  acceptInvitation,
  cancelInvitation,
  countInvitationsForUser,
  declineInvitation,
  inviteUser,
  listInvitationsForUser,
  listInvitationsForWorkspace,
  searchUsers,
} from "./invitations";
import {
  changeRole,
  createWorkspace,
  deleteWorkspace,
  ensurePersonalWorkspace,
  removeMember,
} from "./workspaces";

const users = testUsers();
const db = getDb();
afterAll(() => users.cleanup());

const rejectsWith = (promise: Promise<unknown>, code: string) =>
  expect(promise).rejects.toSatisfy((e) => e instanceof WorkspaceError && e.code === code);

async function setup() {
  const admin = await users.create("Admin");
  const invitee = await users.create("Invitee");
  const id = await createWorkspace(admin.id, "Team");
  return { id, admin, invitee };
}

const invitationIdFor = async (userId: string) => {
  const [first] = await listInvitationsForUser(userId);
  if (!first) throw new Error("no invitation");
  return first.id;
};

describe("R6: inviting existing users", () => {
  it("finds users by name or email, marks members and invitees, and returns at most 10", async () => {
    const { id, admin, invitee } = await setup();
    const tag = invitee.id.slice(0, 8);
    await Promise.all(Array.from({ length: 11 }, (_, i) => users.create(`Bulk ${tag} ${i}`)));

    expect(await searchUsers(admin.id, id, invitee.email)).toEqual([
      { id: invitee.id, name: "Invitee", email: invitee.email, status: null },
    ]);
    expect(await searchUsers(admin.id, id, admin.email)).toEqual([
      { id: admin.id, name: "Admin", email: admin.email, status: "member" },
    ]);
    const bulk = await searchUsers(admin.id, id, `Bulk ${tag}`);
    expect(bulk).toHaveLength(10);
    expect(bulk.map((found) => found.name)).toEqual(
      Array.from({ length: 11 }, (_, i) => `Bulk ${tag} ${i}`)
        .sort()
        .slice(0, 10),
    );

    await inviteUser(admin.id, id, invitee.id);
    expect(await searchUsers(admin.id, id, invitee.email)).toEqual([
      { id: invitee.id, name: "Invitee", email: invitee.email, status: "invited" },
    ]);
    await acceptInvitation(invitee.id, await invitationIdFor(invitee.id));
    expect(await searchUsers(admin.id, id, invitee.email)).toEqual([
      { id: invitee.id, name: "Invitee", email: invitee.email, status: "member" },
    ]);
  });

  it("marks status per workspace", async () => {
    const { id, admin, invitee } = await setup();
    const other = await createWorkspace(admin.id, "Other");
    await inviteUser(admin.id, other, invitee.id);
    expect(await searchUsers(admin.id, id, invitee.email)).toEqual([
      { id: invitee.id, name: "Invitee", email: invitee.email, status: null },
    ]);
  });

  it("matches wildcards literally", async () => {
    const { id, admin } = await setup();
    const odd = await users.create(`100%_${admin.id.slice(0, 8)}`);
    expect(await searchUsers(admin.id, id, `100%_${admin.id.slice(0, 8)}`)).toEqual([
      { id: odd.id, name: odd.name, email: odd.email, status: null },
    ]);
    expect(await searchUsers(admin.id, id, "%%")).toEqual([]);
    const tag = admin.id.slice(0, 8);
    await users.create(`abXcd ${tag}`);
    expect(await searchUsers(admin.id, id, `ab_cd ${tag}`)).toEqual([]);
  });

  it("is only for admins of shared workspaces", async () => {
    const { id, admin, invitee } = await setup();
    await inviteUser(admin.id, id, invitee.id);
    await acceptInvitation(invitee.id, await invitationIdFor(invitee.id));
    await rejectsWith(searchUsers(invitee.id, id, "Admin"), "forbidden");

    const personal = await ensurePersonalWorkspace(admin.id);
    await rejectsWith(searchUsers(admin.id, personal, "Invitee"), "personal_workspace");
    await rejectsWith(inviteUser(admin.id, personal, invitee.id), "personal_workspace");
  });

  it("rejects short queries and unknown users", async () => {
    const { id, admin } = await setup();
    await rejectsWith(searchUsers(admin.id, id, " a "), "invalid_query");
    await rejectsWith(
      inviteUser(admin.id, id, "6f1c1f7e-3d4b-4c55-9a43-1b2a5c6d7e8f"),
      "user_not_found",
    );
    await rejectsWith(inviteUser(admin.id, id, "nope"), "user_not_found");
  });

  it("rejects duplicates and members", async () => {
    const { id, admin, invitee } = await setup();
    await inviteUser(admin.id, id, invitee.id);
    await rejectsWith(inviteUser(admin.id, id, invitee.id), "already_invited");
    await rejectsWith(inviteUser(admin.id, id, admin.id), "already_member");
  });
});

describe("R4/R5: who may invite", () => {
  it("lets members neither invite nor cancel", async () => {
    const { id, admin, invitee } = await setup();
    const member = await users.create();
    const other = await users.create();
    await inviteUser(admin.id, id, member.id);
    await acceptInvitation(member.id, await invitationIdFor(member.id));
    await inviteUser(admin.id, id, invitee.id);
    const invitationId = await invitationIdFor(invitee.id);

    await rejectsWith(inviteUser(member.id, id, other.id), "forbidden");
    await rejectsWith(cancelInvitation(member.id, id, invitationId), "forbidden");
  });

  it("answers not_found to non-members", async () => {
    const { id, admin, invitee } = await setup();
    const stranger = await users.create();
    await inviteUser(admin.id, id, invitee.id);
    const invitationId = await invitationIdFor(invitee.id);

    await rejectsWith(searchUsers(stranger.id, id, "Invitee"), "not_found");
    await rejectsWith(inviteUser(stranger.id, id, invitee.id), "not_found");
    await rejectsWith(cancelInvitation(stranger.id, id, invitationId), "not_found");
  });

  it("stops a demoted admin from inviting", async () => {
    const { id, admin, invitee } = await setup();
    const second = await users.create();
    await inviteUser(admin.id, id, second.id);
    await acceptInvitation(second.id, await invitationIdFor(second.id));
    await changeRole(admin.id, id, second.id, "admin");
    await changeRole(admin.id, id, second.id, "member");

    await rejectsWith(inviteUser(second.id, id, invitee.id), "forbidden");
  });
});

describe("workspace scoping", () => {
  it("ignores targets from another workspace", async () => {
    const adminA = await users.create("Admin A");
    const adminB = await users.create("Admin B");
    const memberB = await users.create("Member B");
    const invitee = await users.create("Invitee");
    const a = await createWorkspace(adminA.id, "Team A");
    const b = await createWorkspace(adminB.id, "Team B");
    await inviteUser(adminB.id, b, memberB.id);
    await acceptInvitation(memberB.id, await invitationIdFor(memberB.id));
    await inviteUser(adminB.id, b, invitee.id);
    const invitationOfB = await invitationIdFor(invitee.id);

    await rejectsWith(cancelInvitation(adminA.id, a, invitationOfB), "not_found");
    expect(await db.$count(workspaceInvitation, eq(workspaceInvitation.id, invitationOfB))).toBe(1);
    await rejectsWith(changeRole(adminA.id, a, memberB.id, "admin"), "not_found");
    expect((await getMembership(memberB.id, b))?.role).toBe("member");
    await rejectsWith(removeMember(adminA.id, a, memberB.id), "not_found");
    expect(await getMembership(memberB.id, b)).not.toBeNull();
  });
});

describe("R7: answering invitations", () => {
  it("makes the invitee a member and removes the invitation", async () => {
    const { id, admin, invitee } = await setup();
    await inviteUser(admin.id, id, invitee.id);
    expect(await listInvitationsForUser(invitee.id)).toEqual([
      { id: expect.any(String), workspaceId: id, workspaceName: "Team", invitedBy: "Admin" },
    ]);

    expect(await countInvitationsForUser(invitee.id)).toBe(1);
    expect(await acceptInvitation(invitee.id, await invitationIdFor(invitee.id))).toBe(id);
    expect((await getMembership(invitee.id, id))?.role).toBe("member");
    expect(await listInvitationsForUser(invitee.id)).toEqual([]);
    expect(await countInvitationsForUser(invitee.id)).toBe(0);
  });

  it("can only be answered by the invitee", async () => {
    const { id, admin, invitee } = await setup();
    await inviteUser(admin.id, id, invitee.id);
    const invitationId = await invitationIdFor(invitee.id);
    await rejectsWith(acceptInvitation(admin.id, invitationId), "not_found");
    await rejectsWith(declineInvitation(admin.id, invitationId), "not_found");
  });

  it("declining deletes the invitation, so the user can be invited again", async () => {
    const { id, admin, invitee } = await setup();
    await inviteUser(admin.id, id, invitee.id);
    await declineInvitation(invitee.id, await invitationIdFor(invitee.id));
    expect(await listInvitationsForWorkspace(admin.id, id)).toEqual([]);
    await inviteUser(admin.id, id, invitee.id);
  });

  it("accepting a cancelled invitation fails without a membership", async () => {
    const { id, admin, invitee } = await setup();
    await inviteUser(admin.id, id, invitee.id);
    const invitationId = await invitationIdFor(invitee.id);
    await cancelInvitation(admin.id, id, invitationId);

    await rejectsWith(acceptInvitation(invitee.id, invitationId), "not_found");
    expect(await getMembership(invitee.id, id)).toBeNull();
  });

  it("lists a workspace's invitations to admins only", async () => {
    const { id, admin, invitee } = await setup();
    await inviteUser(admin.id, id, invitee.id);
    expect(await listInvitationsForWorkspace(admin.id, id)).toEqual([
      { id: expect.any(String), userId: invitee.id, name: "Invitee", email: invitee.email },
    ]);
    const stranger = await users.create();
    await rejectsWith(listInvitationsForWorkspace(stranger.id, id), "not_found");
  });
});

describe("R8: deleting a workspace", () => {
  it("removes its invitations", async () => {
    const { id, admin, invitee } = await setup();
    await inviteUser(admin.id, id, invitee.id);
    await deleteWorkspace(admin.id, id, "Team");
    expect(await db.$count(workspaceInvitation, eq(workspaceInvitation.workspaceId, id))).toBe(0);
  });
});
