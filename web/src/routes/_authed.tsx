import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { meQuery } from "@/api/queries";
import { AppSidebar } from "@/components/app-sidebar";
import { Shell } from "@/components/shell";

// The pages for a signed-in user, who is loaded into their context, beside
// the sidebar; the browser is sent to sign in when there is no token or the
// server refuses it.
export const Route = createFileRoute("/_authed")({
  beforeLoad: async ({ context: { queryClient, session, api } }) => {
    if (session.token === null) {
      throw redirect({ to: "/sign-in" });
    }
    try {
      return { me: await queryClient.ensureQueryData(meQuery(api)) };
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        throw redirect({ to: "/sign-in" });
      }
      throw error;
    }
  },
  component: () => (
    <Shell sidebar={<AppSidebar />}>
      <Outlet />
    </Shell>
  ),
});
