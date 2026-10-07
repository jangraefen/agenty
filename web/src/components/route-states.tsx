import { type ErrorComponentProps, useRouter } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";

// What a page shows while its data loads, and when it cannot be loaded.

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
