import Link from "next/link";
import { Suspense } from "react";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarRail,
  SidebarSeparator,
} from "@/components/ui/sidebar";
import { getCurrentUser } from "@/server/auth/session";
import { countInvitationsForUser } from "@/server/workspaces/invitations";
import { listWorkspaces } from "@/server/workspaces/workspaces";
import { MobileTopBar } from "./mobile-top-bar";
import { UserMenu } from "./user-menu";
import { WorkspaceSwitcher } from "./workspace-switcher";

/**
 * Sidebar for signed-in users; renders nothing otherwise. Streams in: it reads the session. It is
 * part of the root layout, so only a refresh() (server actions) or a full load updates its data.
 */
export function AppSidebar() {
  return (
    <Suspense>
      <SignedInSidebar />
    </Suspense>
  );
}

/** The bar with the sidebar button on small screens, for signed-in users. */
export function AppTopBar() {
  return (
    <Suspense>
      <SignedInTopBar />
    </Suspense>
  );
}

async function SignedInSidebar() {
  const user = await getCurrentUser();
  if (!user) return null;
  const [workspaces, pendingInvitations] = await Promise.all([
    listWorkspaces(user.id),
    countInvitationsForUser(user.id),
  ]);

  return (
    <Sidebar>
      <nav aria-label="Main" className="flex h-full flex-col">
        <SidebarHeader>
          <Link className="px-2 py-1.5 font-semibold" href="/">
            Agenty
          </Link>
        </SidebarHeader>
        {/* Chats go here later. */}
        <SidebarContent />
        <SidebarFooter>
          <WorkspaceSwitcher
            pendingInvitations={pendingInvitations}
            workspaces={workspaces.map(({ id, name, personal }) => ({ id, name, personal }))}
          />
          <SidebarSeparator className="mx-0" />
          <UserMenu email={user.email} name={user.name} />
        </SidebarFooter>
      </nav>
      <SidebarRail />
    </Sidebar>
  );
}

async function SignedInTopBar() {
  const user = await getCurrentUser();
  return user ? <MobileTopBar /> : null;
}
