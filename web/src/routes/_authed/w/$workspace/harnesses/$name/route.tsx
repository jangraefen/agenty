import { createFileRoute, Link, Outlet } from "@tanstack/react-router";
import { harnessQuery } from "@/api/queries";
import { NotFoundPage } from "@/components/route-states";
import { orNotFound } from "@/lib/not-found";

// A harness's pages, which need it loaded.
export const Route = createFileRoute("/_authed/w/$workspace/harnesses/$name")({
  loader: ({ context: { queryClient, api }, params }) =>
    orNotFound(queryClient.ensureQueryData(harnessQuery(api, params.workspace, params.name))),
  component: Outlet,
  notFoundComponent: HarnessNotFound,
});

function HarnessNotFound() {
  const { workspace } = Route.useParams();
  return (
    <NotFoundPage title="Harness not found">
      <Link to="/w/$workspace/harnesses" params={{ workspace }} className="underline">
        All harnesses
      </Link>
    </NotFoundPage>
  );
}
