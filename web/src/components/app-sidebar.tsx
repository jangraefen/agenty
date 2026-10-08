import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useRouteContext } from "@tanstack/react-router";
import type { ReactNode } from "react";
import type { IconType } from "react-icons";
import {
  LuBot,
  LuChevronsUpDown,
  LuHistory,
  LuLogOut,
  LuShieldCheck,
  LuSquarePen,
  LuUser,
} from "react-icons/lu";
import { approvalsQuery, recentChatsQuery } from "@/api/queries";
import { ThemeMenu } from "@/components/theme-menu";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

const linkClass =
  "rounded-md px-2 py-1.5 text-sm hover:bg-accent aria-[current=page]:bg-accent aria-[current=page]:font-medium";
const itemClass = `flex items-center gap-2 ${linkClass}`;

// AppSidebar is beside every page of a signed-in user: a new chat, their
// recent chats, the management of a workspace, and signing out.
export function AppSidebar() {
  const { me, session } = useRouteContext({ from: "/_authed" });
  const navigate = useNavigate();
  // The workspace of a management page, if the user is a member of it, else
  // the first.
  const { workspace: param } = useParams({ strict: false });
  const workspace = param !== undefined && me.workspaces.includes(param) ? param : me.workspaces[0];

  async function signOut() {
    session.signOut();
    await navigate({ to: "/sign-in" });
  }

  return (
    <>
      <span className="px-2 font-semibold">Agenty</span>
      <Link to="/" activeOptions={{ includeSearch: false }} className={itemClass}>
        <LuSquarePen aria-hidden="true" className="size-4 shrink-0" />
        New chat
      </Link>
      <RecentChats />
      {workspace !== undefined && <Manage workspace={workspace} workspaces={me.workspaces} />}
      <div className="grid gap-2 border-t pt-3">
        <div className="flex items-center gap-2">
          <span className="flex min-w-0 flex-1 items-center gap-2 px-2 text-sm text-muted-foreground">
            <LuUser aria-hidden="true" className="size-4 shrink-0" />
            <span className="truncate">{me.user}</span>
          </span>
          <Button variant="outline" size="sm" onClick={signOut}>
            <LuLogOut aria-hidden="true" />
            Sign out
          </Button>
        </div>
        <ThemeMenu />
      </div>
    </>
  );
}

function RecentChats() {
  const { api } = useRouteContext({ from: "/_authed" });
  const chats = useInfiniteQuery(recentChatsQuery(api));
  const listed = chats.data?.pages.flatMap((page) => page.conversations) ?? [];

  return (
    <nav aria-label="Recent chats" className="flex min-w-0 flex-1 flex-col gap-1">
      <h2 className="px-2 text-xs font-medium text-muted-foreground">Recent chats</h2>
      {chats.isError && (
        <p role="alert" className="px-2 text-sm text-destructive">
          The chats could not be loaded: {chats.error.message}
        </p>
      )}
      {chats.isSuccess && listed.length === 0 && (
        <p className="px-2 text-sm text-muted-foreground">No chats yet.</p>
      )}
      <ul className="flex flex-col gap-0.5">
        {listed.map((chat) => (
          <li key={chat.id}>
            <Link
              to="/c/$conversationId"
              params={{ conversationId: chat.id }}
              title={chat.title}
              className={`block truncate ${linkClass}`}
            >
              {chat.title}
            </Link>
          </li>
        ))}
      </ul>
      {chats.hasNextPage && (
        <Button
          variant="ghost"
          size="sm"
          className="self-start"
          aria-disabled={chats.isFetchingNextPage}
          onClick={() => void chats.fetchNextPage()}
        >
          Show more
        </Button>
      )}
    </nav>
  );
}

function Manage({ workspace, workspaces }: { workspace: string; workspaces: string[] }) {
  const { api } = useRouteContext({ from: "/_authed" });
  const approvals = useQuery(approvalsQuery(api, workspace));
  const waiting = approvals.data?.length ?? 0;
  return (
    <nav aria-label="Manage" className="grid gap-1">
      <h2 className="px-2 text-xs font-medium text-muted-foreground">Manage</h2>
      {workspaces.length > 1 && <WorkspaceMenu workspace={workspace} workspaces={workspaces} />}
      <ul className="grid gap-0.5">
        <ManageLink to="/w/$workspace/harnesses" workspace={workspace} icon={LuBot}>
          Harnesses
        </ManageLink>
        <ManageLink to="/w/$workspace/runs" workspace={workspace} icon={LuHistory}>
          Runs
        </ManageLink>
        <ManageLink
          to="/w/$workspace/approvals"
          workspace={workspace}
          icon={LuShieldCheck}
          label={waiting > 0 ? `Approvals (${waiting} waiting)` : undefined}
        >
          Approvals
          {waiting > 0 && (
            <span className="ml-auto rounded-full bg-amber-200 px-1.5 text-xs font-medium text-amber-950">
              {waiting}
            </span>
          )}
        </ManageLink>
      </ul>
    </nav>
  );
}

function ManageLink({
  to,
  workspace,
  icon: Icon,
  label,
  children,
}: {
  to: "/w/$workspace/harnesses" | "/w/$workspace/runs" | "/w/$workspace/approvals";
  workspace: string;
  icon: IconType;
  label?: string | undefined;
  children: ReactNode;
}) {
  return (
    <li>
      <Link to={to} params={{ workspace }} aria-label={label} className={itemClass}>
        <Icon aria-hidden="true" className="size-4 shrink-0" />
        {children}
      </Link>
    </li>
  );
}

// WorkspaceMenu switches to managing another of the user's workspaces. Not
// modal, so the page stays usable while it is open.
function WorkspaceMenu({ workspace, workspaces }: { workspace: string; workspaces: string[] }) {
  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger className="flex h-8 items-center justify-between gap-1 rounded-md border px-2 text-sm">
        <span>
          <span className="sr-only">Workspace:</span> {workspace}
        </span>
        <LuChevronsUpDown aria-hidden="true" className="size-4 text-muted-foreground" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="min-w-40">
        {workspaces.map((name) => (
          <DropdownMenuItem key={name} asChild>
            <Link
              to="/w/$workspace/harnesses"
              params={{ workspace: name }}
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
