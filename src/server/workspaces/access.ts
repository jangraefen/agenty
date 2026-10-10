import "server-only";
import { notFound } from "next/navigation";
import { cache } from "react";
import { type CurrentUser, getCurrentUser, requireUser } from "@/server/auth/session";
import { getMembership, type WorkspaceAccess } from "./internal";

export type { WorkspaceAccess } from "./internal";

/**
 * The signed-in user's access to a workspace, or null (signed out, not a member, malformed id).
 * Looked up once per request and workspace. Every page under /w/[workspaceId] and every later
 * workspace-owned read goes through this or the require* helpers below.
 */
export const getWorkspaceAccess = cache(
  async (workspaceId: string): Promise<WorkspaceAccess | null> => {
    const user = await getCurrentUser();
    return user ? getMembership(user.id, workspaceId) : null;
  },
);

/** Members only; everyone else sees the not-found page (signed-out visitors go to sign-in). */
export async function requireWorkspaceMember(
  workspaceId: string,
): Promise<WorkspaceAccess & { user: CurrentUser }> {
  const user = await requireUser();
  const access = await getWorkspaceAccess(workspaceId);
  if (!access) notFound();
  return { ...access, user };
}

/** Admins only; members and non-members see the not-found page. */
export async function requireWorkspaceAdmin(
  workspaceId: string,
): Promise<WorkspaceAccess & { user: CurrentUser }> {
  const access = await requireWorkspaceMember(workspaceId);
  if (access.role !== "admin") notFound();
  return access;
}
