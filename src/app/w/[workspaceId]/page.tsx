import { Suspense } from "react";
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { requireWorkspaceMember } from "@/server/workspaces/access";

export default function WorkspacePage({ params }: PageProps<"/w/[workspaceId]">) {
  return (
    <Suspense>
      <WorkspaceHome params={params} />
    </Suspense>
  );
}

async function WorkspaceHome({ params }: { params: PageProps<"/w/[workspaceId]">["params"] }) {
  const { workspaceId } = await params;
  const { role } = await requireWorkspaceMember(workspaceId);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Agents arrive in the next milestone</CardTitle>
        <CardDescription>
          You are {role === "admin" ? "an admin" : "a member"} of this workspace.
        </CardDescription>
      </CardHeader>
    </Card>
  );
}
