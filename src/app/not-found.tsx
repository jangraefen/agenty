import Link from "next/link";
import { buttonVariants } from "@/components/ui/button";

/**
 * Unmatched URLs. It renders in the bare root layout (no sidebar, since it can't know the session
 * frame); `notFound()` in signed-in pages renders `(signed-in)/not-found.tsx` instead.
 */
export default function NotFound() {
  return (
    <main className="mx-auto flex min-h-svh w-full max-w-2xl flex-col justify-center gap-4 p-4 md:p-8">
      <div className="flex flex-col gap-2">
        <h1 className="font-semibold text-2xl tracking-tight">Page not found</h1>
        <p className="text-muted-foreground">This page doesn't exist.</p>
      </div>
      <div>
        <Link className={buttonVariants({ variant: "outline" })} href="/">
          Go to the start page
        </Link>
      </div>
    </main>
  );
}
