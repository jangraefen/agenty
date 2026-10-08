import { createRootRouteWithContext, Link, Outlet } from "@tanstack/react-router";
import { NotFoundPage, RouteError } from "@/components/route-states";
import type { RouterContext } from "@/router";

export const Route = createRootRouteWithContext<RouterContext>()({
  component: Outlet,
  notFoundComponent: () => (
    <main className="mx-auto max-w-xl p-6">
      <NotFoundPage title="Page not found">
        <Link to="/" className="underline">
          Home
        </Link>
      </NotFoundPage>
    </main>
  ),
  errorComponent: (props) => (
    <main className="mx-auto max-w-xl p-6">
      <RouteError {...props} />
    </main>
  ),
});
