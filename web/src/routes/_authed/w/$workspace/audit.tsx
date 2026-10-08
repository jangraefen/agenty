import { useInfiniteQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { workspaceAuditQuery } from "@/api/queries";
import { EventsTable } from "@/components/events-table";

// The changes made to the workspace, from the audit log, for its members:
// its harnesses' new versions. Its members' runs are theirs, not the
// workspace's.
export const Route = createFileRoute("/_authed/w/$workspace/audit")({
  component: WorkspaceAudit,
});

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
