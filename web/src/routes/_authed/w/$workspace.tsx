import { createFileRoute, Link, notFound, Outlet, useNavigate } from "@tanstack/react-router";
import { useId } from "react";
import { loadMe } from "@/auth/load-me";
import { Button } from "@/components/ui/button";

// A workspace's pages, under a header to switch workspaces and sign out.
export const Route = createFileRoute("/_authed/w/$workspace")({
  loader: async ({ context, params }) => {
    const me = await loadMe(context);
    if (!me.workspaces.includes(params.workspace)) {
      throw notFound();
    }
    return me;
  },
  component: WorkspaceLayout,
  notFoundComponent: WorkspaceNotFound,
});

function WorkspaceLayout() {
  const me = Route.useLoaderData();
  const { workspace } = Route.useParams();
  const { session } = Route.useRouteContext();
  const navigate = useNavigate();
  const id = useId();

  async function signOut() {
    session.signOut();
    await navigate({ to: "/sign-in" });
  }

  return (
    <div className="min-h-screen">
      <header className="flex items-center gap-4 border-b px-6 py-3">
        <Link to="/" className="font-semibold">
          Agenty
        </Link>
        <label htmlFor={`${id}-workspace`} className="sr-only">
          Workspace
        </label>
        <select
          id={`${id}-workspace`}
          value={workspace}
          onChange={(event) =>
            navigate({ to: "/w/$workspace/runs", params: { workspace: event.target.value } })
          }
          className="h-8 rounded-md border bg-background px-2 text-sm"
        >
          {me.workspaces.map((name) => (
            <option key={name} value={name}>
              {name}
            </option>
          ))}
        </select>
        <span className="ml-auto text-sm text-muted-foreground">{me.user}</span>
        <Button variant="outline" size="sm" onClick={signOut}>
          Sign out
        </Button>
      </header>
      <main className="px-6 py-4">
        <Outlet />
      </main>
    </div>
  );
}

function WorkspaceNotFound() {
  return (
    <main className="mx-auto max-w-xl p-6">
      <h1 className="text-xl font-semibold">Workspace not found</h1>
      <p className="mt-2 text-muted-foreground">
        It does not exist, or you are not a member of it.
      </p>
      <Link to="/" className="mt-2 inline-block underline">
        Your workspaces
      </Link>
    </main>
  );
}
