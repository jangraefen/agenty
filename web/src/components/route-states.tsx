import { type ErrorComponentProps, useRouter } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

// What a page shows while its data loads, when it cannot be loaded, and when
// what it would show does not exist.

export function RoutePending() {
  return <p className="text-muted-foreground">Loading…</p>;
}

export function RouteError({ error }: ErrorComponentProps) {
  const router = useRouter();
  return (
    <section className="grid justify-items-start gap-2">
      <h1 className="text-xl font-semibold">Something went wrong</h1>
      <p role="alert" className="text-muted-foreground">
        {error instanceof Error ? error.message : String(error)}
      </p>
      {/* Loads the page's data again, clearing the error. */}
      <Button variant="outline" size="sm" onClick={() => void router.invalidate()}>
        Try again
      </Button>
    </section>
  );
}

// NotFoundPage says what was not found, and maybe why in detail; children
// link to where the user can go on.
export function NotFoundPage({
  title,
  detail,
  className,
  children,
}: {
  title: string;
  detail?: string;
  className?: string;
  children: ReactNode;
}) {
  return (
    <section className={cn("grid justify-items-start gap-2", className)}>
      <h1 className="text-xl font-semibold">{title}</h1>
      {detail !== undefined && <p className="text-muted-foreground">{detail}</p>}
      {children}
    </section>
  );
}
