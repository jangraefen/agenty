/**
 * The root of the route tree, which TanStack Router builds from the files
 * under routes/ (routeTree.gen.ts is generated from them, never edited).
 *
 * The tree has two branches below this root:
 *
 * - `/sign-in` (sign-in.tsx): the one page for a user without a token.
 * - `_authed` (_authed.tsx): a pathless layout route that guards every other
 *   page. It sends a browser without a valid token to `/sign-in`, loads the
 *   signed-in user (`GET /v1/me`) into the route context, and lays its pages
 *   out in the sidebar shell. Below it:
 *   - `/` a new chat, `/c/{conversation}` a conversation's chat, and
 *     `/activity` the user's own audit events: these name no workspace, as
 *     chats are the user's across all their workspaces;
 *   - `/w/{ws}` the management of one workspace: its overview, `harnesses`,
 *     `harnesses/{name}` with its `edit` page, `new-harness` and `audit`;
 *   - `/audit` the audit log, for auditors only: its runs, `runs/{id}` and
 *     `events`.
 *
 * The root itself renders only its child (`Outlet`). Its not-found and error
 * pages are the last resort for a URL or a failure no deeper route handles;
 * they sit in a plain centred column, as they may show before or without the
 * sidebar shell, whose data needs a signed-in user.
 *
 * The root's context type, RouterContext, carries the query client, the
 * session and the typed API client, so that every route's beforeLoad and
 * loader reach the API the same way the components do.
 */
import { createRootRouteWithContext, Link, Outlet } from "@tanstack/react-router";
import { NotFoundPage, RouteError } from "@/components/route-states";
import type { RouterContext } from "@/router";

/** The root route: renders its matched child, with fallback not-found and error pages. */
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
