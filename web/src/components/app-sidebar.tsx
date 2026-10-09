/**
 * The sidebar beside every page of a signed-in user, which the authed layout
 * (routes/_authed.tsx) puts in the Shell.
 *
 * From top to bottom: New chat (`/`), the user's recent chats (RecentChats,
 * linking to `/c/{conversation}`), the management of one workspace (Manage,
 * with a switcher, WorkspaceMenu, and links under `/w/{ws}`), the audit log
 * for auditors (`/audit`), and at the foot the user's name, linking to their
 * activity, Sign out and the theme menu.
 *
 * It reads the signed-in user from the authed layout's context, and the
 * recent chats from recentChatsQuery, which polls while a chat's run is
 * under way or waits for approval, and which the chat's event stream and
 * approval answers invalidate, so the marks beside the chats keep up.
 */
import { useInfiniteQuery } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useRouteContext } from "@tanstack/react-router";
import {
  LuBot,
  LuChevronsUpDown,
  LuCircleAlert,
  LuFileClock,
  LuLayoutDashboard,
  LuLock,
  LuLogOut,
  LuScrollText,
  LuSquarePen,
  LuUser,
} from "react-icons/lu";
import { recentChatsQuery } from "@/api/queries";
import { LoadMore } from "@/components/load-more";
import { ThemeMenu } from "@/components/theme-menu";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

/** The classes of a sidebar link; the router marks the current page's with aria-current. */
const itemClass =
  "flex min-w-0 items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent aria-[current=page]:bg-accent aria-[current=page]:font-medium";

/**
 * AppSidebar is beside every page of a signed-in user: a new chat, their
 * recent chats, the management of a workspace, and signing out. The
 * Compliance section shows only to auditors, whose `me.auditor` the server
 * set; for anyone else the audit log's pages do not exist.
 */
export function AppSidebar() {
  const { me, session } = useRouteContext({ from: "/_authed" });
  const navigate = useNavigate();
  // The workspace of a management page, if the user is a member of it, else
  // the first: pages outside a workspace, such as a chat, manage the first.
  const { workspace: param } = useParams({ strict: false });
  const workspace = param !== undefined && me.workspaces.includes(param) ? param : me.workspaces[0];

  // Signing out forgets the token; App.tsx, told by the session, clears the
  // query cache, so nothing of the user's stays in memory for the next.
  async function signOut() {
    session.signOut();
    await navigate({ to: "/sign-in" });
  }

  return (
    <>
      <span className="px-2 font-semibold">Agenty</span>
      {/* Current on the new chat page whichever harness its search picks. */}
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

/**
 * RecentChats lists the conversations the user started, in every workspace
 * of theirs, the latest active first, a page at a time with Show more.
 *
 * A chat in a workspace the user left is marked read-only, and a chat whose
 * latest run waits for approval is marked so, the mark the user acts on.
 * The icons are hidden from screen readers, which read the words beside
 * them instead.
 */
function RecentChats() {
  const { api, me } = useRouteContext({ from: "/_authed" });
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
              {!me.workspaces.includes(chat.workspace) && (
                <>
                  <LuLock
                    aria-hidden="true"
                    title="Read-only: you left its workspace"
                    className="ml-auto size-4 shrink-0 text-muted-foreground"
                  />{" "}
                  <span className="sr-only">(read-only)</span>
                </>
              )}
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
      <LoadMore query={chats} variant="ghost" size="sm" className="self-start">
        Show more
      </LoadMore>
    </nav>
  );
}

/**
 * Manage links the management pages of one workspace: its overview, its
 * harnesses and its audit log, under the switcher that names it. The
 * section shows even for a user of one workspace, so the sidebar always
 * says which workspace its links manage.
 */
function Manage({ workspace, workspaces }: { workspace: string; workspaces: string[] }) {
  return (
    <nav aria-label="Manage" className="grid gap-1">
      <h2 className="px-2 text-xs font-medium text-muted-foreground">Manage</h2>
      <WorkspaceMenu workspace={workspace} workspaces={workspaces} />
      <Link
        to="/w/$workspace"
        params={{ workspace }}
        // Only the overview itself, not every page under /w/{ws}.
        activeOptions={{ exact: true }}
        className={itemClass}
      >
        <LuLayoutDashboard aria-hidden="true" className="size-4 shrink-0" />
        Overview
      </Link>
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

/**
 * WorkspaceMenu names the workspace managed and switches to the overview of
 * another of the user's. Not modal, so the page stays usable while it is
 * open. Its items are links, so switching is navigation, and the sidebar
 * follows the new URL's workspace.
 */
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
              to="/w/$workspace"
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
