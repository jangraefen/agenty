import "server-only";
import { and, eq } from "drizzle-orm";
import { getDb } from "@/server/db/client";
import { type WorkspaceRole, workspace, workspaceMember } from "@/server/db/schema";
import { WorkspaceError, type WorkspaceErrorCode } from "./errors";
import { workspaceIdSchema } from "./validation";

export type Tx = Parameters<Parameters<ReturnType<typeof getDb>["transaction"]>[0]>[0];
export type WorkspaceSummary = { id: string; name: string; personal: boolean };
export type WorkspaceAccess = { workspace: WorkspaceSummary; role: WorkspaceRole };

/** A uuid from request input; anything else is `code` (default not_found), before any query. */
export function parseId(value: unknown, code: WorkspaceErrorCode = "not_found"): string {
  const parsed = workspaceIdSchema.safeParse(value);
  if (!parsed.success) throw new WorkspaceError(code);
  return parsed.data;
}

const summaryColumns = {
  id: workspace.id,
  name: workspace.name,
  personalUserId: workspace.personalUserId,
};

function toSummary(row: { id: string; name: string; personalUserId: string | null }) {
  return { id: row.id, name: row.name, personal: row.personalUserId !== null };
}

/**
 * Locks the workspace row, then reads the actor's role. Every mutating operation starts here, so
 * concurrent changes to one workspace run one after another and see each other's results.
 */
export async function lockForActor(
  tx: Tx,
  workspaceId: unknown,
  actorId: string,
): Promise<WorkspaceAccess> {
  const id = parseId(workspaceId);
  const [row] = await tx
    .select(summaryColumns)
    .from(workspace)
    .where(eq(workspace.id, id))
    .for("update");
  if (!row) throw new WorkspaceError("not_found");
  const [member] = await tx
    .select({ role: workspaceMember.role })
    .from(workspaceMember)
    .where(and(eq(workspaceMember.workspaceId, id), eq(workspaceMember.userId, actorId)));
  if (!member) throw new WorkspaceError("not_found");
  return { workspace: toSummary(row), role: member.role };
}

export async function lockForAdmin(
  tx: Tx,
  workspaceId: unknown,
  actorId: string,
): Promise<WorkspaceAccess> {
  const access = await lockForActor(tx, workspaceId, actorId);
  if (access.role !== "admin") throw new WorkspaceError("forbidden");
  return access;
}

/** The user's membership, or null for non-members and malformed ids. For reads; no lock. */
export async function getMembership(
  userId: string,
  workspaceId: unknown,
): Promise<WorkspaceAccess | null> {
  const parsed = workspaceIdSchema.safeParse(workspaceId);
  if (!parsed.success) return null;
  const [row] = await getDb()
    .select({ ...summaryColumns, role: workspaceMember.role })
    .from(workspace)
    .innerJoin(workspaceMember, eq(workspaceMember.workspaceId, workspace.id))
    .where(and(eq(workspace.id, parsed.data), eq(workspaceMember.userId, userId)));
  return row ? { workspace: toSummary(row), role: row.role } : null;
}

export async function requireMembership(
  userId: string,
  workspaceId: unknown,
): Promise<WorkspaceAccess> {
  const access = await getMembership(userId, workspaceId);
  if (!access) throw new WorkspaceError("not_found");
  return access;
}

/** Number of admins; call only with the workspace locked. */
export async function countAdmins(tx: Tx, workspaceId: string): Promise<number> {
  return tx.$count(
    workspaceMember,
    and(eq(workspaceMember.workspaceId, workspaceId), eq(workspaceMember.role, "admin")),
  );
}
