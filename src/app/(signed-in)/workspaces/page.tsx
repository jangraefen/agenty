import type { Metadata } from "next";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
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
          <Table aria-label="Your workspaces">
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Type</TableHead>
                <TableHead>Your role</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {workspaces.map((ws) => (
                <TableRow key={ws.id}>
                  <TableCell>
                    <Link className="font-medium hover:underline" href={`/w/${ws.id}`}>
                      {ws.name}
                    </Link>
                  </TableCell>
                  <TableCell className="text-muted-foreground">
                    {ws.personal ? "Personal" : "Shared"}
                  </TableCell>
                  <TableCell className="text-muted-foreground">
                    {ws.role === "admin" ? "Admin" : "Member"}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      <Card className="scroll-mt-16" id="create-workspace">
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
