import "server-only";
import { and, asc, desc, eq, sql } from "drizzle-orm";
import { user } from "@/server/db/auth-schema";
import { getDb } from "@/server/db/client";
import { type WorkspaceRole, workspace, workspaceMember } from "@/server/db/schema";
import { WorkspaceError } from "./errors";
import {
  countAdmins,
  lockForActor,
  lockForAdmin,
  parseId,
  requireMembership,
  type Tx,
  type WorkspaceSummary,
} from "./internal";
import { PERSONAL_WORKSPACE_NAME, workspaceNameSchema, workspaceRoleSchema } from "./validation";

function parseName(name: unknown): string {
  const parsed = workspaceNameSchema.safeParse(name);
  if (!parsed.success) throw new WorkspaceError("invalid_name");
  return parsed.data;
}

/**
 * The user's personal workspace id, creating it (with the user as admin) if missing (R1). Safe to
 * call concurrently: the unique index on personal_user_id lets one insert win.
 */
export async function ensurePersonalWorkspace(userId: string): Promise<string> {
  const db = getDb();
  const find = async () => {
    const [row] = await db
      .select({ id: workspace.id })
      .from(workspace)
      .where(eq(workspace.personalUserId, userId));
    return row?.id;
  };
  const existing = await find();
  if (existing) return existing;

  const created = await db.transaction(async (tx) => {
    const [row] = await tx
      .insert(workspace)
      .values({ name: PERSONAL_WORKSPACE_NAME, personalUserId: userId })
      .onConflictDoNothing({ target: workspace.personalUserId })
      .returning({ id: workspace.id });
    if (row)
      await tx.insert(workspaceMember).values({ workspaceId: row.id, userId, role: "admin" });
    return row?.id;
  });
  // Lost the race: another call committed the workspace (and its membership) first.
  const id = created ?? (await find());
  if (!id) throw new Error("personal workspace missing after insert");
  return id;
}

export async function createWorkspace(actorId: string, name: string): Promise<string> {
  const validName = parseName(name);
  return getDb().transaction(async (tx) => {
    const [row] = await tx
      .insert(workspace)
      .values({ name: validName })
      .returning({ id: workspace.id });
    if (!row) throw new Error("workspace insert returned nothing");
    await tx
      .insert(workspaceMember)
      .values({ workspaceId: row.id, userId: actorId, role: "admin" });
    return row.id;
  });
}

export async function renameWorkspace(
  actorId: string,
  workspaceId: string,
  name: string,
): Promise<void> {
  const validName = parseName(name);
  await getDb().transaction(async (tx) => {
    const { workspace: ws } = await lockForAdmin(tx, workspaceId, actorId);
    await tx.update(workspace).set({ name: validName }).where(eq(workspace.id, ws.id));
  });
}

/** R2, R8: not for personal workspaces; `confirmation` must equal the name after trimming. */
export async function deleteWorkspace(
  actorId: string,
  workspaceId: string,
  confirmation: string,
): Promise<void> {
  await getDb().transaction(async (tx) => {
    const { workspace: ws } = await lockForAdmin(tx, workspaceId, actorId);
    if (ws.personal) throw new WorkspaceError("personal_workspace");
    if (String(confirmation).trim() !== ws.name) throw new WorkspaceError("confirmation_mismatch");
    await tx.delete(workspace).where(eq(workspace.id, ws.id));
  });
}

export async function listWorkspaces(
  userId: string,
): Promise<(WorkspaceSummary & { role: WorkspaceRole })[]> {
  const personal = sql<boolean>`${workspace.personalUserId} is not null`;
  return getDb()
    .select({ id: workspace.id, name: workspace.name, personal, role: workspaceMember.role })
    .from(workspace)
    .innerJoin(workspaceMember, eq(workspaceMember.workspaceId, workspace.id))
    .where(eq(workspaceMember.userId, userId))
    .orderBy(desc(personal), asc(workspace.name), asc(workspace.id));
}

export async function listMembers(
  actorId: string,
  workspaceId: string,
): Promise<{ userId: string; name: string; email: string; role: WorkspaceRole }[]> {
  const { workspace: ws } = await requireMembership(actorId, workspaceId);
  return getDb()
    .select({
      userId: workspaceMember.userId,
      name: user.name,
      email: user.email,
      role: workspaceMember.role,
    })
    .from(workspaceMember)
    .innerJoin(user, eq(user.id, workspaceMember.userId))
    .where(eq(workspaceMember.workspaceId, ws.id))
    .orderBy(asc(user.name), asc(user.email));
}

/** The target's current role; not_found if they are not a member. */
async function memberRole(tx: Tx, workspaceId: string, userId: string): Promise<WorkspaceRole> {
  const [row] = await tx
    .select({ role: workspaceMember.role })
    .from(workspaceMember)
    .where(and(eq(workspaceMember.workspaceId, workspaceId), eq(workspaceMember.userId, userId)));
  if (!row) throw new WorkspaceError("not_found");
  return row.role;
}

/** R5: the last admin may not stop being admin. Call with the workspace locked. */
async function assertNotLastAdmin(tx: Tx, workspaceId: string, role: WorkspaceRole) {
  if (role === "admin" && (await countAdmins(tx, workspaceId)) <= 1) {
    throw new WorkspaceError("last_admin");
  }
}

export async function changeRole(
  actorId: string,
  workspaceId: string,
  memberUserId: string,
  role: string,
): Promise<void> {
  const parsedRole = workspaceRoleSchema.safeParse(role);
  if (!parsedRole.success) throw new WorkspaceError("invalid_role");
  const targetId = parseId(memberUserId);
  await getDb().transaction(async (tx) => {
    const { workspace: ws } = await lockForAdmin(tx, workspaceId, actorId);
    const current = await memberRole(tx, ws.id, targetId);
    if (current === parsedRole.data) return;
    await assertNotLastAdmin(tx, ws.id, current);
    await tx
      .update(workspaceMember)
      .set({ role: parsedRole.data })
      .where(and(eq(workspaceMember.workspaceId, ws.id), eq(workspaceMember.userId, targetId)));
  });
}

/** Removing oneself is leaving (allowed for members too). */
export async function removeMember(
  actorId: string,
  workspaceId: string,
  memberUserId: string,
): Promise<void> {
  if (memberUserId === actorId) return leaveWorkspace(actorId, workspaceId);
  const targetId = parseId(memberUserId);
  await getDb().transaction(async (tx) => {
    const { workspace: ws } = await lockForAdmin(tx, workspaceId, actorId);
    await assertNotLastAdmin(tx, ws.id, await memberRole(tx, ws.id, targetId));
    await tx
      .delete(workspaceMember)
      .where(and(eq(workspaceMember.workspaceId, ws.id), eq(workspaceMember.userId, targetId)));
  });
}

/** R2 needs no own check here: a personal workspace's only member is its last admin (R5). */
export async function leaveWorkspace(actorId: string, workspaceId: string): Promise<void> {
  await getDb().transaction(async (tx) => {
    const { workspace: ws, role } = await lockForActor(tx, workspaceId, actorId);
    await assertNotLastAdmin(tx, ws.id, role);
    await tx
      .delete(workspaceMember)
      .where(and(eq(workspaceMember.workspaceId, ws.id), eq(workspaceMember.userId, actorId)));
  });
}
