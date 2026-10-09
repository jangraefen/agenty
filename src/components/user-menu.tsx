"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { authClient } from "@/lib/auth-client";
import type { Role } from "@/server/auth/providers";

function useSignOut() {
  const router = useRouter();
  const [pending, setPending] = useState(false);
  async function signOut() {
    setPending(true);
    try {
      const { error } = await authClient.signOut();
      if (!error) {
        router.replace("/sign-in");
        router.refresh();
      }
    } finally {
      setPending(false);
    }
  }
  return { signOut, pending };
}

export function UserMenu({ name, email, role }: { name: string; email: string; role: Role }) {
  const { signOut, pending } = useSignOut();
  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={<Button variant="ghost" />}>{name}</DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-64">
        <DropdownMenuGroup>
          <DropdownMenuLabel className="flex flex-col gap-1">
            <span className="font-medium text-foreground">{name}</span>
            <span>{email}</span>
            <Badge className="mt-1" variant={role === "admin" ? "default" : "secondary"}>
              {role}
            </Badge>
          </DropdownMenuLabel>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem disabled={pending} onClick={signOut}>
          Sign out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function SignOutButton() {
  const { signOut, pending } = useSignOut();
  return (
    <Button disabled={pending} onClick={signOut} variant="outline">
      Sign out
    </Button>
  );
}
