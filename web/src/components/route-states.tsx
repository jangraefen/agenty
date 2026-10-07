import type { ErrorComponentProps } from "@tanstack/react-router";

// What a page shows while its data loads, and when it cannot be loaded.

export function RoutePending() {
  return <p className="text-muted-foreground">Loading…</p>;
}

export function RouteError({ error }: ErrorComponentProps) {
  return (
    <section>
      <h1 className="text-xl font-semibold">Something went wrong</h1>
      <p role="alert" className="mt-2 text-muted-foreground">
        {error instanceof Error ? error.message : String(error)}
      </p>
    </section>
  );
}
