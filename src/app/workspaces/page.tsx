import type { Metadata } from "next";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { requireUser } from "@/server/auth/session";
import { listInvitationsForUser } from "@/server/workspaces/invitations";
import { listWorkspaces } from "@/server/workspaces/workspaces";
import { acceptInvitationAction, createWorkspaceAction, declineInvitationAction } from "./actions";
import { workspaceErrorMessage } from "./error-messages";

export const metadata: Metadata = { title: "Workspaces · Agenty" };

const first = (value: string | string[] | undefined) => (Array.isArray(value) ? value[0] : value);

export default async function WorkspacesPage({ searchParams }: PageProps<"/workspaces">) {
  const user = await requireUser();
  const [workspaces, invitations, params] = await Promise.all([
    listWorkspaces(user.id),
    listInvitationsForUser(user.id),
    searchParams,
  ]);
  const error = workspaceErrorMessage(first(params.error));

  return (
    <div className="flex flex-col gap-6">
      <h1 className="font-semibold text-2xl tracking-tight">Workspaces</h1>
      {error ? (
        <p className="text-destructive text-sm" role="alert">
          {error}
        </p>
      ) : null}

      {invitations.length > 0 ? (
        <Card>
          <CardHeader>
            <CardTitle>Invitations</CardTitle>
          </CardHeader>
          <CardContent>
            <ul className="flex flex-col gap-3">
              {invitations.map((invitation) => (
                <li className="flex items-center justify-between gap-4" key={invitation.id}>
                  <span>
                    <span className="font-medium">{invitation.workspaceName}</span>
                    {invitation.invitedBy ? (
                      <span className="text-muted-foreground">
                        {" "}
                        · invited by {invitation.invitedBy}
                      </span>
                    ) : null}
                  </span>
                  <span className="flex gap-2">
                    <form action={acceptInvitationAction}>
                      <input name="invitationId" type="hidden" value={invitation.id} />
                      <Button type="submit">Accept</Button>
                    </form>
                    <form action={declineInvitationAction}>
                      <input name="invitationId" type="hidden" value={invitation.id} />
                      <Button type="submit" variant="outline">
                        Decline
                      </Button>
                    </form>
                  </span>
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle>Your workspaces</CardTitle>
        </CardHeader>
        <CardContent>
          <ul className="flex flex-col gap-2">
            {workspaces.map((ws) => (
              <li className="flex items-center gap-3" key={ws.id}>
                <Link className="font-medium hover:underline" href={`/w/${ws.id}`}>
                  {ws.name}
                </Link>
                <span className="text-muted-foreground text-sm">
                  {ws.role === "admin" ? "Admin" : "Member"}
                </span>
                {ws.personal ? (
                  <span className="rounded-full border px-2 text-muted-foreground text-xs">
                    Personal
                  </span>
                ) : null}
              </li>
            ))}
          </ul>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Create a workspace</CardTitle>
        </CardHeader>
        <CardContent>
          <form action={createWorkspaceAction} className="flex items-end gap-3">
            <div className="flex flex-1 flex-col gap-2">
              <Label htmlFor="create-name">Name</Label>
              <Input id="create-name" maxLength={80} name="name" required />
            </div>
            <Button type="submit">Create</Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
