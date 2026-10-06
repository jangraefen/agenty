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

  // Signing out, by the user or by a refused token, drops what the user
  // could see and sends the router back through its sign-in checks.
  useEffect(
    () =>
      session.subscribe(() => {
        if (session.token === null) {
          queryClient.clear();
        }
        void router.invalidate();
      }),
    [session, queryClient, router],
  );
  useEffect(() => session.followOtherTabs(window), [session]);

  return (
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  );
}
