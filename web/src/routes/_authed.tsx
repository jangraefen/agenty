import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { meQuery } from "@/api/queries";
import { AppSidebar } from "@/components/app-sidebar";
import { Shell } from "@/components/shell";

/**
 * The authed layout: the guard and the shell of every page of a signed-in
 * user.
 *
 * `_authed` is a pathless route: its leading underscore keeps it out of the
 * URL, so its children are `/`, `/c/{conversation}`, `/activity`,
 * `/w/{ws}/...` and `/audit/...`. Being their common parent, it is the one
 * place that decides whether a page may show at all, before any child's
 * beforeLoad or loader asks the API for its data.
 *
 * Its beforeLoad loads the user (`GET /v1/me`: their name, workspaces and
 * whether they are an auditor) and returns it as `me`, which the router
 * merges into the context of every route below. The children use it to
 * check membership (w/$workspace.tsx), the auditor flag (audit.tsx), and to
 * tell a chat of a workspace the user left (components/chat.tsx). The
 * component wraps every child in the Shell, with the AppSidebar beside it.
 *
 * Signing out, from the sidebar, in another tab, or by the API client when
 * the server refuses the token on any request, invalidates the router (see
 * App.tsx), which runs this guard again and so sends the browser to
 * `/sign-in`.
 */
export const Route = createFileRoute("/_authed")({
  beforeLoad: async ({ context: { queryClient, session, api } }) => {
    // No token: nothing below can load, so there is no request to make.
    if (session.token === null) {
      throw redirect({ to: "/sign-in" });
    }
    // ensureQueryData answers from the cache once the user is loaded, so
    // each navigation does not ask for /v1/me again; sign-in fills that
    // cache entry itself.
    try {
      return { me: await queryClient.ensureQueryData(meQuery(api)) };
    } catch (error) {
      // A token the server refuses, such as one the operator removed, sends
      // the user to sign in again. Any other failure is an error page, not a
      // sign-out: the token may well be valid.
      if (error instanceof ApiError && error.status === 401) {
        throw redirect({ to: "/sign-in" });
      }
      throw error;
    }
  },
  // Every signed-in page sits beside the one sidebar; the shell hides it
  // behind a menu button on narrow screens.
  component: () => (
    <Shell sidebar={<AppSidebar />}>
      <Outlet />
    </Shell>
  ),
});
