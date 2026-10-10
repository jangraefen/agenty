import "server-only";
import { and, asc, eq, ilike, or } from "drizzle-orm";
import { alias } from "drizzle-orm/pg-core";
import { user } from "@/server/db/auth-schema";
import { getDb } from "@/server/db/client";
import { workspace, workspaceInvitation, workspaceMember } from "@/server/db/schema";
import { WorkspaceError } from "./errors";
import { lockForAdmin, parseId, requireMembership } from "./internal";
import { likePattern, userSearchQuerySchema } from "./validation";

const SEARCH_LIMIT = 10;

/** A user's relation to the workspace being searched from: member, invited, or neither (null). */
export type UserSearchStatus = "member" | "invited" | null;
export type UserSearchResult = {
  id: string;
  name: string;
  email: string;
  status: UserSearchStatus;
};

/**
 * R6: admins of shared workspaces search all users by name or email. Members and invitees are
 * included with their status, so the UI can show them without an Invite button.
 */
export async function searchUsers(
  actorId: string,
  workspaceId: string,
  query: string,
): Promise<UserSearchResult[]> {
  const parsed = userSearchQuerySchema.safeParse(query);
  if (!parsed.success) throw new WorkspaceError("invalid_query");
  const { workspace: ws, role } = await requireMembership(actorId, workspaceId);
  if (role !== "admin") throw new WorkspaceError("forbidden");
  if (ws.personal) throw new WorkspaceError("personal_workspace");

  const pattern = likePattern(parsed.data);
  const rows = await getDb()
    .select({
      id: user.id,
      name: user.name,
      email: user.email,
      memberId: workspaceMember.userId,
      invitationId: workspaceInvitation.id,
    })
    .from(user)
    .leftJoin(
      workspaceMember,
      and(eq(workspaceMember.workspaceId, ws.id), eq(workspaceMember.userId, user.id)),
    )
    .leftJoin(
      workspaceInvitation,
      and(eq(workspaceInvitation.workspaceId, ws.id), eq(workspaceInvitation.userId, user.id)),
    )
    .where(or(ilike(user.name, pattern), ilike(user.email, pattern)))
    .orderBy(asc(user.name), asc(user.email))
    .limit(SEARCH_LIMIT);
  return rows.map(({ memberId, invitationId, ...found }) => ({
    ...found,
    status: memberId ? "member" : invitationId ? "invited" : null,
  }));
}

export async function inviteUser(
  actorId: string,
  workspaceId: string,
  inviteeId: string,
): Promise<void> {
  const targetId = parseId(inviteeId, "user_not_found");
  await getDb().transaction(async (tx) => {
    const { workspace: ws } = await lockForAdmin(tx, workspaceId, actorId);
    if (ws.personal) throw new WorkspaceError("personal_workspace");
    const [invitee] = await tx.select({ id: user.id }).from(user).where(eq(user.id, targetId));
    if (!invitee) throw new WorkspaceError("user_not_found");
    const [member] = await tx
      .select({ userId: workspaceMember.userId })
      .from(workspaceMember)
      .where(and(eq(workspaceMember.workspaceId, ws.id), eq(workspaceMember.userId, targetId)));
    if (member) throw new WorkspaceError("already_member");
    const [created] = await tx
      .insert(workspaceInvitation)
      .values({ workspaceId: ws.id, userId: targetId, invitedByUserId: actorId })
      .onConflictDoNothing()
      .returning({ id: workspaceInvitation.id });
    if (!created) throw new WorkspaceError("already_invited");
  });
}

/** Any admin may cancel any invitation of the workspace. */
export async function cancelInvitation(
  actorId: string,
  workspaceId: string,
  invitationId: string,
): Promise<void> {
  const id = parseId(invitationId);
  await getDb().transaction(async (tx) => {
    const { workspace: ws } = await lockForAdmin(tx, workspaceId, actorId);
    const deleted = await tx
      .delete(workspaceInvitation)
      .where(and(eq(workspaceInvitation.id, id), eq(workspaceInvitation.workspaceId, ws.id)))
      .returning({ id: workspaceInvitation.id });
    if (deleted.length === 0) throw new WorkspaceError("not_found");
  });
}

export async function listInvitationsForWorkspace(
  actorId: string,
  workspaceId: string,
): Promise<{ id: string; userId: string; name: string; email: string }[]> {
  const { workspace: ws, role } = await requireMembership(actorId, workspaceId);
  if (role !== "admin") throw new WorkspaceError("forbidden");
  return getDb()
    .select({
      id: workspaceInvitation.id,
      userId: workspaceInvitation.userId,
      name: user.name,
      email: user.email,
    })
    .from(workspaceInvitation)
    .innerJoin(user, eq(user.id, workspaceInvitation.userId))
    .where(eq(workspaceInvitation.workspaceId, ws.id))
    .orderBy(asc(user.name), asc(user.email));
}

/** Number of pending invitations to the user (the header's badge). */
export async function countInvitationsForUser(userId: string): Promise<number> {
  return getDb().$count(workspaceInvitation, eq(workspaceInvitation.userId, userId));
}

const inviter = alias(user, "inviter");

export async function listInvitationsForUser(
  userId: string,
): Promise<{ id: string; workspaceId: string; workspaceName: string; invitedBy: string | null }[]> {
  const rows = await getDb()
    .select({
      id: workspaceInvitation.id,
      workspaceId: workspace.id,
      workspaceName: workspace.name,
      inviterName: inviter.name,
      inviterEmail: inviter.email,
    })
    .from(workspaceInvitation)
    .innerJoin(workspace, eq(workspace.id, workspaceInvitation.workspaceId))
    .leftJoin(inviter, eq(inviter.id, workspaceInvitation.invitedByUserId))
    .where(eq(workspaceInvitation.userId, userId))
    .orderBy(asc(workspace.name), asc(workspaceInvitation.id));
  return rows.map(({ inviterName, inviterEmail, ...row }) => ({
    ...row,
    invitedBy: inviterName || inviterEmail || null,
  }));
}

/**
 * R7: the invitee joins as member. The workspace is locked before the invitation is read again,
 * in the same order as every other operation, so a concurrent cancel either wins (not_found) or
 * waits.
 */
export async function acceptInvitation(actorId: string, invitationId: string): Promise<string> {
  const id = parseId(invitationId);
  const db = getDb();
  const [found] = await db
    .select({ workspaceId: workspaceInvitation.workspaceId })
    .from(workspaceInvitation)
    .where(and(eq(workspaceInvitation.id, id), eq(workspaceInvitation.userId, actorId)));
  if (!found) throw new WorkspaceError("not_found");

  return db.transaction(async (tx) => {
    await tx
      .select({ id: workspace.id })
      .from(workspace)
      .where(eq(workspace.id, found.workspaceId))
      .for("update");
    const deleted = await tx
      .delete(workspaceInvitation)
      .where(and(eq(workspaceInvitation.id, id), eq(workspaceInvitation.userId, actorId)))
      .returning({ workspaceId: workspaceInvitation.workspaceId });
    const accepted = deleted[0];
    if (!accepted) throw new WorkspaceError("not_found");
    await tx
      .insert(workspaceMember)
      .values({ workspaceId: accepted.workspaceId, userId: actorId, role: "member" })
      .onConflictDoNothing();
    return accepted.workspaceId;
  });
}

/** A single delete of the user's own invitation; needs no lock. */
export async function declineInvitation(actorId: string, invitationId: string): Promise<void> {
  const id = parseId(invitationId);
  const deleted = await getDb()
    .delete(workspaceInvitation)
    .where(and(eq(workspaceInvitation.id, id), eq(workspaceInvitation.userId, actorId)))
    .returning({ id: workspaceInvitation.id });
  if (deleted.length === 0) throw new WorkspaceError("not_found");
}
