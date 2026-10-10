"use server";

import { redirect } from "next/navigation";
import { field, runWorkspaceAction } from "@/app/(signed-in)/workspaces/run-action";
import { requireUser } from "@/server/auth/session";
import { cancelInvitation, inviteUser } from "@/server/workspaces/invitations";
import {
  changeRole,
  deleteWorkspace,
  leaveWorkspace,
  removeMember,
  renameWorkspace,
} from "@/server/workspaces/workspaces";

// workspaceId is bound in the page but still client input: the service checks membership.
function settingsPath(workspaceId: string, q = "") {
  const path = `/w/${encodeURIComponent(String(workspaceId))}/settings`;
  return q ? `${path}?q=${encodeURIComponent(q)}` : path;
}

export async function renameWorkspaceAction(
  workspaceId: string,
  formData: FormData,
): Promise<never> {
  const user = await requireUser();
  const path = settingsPath(workspaceId);
  await runWorkspaceAction(path, () =>
    renameWorkspace(user.id, workspaceId, field(formData, "name")),
  );
  redirect(path);
}

export async function inviteUserAction(workspaceId: string, formData: FormData): Promise<never> {
  const user = await requireUser();
  const path = settingsPath(workspaceId, field(formData, "q"));
  await runWorkspaceAction(path, () => inviteUser(user.id, workspaceId, field(formData, "userId")));
  redirect(path);
}

export async function cancelInvitationAction(
  workspaceId: string,
  formData: FormData,
): Promise<never> {
  const user = await requireUser();
  const path = settingsPath(workspaceId, field(formData, "q"));
  await runWorkspaceAction(path, () =>
    cancelInvitation(user.id, workspaceId, field(formData, "invitationId")),
  );
  redirect(path);
}

export async function changeRoleAction(workspaceId: string, formData: FormData): Promise<never> {
  const user = await requireUser();
  const path = settingsPath(workspaceId);
  await runWorkspaceAction(path, () =>
    changeRole(user.id, workspaceId, field(formData, "userId"), field(formData, "role")),
  );
  redirect(path);
}

export async function removeMemberAction(workspaceId: string, formData: FormData): Promise<never> {
  const user = await requireUser();
  const path = settingsPath(workspaceId);
  await runWorkspaceAction(path, () =>
    removeMember(user.id, workspaceId, field(formData, "userId")),
  );
  redirect(path);
}

export async function leaveWorkspaceAction(workspaceId: string): Promise<never> {
  const user = await requireUser();
  await runWorkspaceAction(settingsPath(workspaceId), () => leaveWorkspace(user.id, workspaceId));
  redirect("/workspaces");
}

export async function deleteWorkspaceAction(
  workspaceId: string,
  formData: FormData,
): Promise<never> {
  const user = await requireUser();
  await runWorkspaceAction(settingsPath(workspaceId), () =>
    deleteWorkspace(user.id, workspaceId, field(formData, "confirmation")),
  );
  redirect("/workspaces");
}
