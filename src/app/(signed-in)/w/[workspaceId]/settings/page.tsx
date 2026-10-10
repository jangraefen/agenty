import type { Metadata } from "next";
import { redirect } from "next/navigation";
import { workspaceErrorMessage } from "@/app/(signed-in)/workspaces/error-messages";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
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
import { requireWorkspaceMember, type WorkspaceAccess } from "@/server/workspaces/access";
import { listInvitationsForWorkspace } from "@/server/workspaces/invitations";
import { listMembers } from "@/server/workspaces/workspaces";
import {
  cancelInvitationAction,
  changeRoleAction,
  deleteWorkspaceAction,
  leaveWorkspaceAction,
  removeMemberAction,
  renameWorkspaceAction,
} from "./actions";
import { InviteSearch } from "./invite-search";
import { RoleSelect } from "./role-select";

export const metadata: Metadata = { title: "Workspace settings · Agenty" };

type Props = PageProps<"/w/[workspaceId]/settings">;
type Member = Awaited<ReturnType<typeof listMembers>>[number];

const first = (value: string | string[] | undefined) => (Array.isArray(value) ? value[0] : value);
const displayName = (person: { name: string; email: string }) => person.name || person.email;

export default async function SettingsPage({ params, searchParams }: Props) {
  const { workspaceId } = await params;
  const access = await requireWorkspaceMember(workspaceId);
  // The personal workspace has nothing to set (its nav shows Settings disabled).
  if (access.workspace.personal) redirect(`/w/${access.workspace.id}`);
  const query = await searchParams;
  const error = workspaceErrorMessage(first(query.error));
  const isAdmin = access.role === "admin";
  const members = await listMembers(access.user.id, access.workspace.id);

  return (
    <div className="flex flex-col gap-6">
      <h2 className="font-semibold text-xl">Settings</h2>
      {error ? (
        <p className="text-destructive text-sm" role="alert">
          {error}
        </p>
      ) : null}
      {isAdmin ? <RenameCard access={access} /> : null}
      <MembersCard access={access} members={members} />
      {isAdmin ? (
        <InviteCard access={access} members={members} q={first(query.q)?.trim() ?? ""} />
      ) : null}
      <DangerZoneCard access={access} />
    </div>
  );
}

type Access = WorkspaceAccess & { user: { id: string } };

function RenameCard({ access }: { access: Access }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Name</CardTitle>
      </CardHeader>
      <CardContent>
        <form
          action={renameWorkspaceAction.bind(null, access.workspace.id)}
          className="flex items-end gap-3"
        >
          <div className="flex flex-1 flex-col gap-2">
            <Label htmlFor="rename-name">Workspace name</Label>
            <Input
              defaultValue={access.workspace.name}
              id="rename-name"
              maxLength={80}
              name="name"
              required
            />
          </div>
          <Button type="submit">Rename</Button>
        </form>
      </CardContent>
    </Card>
  );
}

function MembersCard({ access, members }: { access: Access; members: Member[] }) {
  const isAdmin = access.role === "admin";
  const id = access.workspace.id;

  return (
    <Card>
      <CardHeader>
        <CardTitle>Members</CardTitle>
      </CardHeader>
      <CardContent>
        <Table aria-label="Members">
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Email</TableHead>
              <TableHead>Role</TableHead>
              {isAdmin ? (
                <TableHead>
                  <span className="sr-only">Actions</span>
                </TableHead>
              ) : null}
            </TableRow>
          </TableHeader>
          <TableBody>
            {members.map((member) => (
              <TableRow key={member.userId}>
                <TableCell className="font-medium">{displayName(member)}</TableCell>
                <TableCell className="text-muted-foreground">{member.email}</TableCell>
                <TableCell>
                  {isAdmin ? (
                    <form action={changeRoleAction.bind(null, id)}>
                      <input name="userId" type="hidden" value={member.userId} />
                      <RoleSelect label={`Role of ${displayName(member)}`} role={member.role} />
                    </form>
                  ) : member.role === "admin" ? (
                    "Admin"
                  ) : (
                    "Member"
                  )}
                </TableCell>
                {isAdmin ? (
                  <TableCell className="text-right">
                    {member.userId === access.user.id ? null : (
                      <form action={removeMemberAction.bind(null, id)}>
                        <input name="userId" type="hidden" value={member.userId} />
                        <Button size="sm" type="submit" variant="outline">
                          Remove
                        </Button>
                      </form>
                    )}
                  </TableCell>
                ) : null}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  );
}

async function InviteCard({
  access,
  members,
  q,
}: {
  access: Access;
  members: Member[];
  q: string;
}) {
  const id = access.workspace.id;
  const invitations = await listInvitationsForWorkspace(access.user.id, id);
  // Changes whenever a member or invitation is added or removed; the search then reloads.
  const revision = [
    ...members.map((m) => `m${m.userId}`),
    ...invitations.map((i) => `i${i.userId}`),
  ]
    .sort()
    .join(",");

  return (
    <Card>
      <CardHeader>
        <CardTitle>Invite</CardTitle>
        <CardDescription>Search people who have signed in to Agenty before.</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <InviteSearch initialQuery={q} revision={revision} workspaceId={id} />
        {invitations.length > 0 ? (
          <div className="flex flex-col gap-2">
            <h3 className="font-medium text-sm">Pending invitations</h3>
            <ul aria-label="Pending invitations" className="flex flex-col gap-2">
              {invitations.map((invitation) => (
                <li className="flex items-center justify-between gap-3" key={invitation.id}>
                  <span>
                    {displayName(invitation)}
                    <span className="text-muted-foreground"> · {invitation.email}</span>
                  </span>
                  <form action={cancelInvitationAction.bind(null, id)}>
                    <input name="invitationId" type="hidden" value={invitation.id} />
                    <input name="q" type="hidden" value={q} />
                    <Button size="sm" type="submit" variant="outline">
                      Cancel
                    </Button>
                  </form>
                </li>
              ))}
            </ul>
          </div>
        ) : null}
      </CardContent>
    </Card>
  );
}

/** Leave for everyone; Delete (with the type-the-name confirmation) for admins. */
function DangerZoneCard({ access }: { access: Access }) {
  const isAdmin = access.role === "admin";
  return (
    <Card className="ring-destructive/40">
      <CardHeader>
        <CardTitle className="text-destructive">Danger zone</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-6">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex flex-col gap-1">
            <h3 className="font-medium text-sm">Leave</h3>
            <p className="text-muted-foreground text-sm">
              You lose access until an admin invites you again.
            </p>
          </div>
          <form action={leaveWorkspaceAction.bind(null, access.workspace.id)}>
            <Button type="submit" variant="outline">
              Leave workspace
            </Button>
          </form>
        </div>
        {isAdmin ? (
          <div className="flex flex-col gap-3 border-t pt-6">
            <div className="flex flex-col gap-1">
              <h3 className="font-medium text-sm">Delete</h3>
              <p className="text-muted-foreground text-sm">
                Deletes the workspace for all members. This can't be undone.
              </p>
            </div>
            <form
              action={deleteWorkspaceAction.bind(null, access.workspace.id)}
              className="flex items-end gap-3"
            >
              <div className="flex flex-1 flex-col gap-2">
                <Label htmlFor="delete-confirmation">Type the workspace name to confirm</Label>
                <Input autoComplete="off" id="delete-confirmation" name="confirmation" required />
              </div>
              <Button type="submit" variant="destructive">
                Delete workspace
              </Button>
            </form>
          </div>
        ) : null}
      </CardContent>
    </Card>
  );
}
