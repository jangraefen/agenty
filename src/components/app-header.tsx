import Link from "next/link";
import { Suspense } from "react";
import { getCurrentUser } from "@/server/auth/session";
import { listInvitationsForUser } from "@/server/workspaces/invitations";
import { UserMenu } from "./user-menu";

/** Header for signed-in users; renders nothing otherwise. Streams in: it reads the session. */
export function AppHeader() {
  return (
    <Suspense>
      <Header />
    </Suspense>
  );
}

async function Header() {
  const user = await getCurrentUser();
  if (!user) return null;
  const invitations = await listInvitationsForUser(user.id);

  return (
    <header className="flex items-center justify-between gap-4 border-b px-6 py-3">
      <div className="flex items-center gap-6">
        <Link className="font-semibold" href="/">
          Agenty
        </Link>
        <Link className="text-sm hover:underline" href="/workspaces">
          Workspaces
          {invitations.length > 0 ? (
            <span className="ml-1 rounded-full bg-primary px-1.5 text-primary-foreground text-xs">
              {invitations.length}
              <span className="sr-only"> pending invitations</span>
            </span>
          ) : null}
        </Link>
      </div>
      <UserMenu email={user.email} name={user.name} />
    </header>
  );
}
