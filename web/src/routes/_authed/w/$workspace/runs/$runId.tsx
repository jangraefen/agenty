import { createFileRoute, Link, redirect } from "@tanstack/react-router";
import { runQuery } from "@/api/queries";
import { orNotFound } from "@/lib/not-found";

// A run is shown in its conversation's chat, scrolled to the run, so links to
// runs, such as an approval's, lead there.
export const Route = createFileRoute("/_authed/w/$workspace/runs/$runId")({
  loader: async ({ context: { queryClient, api }, params }) => {
    const run = await orNotFound(
      queryClient.ensureQueryData(runQuery(api, params.workspace, params.runId)),
    );
    throw redirect({
      to: "/c/$conversationId",
      params: { conversationId: run.conversation_id },
      hash: `run-${run.id}`,
      replace: true,
    });
  },
  notFoundComponent: RunNotFound,
});

function RunNotFound() {
  const { workspace } = Route.useParams();
  return (
    <section>
      <h1 className="text-xl font-semibold">Run not found</h1>
      <Link to="/w/$workspace/runs" params={{ workspace }} className="mt-2 inline-block underline">
        All runs
      </Link>
    </section>
  );
}
