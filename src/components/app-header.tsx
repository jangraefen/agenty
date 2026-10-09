import Link from "next/link";
import { getCurrentUser } from "@/server/auth/session";
import { UserMenu } from "./user-menu";

/**
 * Header for signed-in users; renders nothing otherwise. Render inside <Suspense>: it reads the
 * session per request.
 */
export async function AppHeader() {
  const user = await getCurrentUser();
  if (!user) return null;

  return (
    <header className="flex items-center justify-between gap-4 border-b px-6 py-3">
      <Link className="font-semibold" href="/">
        Agenty
      </Link>
      <UserMenu email={user.email} name={user.name} />
    </header>
  );
}
