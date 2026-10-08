import { createFileRoute, Link, notFound, Outlet } from "@tanstack/react-router";

// The management of a workspace. The membership check runs before any of its
// pages loads its data.
export const Route = createFileRoute("/_authed/w/$workspace")({
  beforeLoad: ({ context, params }) => {
    if (!context.me.workspaces.includes(params.workspace)) {
      throw notFound();
    }
  },
  component: Outlet,
  notFoundComponent: WorkspaceNotFound,
});

function WorkspaceNotFound() {
  return (
    <section className="mx-auto max-w-xl">
      <h1 className="text-xl font-semibold">Workspace not found</h1>
      <p className="mt-2 text-muted-foreground">
        It does not exist, or you are not a member of it.
      </p>
      <Link to="/" className="mt-2 inline-block underline">
        Start a new chat
      </Link>
    </section>
  );
}
