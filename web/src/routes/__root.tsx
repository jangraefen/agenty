import { createRootRouteWithContext, Link, Outlet } from "@tanstack/react-router";
import type { RouterContext } from "@/router";

export const Route = createRootRouteWithContext<RouterContext>()({
  component: Outlet,
  notFoundComponent: NotFound,
  errorComponent: ({ error }) => (
    <main className="mx-auto max-w-xl p-6">
      <h1 className="text-xl font-semibold">Something went wrong</h1>
      <p role="alert" className="mt-2 text-muted-foreground">
        {error instanceof Error ? error.message : String(error)}
      </p>
    </main>
  ),
});

function NotFound() {
  return (
    <main className="mx-auto max-w-xl p-6">
      <h1 className="text-xl font-semibold">Page not found</h1>
      <Link to="/" className="mt-2 inline-block underline">
        Home
      </Link>
    </main>
  );
}
