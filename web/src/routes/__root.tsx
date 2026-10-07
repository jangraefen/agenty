import { createRootRouteWithContext, Link, Outlet } from "@tanstack/react-router";
import { RouteError } from "@/components/route-states";
import type { RouterContext } from "@/router";

export const Route = createRootRouteWithContext<RouterContext>()({
  component: Outlet,
  notFoundComponent: NotFound,
  errorComponent: (props) => (
    <main className="mx-auto max-w-xl p-6">
      <RouteError {...props} />
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
