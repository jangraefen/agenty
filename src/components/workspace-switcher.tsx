"use client";

import { CheckIcon, ChevronsUpDownIcon, InboxIcon, PlusIcon } from "lucide-react";
import Link from "next/link";
import { useParams } from "next/navigation";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { SidebarMenu, SidebarMenuButton, SidebarMenuItem } from "@/components/ui/sidebar";

export type SwitcherWorkspace = { id: string; name: string; personal: boolean };

/**
 * The workspace in the URL (/w/[workspaceId]); elsewhere (e.g. /workspaces) the trigger says
 * "Workspaces" and no entry is marked. The list comes from the server (personal first); the URL is
 * read here, because the layout that renders the sidebar does not re-render on client navigations.
 */
export function WorkspaceSwitcher({
  workspaces,
  pendingInvitations,
}: {
  workspaces: SwitcherWorkspace[];
  pendingInvitations: number;
}) {
  const { workspaceId } = useParams<{ workspaceId?: string }>();
  const current = workspaces.find((ws) => ws.id === workspaceId);

  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={<SidebarMenuButton className="data-popup-open:bg-sidebar-accent" size="lg" />}
          >
            <span className="flex min-w-0 flex-1 flex-col text-left leading-tight">
              <span className="text-muted-foreground text-xs">Workspace</span>
              <span className="truncate font-medium">{current?.name ?? "Workspaces"}</span>
            </span>
            {pendingInvitations > 0 ? <InvitationBadge count={pendingInvitations} /> : null}
            <ChevronsUpDownIcon className="ml-auto" />
          </DropdownMenuTrigger>
          <DropdownMenuContent className="w-(--anchor-width) min-w-56" side="top">
            <DropdownMenuGroup>
              <DropdownMenuLabel>Workspaces</DropdownMenuLabel>
              {workspaces.map((ws) => (
                <DropdownMenuItem
                  aria-current={ws.id === current?.id ? "page" : undefined}
                  key={ws.id}
                  render={<Link href={`/w/${ws.id}`} />}
                >
                  <span className="min-w-0 flex-1 truncate">{ws.name}</span>
                  {ws.id === current?.id ? (
                    <>
                      <CheckIcon aria-hidden="true" />
                      <span className="sr-only">(current)</span>
                    </>
                  ) : null}
                </DropdownMenuItem>
              ))}
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
            <DropdownMenuItem render={<Link href="/workspaces" />}>
              <InboxIcon aria-hidden="true" />
              <span className="flex-1">All workspaces &amp; invitations</span>
              {pendingInvitations > 0 ? <InvitationBadge count={pendingInvitations} /> : null}
            </DropdownMenuItem>
            <DropdownMenuItem render={<Link href="/workspaces#create-workspace" />}>
              <PlusIcon aria-hidden="true" />
              Create workspace
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
    </SidebarMenu>
  );
}

function InvitationBadge({ count }: { count: number }) {
  return (
    <span className="rounded-full bg-primary px-1.5 text-primary-foreground text-xs tabular-nums">
      {count}
      <span className="sr-only"> pending invitations</span>
    </span>
  );
}
