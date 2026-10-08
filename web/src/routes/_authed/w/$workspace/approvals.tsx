import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { approvalsQuery } from "@/api/queries";
import { AnswerNotice, type AnswerOutcome } from "@/components/answer-notice";
import { ApprovalCard } from "@/components/approval-card";

export const Route = createFileRoute("/_authed/w/$workspace/approvals")({
  // Prefetched, not required: the page polls, so it recovers by itself when
  // the approvals cannot be loaded for a while. Only when none are cached:
  // the page refreshes cached ones itself, and preloading on intent fetches
  // nothing.
  loader: ({ context: { queryClient, api }, params }) =>
    queryClient.prefetchQuery({
      ...approvalsQuery(api, params.workspace),
      staleTime: Number.POSITIVE_INFINITY,
    }),
  component: Approvals,
});

function Approvals() {
  const { workspace } = Route.useParams();
  const { api } = Route.useRouteContext();
  const approvals = useQuery({
    ...approvalsQuery(api, workspace),
    // What the loader just fetched counts as fresh for a second, as it would
    // for a suspense query, so mounting does not fetch it again.
    staleTime: 1000,
    refetchInterval: 5000,
  });
  const [outcome, setOutcome] = useState<AnswerOutcome | null>(null);

  return (
    <section className="grid gap-4">
      <h1 className="text-xl font-semibold">Approvals</h1>
      <p className="text-sm text-muted-foreground">
        Tool calls that policy holds until a member of the workspace answers. An unanswered request
        is rejected when its time runs out.
      </p>
      <AnswerNotice outcome={outcome} />
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
            <ApprovalCard
              key={request.id}
              request={request}
              workspace={workspace}
              showRun
              onOutcome={setOutcome}
            />
          ))}
        </ul>
      )}
    </section>
  );
}
