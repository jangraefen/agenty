import Link from "next/link";
import { getOrganizationSlug } from "@/server/auth/members";
import type { getSignedIn } from "@/server/auth/tenant";
import { UserMenu } from "./user-menu";

export type SignedIn = Awaited<ReturnType<typeof getSignedIn>>;

/** Organization shell header. Render inside <Suspense>: it reads the database per request. */
export async function AppHeader({ signedIn }: { signedIn: SignedIn }) {
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
