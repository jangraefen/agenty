import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Link, notFound, Outlet, useNavigate } from "@tanstack/react-router";
import { approvalsQuery } from "@/api/queries";
import { ThemeMenu } from "@/components/theme-menu";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

// A workspace's pages, under a header to switch workspaces and sign out. The
// membership check runs before any of the pages loads its data.
export const Route = createFileRoute("/_authed/w/$workspace")({
  beforeLoad: ({ context, params }) => {
    if (!context.me.workspaces.includes(params.workspace)) {
      throw notFound();
    }
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
      <header className="flex flex-wrap items-center gap-x-4 gap-y-2 border-b px-6 py-3">
        <Link to="/" className="font-semibold">
          Agenty
        </Link>
        <WorkspaceMenu workspace={workspace} workspaces={me.workspaces} />
        <PageLinks workspace={workspace} />
        {/* The user and signing out stay together when the header wraps. */}
        <div className="ml-auto flex items-center gap-4">
          <ThemeMenu />
          <span className="text-sm text-muted-foreground">{me.user}</span>
          <Button variant="outline" size="sm" onClick={signOut}>
            Sign out
          </Button>
        </div>
      </header>
      <main className="px-6 py-4">
        <Outlet />
      </main>
    </div>
  );
}

// WorkspaceMenu switches to another of the user's workspaces. Not modal, so
// the page stays usable while it is open.
function WorkspaceMenu({ workspace, workspaces }: { workspace: string; workspaces: string[] }) {
  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger className="flex h-8 items-center gap-1 rounded-md border px-2 text-sm">
        <span className="sr-only">Workspace:</span> {workspace}
        <span aria-hidden="true">▾</span>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="min-w-40">
        {workspaces.map((name) => (
          <DropdownMenuItem key={name} asChild>
            <Link
              to="/w/$workspace/runs"
              params={{ workspace: name }}
              // The current workspace, on any of its pages; on its runs
              // page, the router marks the link as the current page.
              aria-current={name === workspace ? "true" : undefined}
              className="aria-[current]:font-semibold"
            >
              {name}
            </Link>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
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
          <Link to="/w/$workspace/harnesses" params={{ workspace }} className={linkClass}>
            Harnesses
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
