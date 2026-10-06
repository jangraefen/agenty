import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { type RouterHistory, RouterProvider } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { makeClient } from "./api/client";
import type { Session } from "./auth/session";
import { makeRouter } from "./router";

export function App({ session, history }: { session: Session; history?: RouterHistory }) {
  const [queryClient] = useState(
    () => new QueryClient({ defaultOptions: { queries: { retry: false } } }),
  );
  const [router] = useState(() =>
    makeRouter({ queryClient, session, api: makeClient(session) }, history),
  );

  // Signing out, by the user or by a refused token, or another user signing
  // in in another tab, drops what the user could see and sends the router
  // back through its sign-in checks.
  useEffect(() => {
    let token = session.token;
    return session.subscribe(() => {
      if (session.token !== token) {
        token = session.token;
        queryClient.clear();
      }
      void router.invalidate();
    });
  }, [session, queryClient, router]);
  useEffect(() => session.followOtherTabs(window), [session]);

  return (
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  );
}
