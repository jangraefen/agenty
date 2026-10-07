import type { QueryClient } from "@tanstack/react-query";
import { createRouter, type RouterHistory } from "@tanstack/react-router";
import type { Api } from "./api/client";
import type { Session } from "./auth/session";
import { RouteError, RoutePending } from "./components/route-states";
import { parseSearch, stringifySearch } from "./lib/search";
import { routeTree } from "./routeTree.gen";

export interface RouterContext {
  queryClient: QueryClient;
  session: Session;
  api: Api;
}

export function makeRouter(context: RouterContext, history?: RouterHistory) {
  return createRouter({
    routeTree,
    context,
    ...(history === undefined ? {} : { history }),
    defaultPreload: "intent",
    // The query cache decides when preloaded data is stale.
    defaultPreloadStaleTime: 0,
    defaultPendingComponent: RoutePending,
    defaultErrorComponent: RouteError,
    parseSearch,
    stringifySearch,
  });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof makeRouter>;
  }
}
