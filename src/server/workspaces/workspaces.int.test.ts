import { and, eq } from "drizzle-orm";
import { afterAll, describe, expect, it } from "vitest";
import { getDb } from "@/server/db/client";
import { workspace, workspaceMember } from "@/server/db/schema";
import { testUsers } from "../../../tests/support/users";
import { WorkspaceError } from "./errors";
import { getMembership } from "./internal";
import {
  changeRole,
  createWorkspace,
  deleteWorkspace,
  ensurePersonalWorkspace,
  leaveWorkspace,
  listMembers,
  listWorkspaces,
  removeMember,
  renameWorkspace,
} from "./workspaces";

const users = testUsers();
const db = getDb();
afterAll(() => users.cleanup());

const rejectsWith = (promise: Promise<unknown>, code: string) =>
  expect(promise).rejects.toSatisfy((e) => e instanceof WorkspaceError && e.code === code);

/** A shared workspace with an admin and a member (added directly; invitations are Task 3). */
async function sharedWorkspace() {
  const admin = await users.create("Admin");
  const member = await users.create("Member");
  const id = await createWorkspace(admin.id, "Team");
  await db.insert(workspaceMember).values({ workspaceId: id, userId: member.id, role: "member" });
  return { id, admin, member };
}

const adminCount = async (id: string) =>
  db.$count(
    workspaceMember,
    and(eq(workspaceMember.workspaceId, id), eq(workspaceMember.role, "admin")),
  );

describe("R1: personal workspace", () => {
  it("is created once with the user as admin", async () => {
    const u = await users.create();
    const first = await ensurePersonalWorkspace(u.id);
    const second = await ensurePersonalWorkspace(u.id);

    expect(second).toBe(first);
    expect(await getMembership(u.id, first)).toEqual({
      workspace: { id: first, name: "Personal", personal: true },
      role: "admin",
    });
  });

  it("is created once under concurrent calls", async () => {
    const u = await users.create();
    const ids = await Promise.all(Array.from({ length: 5 }, () => ensurePersonalWorkspace(u.id)));

    expect(new Set(ids).size).toBe(1);
    expect(await db.$count(workspace, eq(workspace.personalUserId, u.id))).toBe(1);
  });
});

describe("R2: personal workspace limits", () => {
  it("cannot be deleted or left, and its member's role cannot change", async () => {
    const u = await users.create();
    const id = await ensurePersonalWorkspace(u.id);

    await rejectsWith(deleteWorkspace(u.id, id, "Personal"), "personal_workspace");
    await rejectsWith(leaveWorkspace(u.id, id), "last_admin");
    await rejectsWith(changeRole(u.id, id, u.id, "member"), "last_admin");
    await rejectsWith(removeMember(u.id, id, u.id), "last_admin");
  });

  it("cannot be renamed", async () => {
    const u = await users.create();
    const id = await ensurePersonalWorkspace(u.id);
    await rejectsWith(renameWorkspace(u.id, id, "Mine"), "personal_workspace");
    expect((await getMembership(u.id, id))?.workspace.name).toBe("Personal");
  });
});

describe("R3/R4: creating and managing", () => {
  it("makes the creator admin of a new, non-personal workspace", async () => {
    const u = await users.create();
    const id = await createWorkspace(u.id, " Team ");
    expect(await getMembership(u.id, id)).toEqual({
      workspace: { id, name: "Team", personal: false },
      role: "admin",
    });
  });

  it("lets admins rename shared workspaces", async () => {
    const { id, admin } = await sharedWorkspace();
    await renameWorkspace(admin.id, id, "  Renamed  ");
    expect((await getMembership(admin.id, id))?.workspace.name).toBe("Renamed");
  });

  it("rejects invalid names", async () => {
    const u = await users.create();
    await rejectsWith(createWorkspace(u.id, "   "), "invalid_name");
    const id = await createWorkspace(u.id, "Team");
    await rejectsWith(renameWorkspace(u.id, id, "x".repeat(81)), "invalid_name");
  });

  it("lets members do none of the admin operations", async () => {
    const { id, admin, member } = await sharedWorkspace();
    await rejectsWith(renameWorkspace(member.id, id, "Mine"), "forbidden");
    await rejectsWith(deleteWorkspace(member.id, id, "Team"), "forbidden");
    await rejectsWith(changeRole(member.id, id, admin.id, "member"), "forbidden");
    await rejectsWith(removeMember(member.id, id, admin.id), "forbidden");
  });

  it("answers not_found to non-members, for reads too", async () => {
    const { id } = await sharedWorkspace();
    const stranger = await users.create();
    await rejectsWith(listMembers(stranger.id, id), "not_found");
    await rejectsWith(renameWorkspace(stranger.id, id, "Mine"), "not_found");
    await rejectsWith(leaveWorkspace(stranger.id, id), "not_found");
    await rejectsWith(deleteWorkspace(stranger.id, id, "Team"), "not_found");
    await rejectsWith(changeRole(stranger.id, id, stranger.id, "admin"), "not_found");
    await rejectsWith(removeMember(stranger.id, id, stranger.id), "not_found");
    expect(await getMembership(stranger.id, id)).toBeNull();
  });

  it("treats malformed ids as not found without a query error", async () => {
    const u = await users.create();
    await rejectsWith(renameWorkspace(u.id, "not-a-uuid", "Team"), "not_found");
    expect(await getMembership(u.id, "not-a-uuid")).toBeNull();
  });

  it("lets an admin promote, demote and remove other members", async () => {
    const { id, admin, member } = await sharedWorkspace();
    await changeRole(admin.id, id, member.id, "admin");
    expect((await getMembership(member.id, id))?.role).toBe("admin");
    await changeRole(member.id, id, admin.id, "member");
    await removeMember(member.id, id, admin.id);
    expect(await getMembership(admin.id, id)).toBeNull();
  });

  it("rejects unknown roles", async () => {
    const { id, admin, member } = await sharedWorkspace();
    await rejectsWith(changeRole(admin.id, id, member.id, "owner"), "invalid_role");
  });

  it("lists workspaces with role, personal first", async () => {
    const u = await users.create();
    const personal = await ensurePersonalWorkspace(u.id);
    const team = await createWorkspace(u.id, "A team");
    expect(await listWorkspaces(u.id)).toEqual([
      { id: personal, name: "Personal", personal: true, role: "admin" },
      { id: team, name: "A team", personal: false, role: "admin" },
    ]);
  });

  it("lists members to members", async () => {
    const { id, admin, member } = await sharedWorkspace();
    expect(await listMembers(member.id, id)).toEqual([
      { userId: admin.id, name: "Admin", email: admin.email, role: "admin" },
      { userId: member.id, name: "Member", email: member.email, role: "member" },
    ]);
  });
});

describe("R5: at least one admin", () => {
  it("keeps the last admin from leaving, being removed or demoted", async () => {
    const { id, admin } = await sharedWorkspace();
    await rejectsWith(leaveWorkspace(admin.id, id), "last_admin");
    await rejectsWith(removeMember(admin.id, id, admin.id), "last_admin");
    await rejectsWith(changeRole(admin.id, id, admin.id, "member"), "last_admin");
  });

  it("leaves exactly one admin when two admins demote each other at once", async () => {
    const { id, admin, member } = await sharedWorkspace();
    await changeRole(admin.id, id, member.id, "admin");

    const results = await Promise.allSettled([
      changeRole(admin.id, id, member.id, "member"),
      changeRole(member.id, id, admin.id, "member"),
    ]);

    expect(results.filter((r) => r.status === "fulfilled")).toHaveLength(1);
    expect(await adminCount(id)).toBe(1);
  });

  it("stops a demoted admin from acting on a stale page", async () => {
    const { id, admin, member } = await sharedWorkspace();
    await changeRole(admin.id, id, member.id, "admin");
    await changeRole(admin.id, id, member.id, "member");
    await rejectsWith(renameWorkspace(member.id, id, "Mine"), "forbidden");
  });

  it("lets a member leave", async () => {
    const { id, member } = await sharedWorkspace();
    await leaveWorkspace(member.id, id);
    expect(await getMembership(member.id, id)).toBeNull();
  });
});

describe("R8: deleting a workspace", () => {
  it("requires the exact name and removes the memberships", async () => {
    const { id, admin } = await sharedWorkspace();
    await rejectsWith(deleteWorkspace(admin.id, id, "team"), "confirmation_mismatch");
    await deleteWorkspace(admin.id, id, "  Team ");
    expect(await db.$count(workspace, eq(workspace.id, id))).toBe(0);
    expect(await db.$count(workspaceMember, eq(workspaceMember.workspaceId, id))).toBe(0);
  });
});
