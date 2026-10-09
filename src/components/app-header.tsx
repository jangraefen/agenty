import Link from "next/link";
import { getOrganizationSlug } from "@/server/auth/members";
import { ForbiddenError, getCurrentSignedIn, UnauthorizedError } from "@/server/auth/tenant";
import { UserMenu } from "./user-menu";

/**
 * Organization header for signed-in members; renders nothing otherwise. It is part of the root
 * layout, which is not re-rendered on client navigation, so pages still check session and role
 * themselves. Render inside <Suspense>: it reads the session per request.
 */
export async function AppHeader() {
  const signedIn = await getCurrentSignedIn().catch((error: unknown) => {
    if (error instanceof UnauthorizedError || error instanceof ForbiddenError) return null;
    throw error;
  });
  if (!signedIn) return null;
  const { ctx, user } = signedIn;
  const slug = await getOrganizationSlug(ctx);

  return (
    <header className="flex items-center justify-between gap-4 border-b px-6 py-3">
      <nav className="flex items-center gap-6">
        <Link className="font-semibold" href="/">
          {slug}
        </Link>
        {ctx.role === "admin" ? (
          <Link
            className="text-muted-foreground text-sm hover:text-foreground"
            href="/settings/members"
          >
            Members
          </Link>
        ) : null}
      </nav>
      <UserMenu email={user.email} name={user.name} role={ctx.role} />
    </header>
  );
}
