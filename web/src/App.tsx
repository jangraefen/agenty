/**
 * The app's composition root below main.tsx: it wires the session and the
 * theme into the query cache, the API client and the router, and keeps them
 * in step.
 *
 * It makes the one QueryClient, the one API client (api/client.ts, signed
 * with the session's token) and the one router (router.tsx), and provides
 * the theme (theme/theme.ts) and the query client to every component. Session
 * and theme are passed in rather than made here, so tests (test/render.tsx)
 * render the whole app with their own storage and a memory history.
 */
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { type RouterHistory, RouterProvider } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { makeClient } from "./api/client";
import type { Session } from "./auth/session";
import { makeRouter } from "./router";
import { type Theme, ThemeContext } from "./theme/theme";

/**
 * The whole app. Its effects subscribe to the session, to other tabs and to
 * the system's colour scheme, and unsubscribe on unmount.
 */
export function App({
  session,
  theme,
  history,
}: {
  session: Session;
  theme: Theme;
  history?: RouterHistory;
}) {
  // Made once per mounted App, in state, so a re-render keeps the cache and
  // the router. Retries are off: a failed request shows its error at once
  // (a route's error page offers to try again) rather than after several
  // silent attempts.
  const [queryClient] = useState(
    () => new QueryClient({ defaultOptions: { queries: { retry: false } } }),
  );
  const [router] = useState(() =>
    makeRouter({ queryClient, session, api: makeClient(session) }, history),
  );

  // Signing out, by the user or by a refused token, or another user signing
  // in in another tab, drops what the user could see and sends the router
  // back through its sign-in checks. Clearing matters: the cache holds what
  // only the signed-in user may see, and must not show it to the next one.
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
  // Each follow* returns its unsubscribe, which React calls on unmount.
  useEffect(() => session.followOtherTabs(window), [session]);
  useEffect(() => theme.followSystem(), [theme]);
  useEffect(() => theme.followOtherTabs(window), [theme]);

  return (
    <ThemeContext value={theme}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </ThemeContext>
  );
}
