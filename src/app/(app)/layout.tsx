import { type ReactNode, Suspense } from "react";
import { AppHeader } from "@/components/app-header";
import { ForbiddenError, getCurrentSignedIn, UnauthorizedError } from "@/server/auth/tenant";

/**
 * Shell of the app pages: the organization header for signed-in members. Layouts are not
 * re-rendered on client navigation, so every page still checks the session and role itself.
 */
export default function AppLayout({ children }: { children: ReactNode }) {
  return (
    <>
      <Suspense>
        <Header />
      </Suspense>
      {children}
    </>
  );
}

async function Header() {
  try {
    await getCurrentSignedIn();
  } catch (error) {
    if (error instanceof UnauthorizedError || error instanceof ForbiddenError) return null;
    throw error;
  }
  return <AppHeader />;
}
