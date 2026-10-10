"use server";

import { redirect } from "next/navigation";
import { requireUser } from "@/server/auth/session";
import { acceptInvitation, declineInvitation } from "@/server/workspaces/invitations";
import { createWorkspace } from "@/server/workspaces/workspaces";
import { field, runWorkspaceAction } from "./run-action";

const PATH = "/workspaces";

export async function createWorkspaceAction(formData: FormData): Promise<never> {
  const user = await requireUser();
  const id = await runWorkspaceAction(PATH, () =>
    createWorkspace(user.id, field(formData, "name")),
  );
  redirect(`/w/${id}`);
}

export async function acceptInvitationAction(formData: FormData): Promise<never> {
  const user = await requireUser();
  const id = await runWorkspaceAction(PATH, () =>
    acceptInvitation(user.id, field(formData, "invitationId")),
  );
  redirect(`/w/${id}`);
}

export async function declineInvitationAction(formData: FormData): Promise<never> {
  const user = await requireUser();
  await runWorkspaceAction(PATH, () => declineInvitation(user.id, field(formData, "invitationId")));
  redirect(PATH);
}
