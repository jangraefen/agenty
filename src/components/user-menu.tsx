"use client";

import { ChevronsUpDownIcon, LogOutIcon } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";
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
import { authClient } from "@/lib/auth-client";

/** The signed-in user at the bottom of the sidebar: name, email and sign-out. */
export function UserMenu({ name, email }: { name: string; email: string }) {
  const router = useRouter();
  const [pending, setPending] = useState(false);
  const [failed, setFailed] = useState(false);
  // Better Auth allows an empty name; the email then labels the menu.
  const label = name || email;

  // Local sign-out: ends the app session only, so there is no IdP redirect to follow.
  async function signOut() {
    setFailed(false);
    setPending(true);
    const succeeded = await authClient.signOut().then(
      ({ error }) => !error,
      () => false,
    );
    setPending(false);
    if (!succeeded) {
      setFailed(true);
      return;
    }
    router.replace("/");
    router.refresh();
  }

  return (
    <SidebarMenu>
      {failed ? (
        <li className="px-2 pb-2 text-destructive text-sm" role="alert">
          Sign-out failed. Please try again.
        </li>
      ) : null}
      <SidebarMenuItem>
        <DropdownMenu>
          <DropdownMenuTrigger
            aria-label={label}
            render={<SidebarMenuButton className="data-popup-open:bg-sidebar-accent" size="lg" />}
          >
            <span className="flex min-w-0 flex-1 flex-col text-left leading-tight">
              <span className="truncate font-medium">{label}</span>
              <span className="truncate text-muted-foreground text-xs">{email}</span>
            </span>
            <ChevronsUpDownIcon className="ml-auto" />
          </DropdownMenuTrigger>
          <DropdownMenuContent className="w-(--anchor-width) min-w-56" side="top">
            <DropdownMenuGroup>
              <DropdownMenuLabel className="flex flex-col gap-1">
                <span className="font-medium text-foreground">{label}</span>
                <span>{email}</span>
              </DropdownMenuLabel>
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
            <DropdownMenuItem disabled={pending} onClick={signOut}>
              <LogOutIcon aria-hidden="true" />
              Sign out
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
    </SidebarMenu>
  );
}
