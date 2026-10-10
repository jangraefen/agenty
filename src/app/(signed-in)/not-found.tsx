import Link from "next/link";
import { buttonVariants } from "@/components/ui/button";

/**
 * Shown when a signed-in page calls `notFound()` (e.g. a workspace the user isn't a member of).
 * It renders inside the signed-in layout, so the sidebar stays. Unknown ids and missing access look
 * the same on purpose.
 */
export default function SignedInNotFound() {
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-2">
        <h1 className="font-semibold text-2xl tracking-tight">Page not found</h1>
        <p className="text-muted-foreground">
          This page doesn't exist or you don't have access to it.
        </p>
      </div>
      <div>
        <Link className={buttonVariants({ variant: "outline" })} href="/workspaces">
          Go to your workspaces
        </Link>
      </div>
    </div>
  );
}
