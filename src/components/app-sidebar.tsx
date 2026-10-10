import Link from "next/link";
import { cache, Suspense } from "react";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuItem,
  SidebarRail,
  SidebarSeparator,
} from "@/components/ui/sidebar";
import { Skeleton } from "@/components/ui/skeleton";
import { getCurrentUser } from "@/server/auth/session";
import { countInvitationsForUser } from "@/server/workspaces/invitations";
import { listWorkspaces } from "@/server/workspaces/workspaces";
import { MobileTopBar } from "./mobile-top-bar";
import { UserMenu } from "./user-menu";
import { WorkspaceSwitcher } from "./workspace-switcher";

/**
 * The sidebar of the signed-in layout. Its frame (app name, placeholders for the switcher and the
 * user menu) is part of the static shell, so a full page load reserves its space from the start;
 * the session-dependent parts stream into it. The layout doesn't re-render on client navigations,
 * so only a refresh() (server actions) or a full load updates its data.
 */
export function AppSidebar() {
  return (
    <Sidebar>
      <nav aria-label="Main" className="flex h-full flex-col">
        <SidebarHeader>
          <HomeLink className="px-2 py-1.5 font-semibold" />
        </SidebarHeader>
        {/* Chats go here later. */}
        <SidebarContent />
        <SidebarFooter>
          <Suspense fallback={<FooterSkeleton />}>
            <SidebarFooterContent />
          </Suspense>
        </SidebarFooter>
      </nav>
      <SidebarRail />
    </Sidebar>
  );
}

/** The bar with the sidebar button on small screens; static like the sidebar frame. */
export function AppTopBar() {
  return (
    <MobileTopBar>
      <HomeLink className="font-semibold" />
    </MobileTopBar>
  );
}

/** One set of queries per request, shared by the app name links and the footer. */
const loadSidebarData = cache(async () => {
  const user = await getCurrentUser();
  if (!user) return null;
  const [workspaces, pendingInvitations] = await Promise.all([
    listWorkspaces(user.id),
    countInvitationsForUser(user.id),
  ]);
  return { user, workspaces, pendingInvitations };
});

/**
 * "Agenty", linking to the personal workspace. It links there directly rather than to `/`: `/`
 * belongs to the signed-out layout, so passing through it would drop the sidebar for a moment.
 * Until the session is read it is plain text in the same place.
 */
function HomeLink({ className }: { className: string }) {
  return (
    <Suspense fallback={<span className={className}>Agenty</span>}>
      <HomeLinkContent className={className} />
    </Suspense>
  );
}

async function HomeLinkContent({ className }: { className: string }) {
  const data = await loadSidebarData();
  const personal = data?.workspaces.find((ws) => ws.personal);
  return (
    <Link className={className} href={personal ? `/w/${personal.id}` : "/"}>
      Agenty
    </Link>
  );
}

async function SidebarFooterContent() {
  const data = await loadSidebarData();
  // Signed out: the page redirects to the sign-in page.
  if (!data) return <FooterSkeleton />;
  return (
    <>
      <WorkspaceSwitcher
        pendingInvitations={data.pendingInvitations}
        workspaces={data.workspaces.map(({ id, name, personal }) => ({ id, name, personal }))}
      />
      <SidebarSeparator className="mx-0" />
      <UserMenu email={data.user.email} name={data.user.name} />
    </>
  );
}

/** Same height as the switcher and the user menu (large menu buttons). */
function FooterSkeleton() {
  return (
    <>
      <MenuButtonSkeleton />
      <SidebarSeparator className="mx-0" />
      <MenuButtonSkeleton />
    </>
  );
}

function MenuButtonSkeleton() {
  return (
    <SidebarMenu aria-hidden="true">
      <SidebarMenuItem>
        <Skeleton className="h-12 w-full" />
      </SidebarMenuItem>
    </SidebarMenu>
  );
}
