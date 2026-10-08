import { createFileRoute, Link, notFound, Outlet } from "@tanstack/react-router";
import { NotFoundPage } from "@/components/route-states";

// The management of a workspace. The membership check runs before any of its
// pages loads its data.
export const Route = createFileRoute("/_authed/w/$workspace")({
  beforeLoad: ({ context, params }) => {
    if (!context.me.workspaces.includes(params.workspace)) {
      throw notFound();
    }
  },
  component: Outlet,
  notFoundComponent: () => (
    <NotFoundPage
      title="Workspace not found"
      detail="It does not exist, or you are not a member of it."
      className="mx-auto max-w-xl"
    >
      <Link to="/" className="underline">
        Start a new chat
      </Link>
    </NotFoundPage>
  ),
});
