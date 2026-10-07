import { createFileRoute, Link, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/_authed/")({
  loader: ({ context: { me } }) => {
    const [only, ...others] = me.workspaces;
    if (only !== undefined && others.length === 0) {
      throw redirect({ to: "/w/$workspace/runs", params: { workspace: only } });
    }
    return me;
  },
  component: Workspaces,
});

function Workspaces() {
  const me = Route.useLoaderData();
  return (
    <main className="mx-auto max-w-xl p-6">
      <h1 className="text-xl font-semibold">Workspaces</h1>
      {me.workspaces.length === 0 ? (
        <p className="mt-2 text-muted-foreground">You are not a member of any workspace yet.</p>
      ) : (
        <ul className="mt-4 grid gap-2">
          {me.workspaces.map((workspace) => (
            <li key={workspace}>
              <Link
                to="/w/$workspace/runs"
                params={{ workspace }}
                className="block rounded-md border px-4 py-3 hover:bg-accent"
              >
                {workspace}
              </Link>
            </li>
          ))}
        </ul>
      )}
    </main>
  );
}
