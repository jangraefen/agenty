import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Link, notFound, Outlet, useNavigate } from "@tanstack/react-router";
import { approvalsQuery } from "@/api/queries";
import { loadMe } from "@/auth/load-me";
import { Button } from "@/components/ui/button";

// A workspace's pages, under a header to switch workspaces and sign out. The
// membership check runs before any of the pages loads its data.
export const Route = createFileRoute("/_authed/w/$workspace")({
  beforeLoad: async ({ context, params }) => {
    const me = await loadMe(context);
    if (!me.workspaces.includes(params.workspace)) {
      throw notFound();
    }
    return { me };
  },
  component: WorkspaceLayout,
  notFoundComponent: WorkspaceNotFound,
});

function WorkspaceLayout() {
  const { me, session } = Route.useRouteContext();
  const { workspace } = Route.useParams();
  const navigate = useNavigate();

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
        {/* Keyed by the workspace, so it closes after switching. */}
        <details key={workspace} className="relative">
          <summary className="flex h-8 cursor-pointer list-none items-center gap-1 rounded-md border px-2 text-sm">
            <span className="sr-only">Workspace:</span> {workspace}
            <span aria-hidden="true">▾</span>
          </summary>
          <nav
            aria-label="Workspaces"
            className="absolute z-10 mt-1 min-w-40 rounded-md border bg-background py-1 shadow-md"
          >
            <ul>
              {me.workspaces.map((name) => (
                <li key={name}>
                  <Link
                    to="/w/$workspace/runs"
                    params={{ workspace: name }}
                    aria-current={name === workspace ? "page" : undefined}
                    className="block px-3 py-1.5 text-sm hover:bg-accent aria-[current=page]:font-semibold"
                  >
                    {name}
                  </Link>
                </li>
              ))}
            </ul>
          </nav>
        </details>
        <PageLinks workspace={workspace} />
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

function PageLinks({ workspace }: { workspace: string }) {
  const { api } = Route.useRouteContext();
  const approvals = useQuery(approvalsQuery(api, workspace));
  const waiting = approvals.data?.length ?? 0;
  const linkClass =
    "rounded-md px-2 py-1 text-sm hover:bg-accent aria-[current=page]:font-semibold";
  return (
    <nav aria-label="Pages">
      <ul className="flex gap-1">
        <li>
          <Link to="/w/$workspace/runs" params={{ workspace }} className={linkClass}>
            Runs
          </Link>
        </li>
        <li>
          <Link
            to="/w/$workspace/approvals"
            params={{ workspace }}
            aria-label={waiting > 0 ? `Approvals (${waiting} waiting)` : "Approvals"}
            className={linkClass}
          >
            Approvals
            {waiting > 0 && (
              <span className="ml-1.5 rounded-full bg-amber-200 px-1.5 text-xs font-medium text-amber-950">
                {waiting}
              </span>
            )}
          </Link>
        </li>
      </ul>
    </nav>
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
