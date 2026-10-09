/**
 * The app's router: TanStack Router over the route tree generated from
 * routes/ (routeTree.gen.ts, which the Vite plugin writes; never edited).
 *
 * App.tsx calls makeRouter once, with the context every route receives: the
 * query client, the session and the API client. Routes use it in beforeLoad
 * to check sign-in and in loaders to ensure the queries of api/queries.ts,
 * so a page renders with its data in the cache. The router itself holds no
 * data: the query cache does, which is why preloaded data's freshness is
 * left to it.
 */
import type { QueryClient } from "@tanstack/react-query";
import { createRouter, type RouterHistory } from "@tanstack/react-router";
import type { Api } from "./api/client";
import type { Session } from "./auth/session";
import { RouteError, RoutePending } from "./components/route-states";
import { parseSearch, stringifySearch } from "./lib/search";
import { routeTree } from "./routeTree.gen";

/**
 * What every route's beforeLoad and loader receive (routes/__root.tsx
 * declares it as the root's context type). Built once in App.tsx; the
 * session's token changes over time, the objects do not.
 */
export interface RouterContext {
  queryClient: QueryClient;
  session: Session;
  api: Api;
}

/**
 * Creates the router with context. history is for tests, which pass a
 * memory history; the app uses the browser's. Pending and error states
 * default to the shared route-state components, and search parameters are
 * read and written as plain strings (lib/search.ts).
 */
export function makeRouter(context: RouterContext, history?: RouterHistory) {
  return createRouter({
    routeTree,
    context,
    ...(history === undefined ? {} : { history }),
    // Links preload their route's data on hover or focus, so a click often
    // finds it in the cache.
    defaultPreload: "intent",
    // The query cache decides when preloaded data is stale.
    defaultPreloadStaleTime: 0,
    defaultPendingComponent: RoutePending,
    defaultErrorComponent: RouteError,
    parseSearch,
    stringifySearch,
  });
}

// Registers the router's type with TanStack Router, so Link, useNavigate and
// the route hooks are typed by this app's routes, params and search.
declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof makeRouter>;
  }
  interface HistoryState {
    // focusMessage asks a conversation's page to focus its message box, as
    // when it opens after its first message was written on another page.
    focusMessage?: boolean;
  }
}
