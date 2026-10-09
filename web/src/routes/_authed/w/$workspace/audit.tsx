import { useInfiniteQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { workspaceAuditQuery } from "@/api/queries";
import { EventsTable } from "@/components/events-table";

/**
 * The workspace's audit log, at `/w/{ws}/audit`: the changes made to the
 * workspace, for its members (`GET /v1/workspaces/{ws}/audit`): its
 * harnesses' new versions. Its members' runs are theirs, not the
 * workspace's, as runs are private; only auditors see every run, under
 * `/audit`.
 *
 * The overview shows the latest few of these same events and links here for
 * the rest.
 */
export const Route = createFileRoute("/_authed/w/$workspace/audit")({
  component: WorkspaceAudit,
});

/**
 * The page: every change, a page at a time, with who made it. The Workspace
 * column is left out, as every event is this workspace's.
 */
function WorkspaceAudit() {
  const { workspace } = Route.useParams();
  const { api } = Route.useRouteContext();
  const events = useInfiniteQuery(workspaceAuditQuery(api, workspace));
  return (
    <section className="grid gap-6">
      <h1 className="text-xl font-semibold">Audit log</h1>
      <EventsTable events={events} empty="No changes yet." showActor showWorkspace={false} />
    </section>
  );
}
