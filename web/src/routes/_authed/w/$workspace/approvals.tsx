import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { approvalsQuery } from "@/api/queries";
import { ApprovalCard } from "@/components/approval-card";

export const Route = createFileRoute("/_authed/w/$workspace/approvals")({
  component: Approvals,
});

function Approvals() {
  const { workspace } = Route.useParams();
  const { api } = Route.useRouteContext();
  const approvals = useQuery({ ...approvalsQuery(api, workspace), refetchInterval: 5000 });

  return (
    <section className="grid max-w-4xl gap-4">
      <h1 className="text-xl font-semibold">Approvals</h1>
      <p className="text-sm text-muted-foreground">
        Tool calls that policy holds until a member of the workspace answers. An unanswered request
        is rejected when its time runs out.
      </p>
      {approvals.isPending && <p className="text-muted-foreground">Loading approvals…</p>}
      {approvals.isError && (
        <p role="alert" className="text-destructive">
          The approvals could not be loaded: {approvals.error.message}
        </p>
      )}
      {approvals.data?.length === 0 && (
        <p className="text-muted-foreground">Nothing is waiting for approval.</p>
      )}
      {approvals.data !== undefined && approvals.data.length > 0 && (
        <ul aria-label="Waiting approvals" className="grid gap-3">
          {approvals.data.map((request) => (
            <ApprovalCard key={request.id} request={request} workspace={workspace} showRun />
          ))}
        </ul>
      )}
    </section>
  );
}
