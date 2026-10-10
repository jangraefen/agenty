import type { Metadata } from "next";
import { Suspense } from "react";
import { workspaceErrorMessage } from "@/app/workspaces/error-messages";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { requireWorkspaceMember, type WorkspaceAccess } from "@/server/workspaces/access";
import { listInvitationsForWorkspace, searchUsersToInvite } from "@/server/workspaces/invitations";
import { userSearchQuerySchema } from "@/server/workspaces/validation";
import { listMembers } from "@/server/workspaces/workspaces";
import {
  cancelInvitationAction,
  changeRoleAction,
  deleteWorkspaceAction,
  inviteUserAction,
  leaveWorkspaceAction,
  removeMemberAction,
  renameWorkspaceAction,
} from "./actions";

export const metadata: Metadata = { title: "Workspace settings · Agenty" };

type Props = PageProps<"/w/[workspaceId]/settings">;

export default function SettingsPage({ params, searchParams }: Props) {
  return (
    <Suspense>
      <Settings params={params} searchParams={searchParams} />
    </Suspense>
  );
}

const first = (value: string | string[] | undefined) => (Array.isArray(value) ? value[0] : value);
const displayName = (person: { name: string; email: string }) => person.name || person.email;
const selectClass = "h-8 rounded-md border border-input bg-transparent px-2 text-sm";

async function Settings({ params, searchParams }: Pick<Props, "params" | "searchParams">) {
  const { workspaceId } = await params;
  const access = await requireWorkspaceMember(workspaceId);
  const query = await searchParams;
  const error = workspaceErrorMessage(first(query.error));
  const isAdmin = access.role === "admin";
  const { personal } = access.workspace;

  return (
    <div className="flex flex-col gap-6">
      <h2 className="font-semibold text-xl">Settings</h2>
      {error ? (
        <p className="text-destructive text-sm" role="alert">
          {error}
        </p>
      ) : null}
      {isAdmin ? <RenameCard access={access} /> : null}
      {personal ? (
        <p className="text-muted-foreground text-sm">
          This is your personal workspace. You can rename it, but not share or delete it.
        </p>
      ) : (
        <>
          <MembersCard access={access} />
          {isAdmin ? <InviteCard access={access} q={first(query.q)?.trim() ?? ""} /> : null}
          <LeaveCard access={access} />
          {isAdmin ? <DeleteCard access={access} /> : null}
        </>
      )}
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

async function MembersCard({ access }: { access: Access }) {
  const members = await listMembers(access.user.id, access.workspace.id);
  const isAdmin = access.role === "admin";
  const id = access.workspace.id;

  return (
    <Card>
      <CardHeader>
        <CardTitle>Members</CardTitle>
      </CardHeader>
      <CardContent>
        <ul className="flex flex-col gap-3">
          {members.map((member) => (
            <li className="flex flex-wrap items-center justify-between gap-3" key={member.userId}>
              <span>
                <span className="font-medium">{displayName(member)}</span>
                <span className="text-muted-foreground"> · {member.email}</span>
              </span>
              {isAdmin ? (
                <span className="flex items-center gap-2">
                  <form
                    action={changeRoleAction.bind(null, id)}
                    className="flex items-center gap-2"
                  >
                    <input name="userId" type="hidden" value={member.userId} />
                    <select
                      aria-label={`Role of ${displayName(member)}`}
                      className={selectClass}
                      defaultValue={member.role}
                      name="role"
                    >
                      <option value="admin">Admin</option>
                      <option value="member">Member</option>
                    </select>
                    <Button size="sm" type="submit" variant="outline">
                      Save role
                    </Button>
                  </form>
                  {member.userId === access.user.id ? null : (
                    <form action={removeMemberAction.bind(null, id)}>
                      <input name="userId" type="hidden" value={member.userId} />
                      <Button size="sm" type="submit" variant="outline">
                        Remove
                      </Button>
                    </form>
                  )}
                </span>
              ) : (
                <span className="text-muted-foreground text-sm">
                  {member.role === "admin" ? "Admin" : "Member"}
                </span>
              )}
            </li>
          ))}
        </ul>
      </CardContent>
    </Card>
  );
}

async function InviteCard({ access, q }: { access: Access; q: string }) {
  const id = access.workspace.id;
  const parsed = q ? userSearchQuerySchema.safeParse(q) : undefined;
  const [results, invitations] = await Promise.all([
    parsed?.success ? searchUsersToInvite(access.user.id, id, parsed.data) : Promise.resolve(null),
    listInvitationsForWorkspace(access.user.id, id),
  ]);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Invite</CardTitle>
        <CardDescription>Search people who have signed in to Agenty before.</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <form action={`/w/${id}/settings`} className="flex items-end gap-3">
          <div className="flex flex-1 flex-col gap-2">
            <Label htmlFor="invite-q">Search users</Label>
            <Input defaultValue={q} id="invite-q" maxLength={100} name="q" />
          </div>
          <Button type="submit" variant="outline">
            Search
          </Button>
        </form>
        {parsed && !parsed.success ? (
          <p className="text-destructive text-sm" role="alert">
            Search needs 2 to 100 characters.
          </p>
        ) : null}
        {results && results.length === 0 ? (
          <p className="text-muted-foreground text-sm">No matching users.</p>
        ) : null}
        {results && results.length > 0 ? (
          <ul aria-label="Search results" className="flex flex-col gap-2">
            {results.map((found) => (
              <li className="flex items-center justify-between gap-3" key={found.id}>
                <span>
                  <span className="font-medium">{displayName(found)}</span>
                  <span className="text-muted-foreground"> · {found.email}</span>
                </span>
                <form action={inviteUserAction.bind(null, id)}>
                  <input name="userId" type="hidden" value={found.id} />
                  <input name="q" type="hidden" value={q} />
                  <Button size="sm" type="submit">
                    Invite
                  </Button>
                </form>
              </li>
            ))}
          </ul>
        ) : null}
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

function LeaveCard({ access }: { access: Access }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Leave</CardTitle>
      </CardHeader>
      <CardContent>
        <form action={leaveWorkspaceAction.bind(null, access.workspace.id)}>
          <Button type="submit" variant="outline">
            Leave workspace
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}

function DeleteCard({ access }: { access: Access }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Delete</CardTitle>
        <CardDescription>
          Deletes the workspace for all members. This can't be undone.
        </CardDescription>
      </CardHeader>
      <CardContent>
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
      </CardContent>
    </Card>
  );
}
