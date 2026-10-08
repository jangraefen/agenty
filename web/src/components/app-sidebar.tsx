import { useInfiniteQuery } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useRouteContext } from "@tanstack/react-router";
import {
  LuBot,
  LuChevronsUpDown,
  LuCircleAlert,
  LuFileClock,
  LuLogOut,
  LuScrollText,
  LuSquarePen,
  LuUser,
} from "react-icons/lu";
import { recentChatsQuery } from "@/api/queries";
import { ThemeMenu } from "@/components/theme-menu";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

const itemClass =
  "flex min-w-0 items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent aria-[current=page]:bg-accent aria-[current=page]:font-medium";

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
      {me.auditor && (
        <nav aria-label="Compliance" className="grid gap-1">
          <h2 className="px-2 text-xs font-medium text-muted-foreground">Compliance</h2>
          <Link to="/audit" className={itemClass}>
            <LuScrollText aria-hidden="true" className="size-4 shrink-0" />
            Audit log
          </Link>
        </nav>
      )}
      <div className="grid gap-2 border-t pt-3">
        <div className="flex items-center gap-2">
          <Link
            to="/activity"
            aria-label={`${me.user}, your activity`}
            className={`flex-1 text-muted-foreground ${itemClass}`}
          >
            <LuUser aria-hidden="true" className="size-4 shrink-0" />
            <span className="truncate">{me.user}</span>
          </Link>
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
              className={itemClass}
            >
              <span className="truncate">{chat.title}</span>
              {chat.status === "waiting" && (
                <>
                  <LuCircleAlert
                    aria-hidden="true"
                    title="Waiting for approval"
                    className="ml-auto size-4 shrink-0 text-amber-500"
                  />{" "}
                  <span className="sr-only">(waiting for approval)</span>
                </>
              )}
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
  return (
    <nav aria-label="Manage" className="grid gap-1">
      <h2 className="px-2 text-xs font-medium text-muted-foreground">Manage</h2>
      {workspaces.length > 1 && <WorkspaceMenu workspace={workspace} workspaces={workspaces} />}
      <Link to="/w/$workspace/harnesses" params={{ workspace }} className={itemClass}>
        <LuBot aria-hidden="true" className="size-4 shrink-0" />
        Harnesses
      </Link>
      <Link to="/w/$workspace/audit" params={{ workspace }} className={itemClass}>
        <LuFileClock aria-hidden="true" className="size-4 shrink-0" />
        Audit log
      </Link>
    </nav>
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
