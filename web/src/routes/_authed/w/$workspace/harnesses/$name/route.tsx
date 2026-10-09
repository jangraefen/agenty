import { createFileRoute, Link, Outlet } from "@tanstack/react-router";
import { harnessQuery } from "@/api/queries";
import { NotFoundPage } from "@/components/route-states";
import { orNotFound } from "@/lib/not-found";

/**
 * The layout of a harness's pages, at `/w/{ws}/harnesses/{name}`: its page
 * (index.tsx) and its edit page (edit.tsx), which both need it loaded.
 *
 * The loader loads the harness's latest version once, for both, into the
 * query cache, where the children read it with useSuspenseQuery; a harness
 * the API does not find shows HarnessNotFound in their place, instead of
 * each child handling a 404 of its own.
 */
export const Route = createFileRoute("/_authed/w/$workspace/harnesses/$name")({
  loader: ({ context: { queryClient, api }, params }) =>
    orNotFound(queryClient.ensureQueryData(harnessQuery(api, params.workspace, params.name))),
  component: Outlet,
  notFoundComponent: HarnessNotFound,
});

/** Shown for a harness name the workspace does not have, linking back to its list. */
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
