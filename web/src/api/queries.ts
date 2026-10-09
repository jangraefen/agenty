/**
 * The TanStack Query options for every API read the frontend makes, and the
 * helpers that keep the cache consistent after a write.
 *
 * Each *Query function pairs a cache key with a fetcher that calls the typed
 * client (client.ts) and unwraps the result. Route loaders pass them to
 * queryClient.ensureQueryData, so a page renders with its data already
 * there, and components pass the same options to useQuery or
 * useInfiniteQuery, so both read one cache entry. Keeping key and fetcher
 * together here means a key is spelt in one place only, which the
 * invalidations below and in run-events.ts depend on.
 *
 * Key layout: workspace data lives under ["workspaces", workspace, ...], so a
 * prefix names everything of one workspace; the user's own conversations
 * under ["recent-chats"] (the paged list) and ["conversations", id] (one with
 * its runs); auditors' views under ["audit", ...]. The QueryClient keeps the
 * default staleTime of 0, so a mounted query refetches its data, and App.tsx
 * turns retries off, so a failure shows at once. Signing out clears the whole
 * cache (App.tsx), as it holds what only that user may see.
 */
import { infiniteQueryOptions, type QueryClient, queryOptions } from "@tanstack/react-query";
import { type Api, type Schemas, unwrap } from "./client";

/** A run's status, as the spec defines it. */
export type RunStatus = Schemas["RunStatus"];

/**
 * Every run status, in lifecycle order. Kept as a list, which the generated
 * union type cannot provide at run time, to validate search parameters
 * (isRunStatus) and to offer the statuses as filter choices.
 */
export const runStatuses: readonly RunStatus[] = [
  "queued",
  "running",
  "waiting",
  "succeeded",
  "failed",
  "cancelled",
];

/** Whether a run with this status has yet to end: queued, running or waiting for approval. */
export function unfinished(status: RunStatus): boolean {
  return status === "queued" || status === "running" || status === "waiting";
}

/** Whether value is a run status: narrows an untyped value, such as a search parameter. */
export function isRunStatus(value: unknown): value is RunStatus {
  return runStatuses.some((status) => status === value);
}

/**
 * The signed-in user. The signed-in layout's beforeLoad ensures it, so every
 * signed-in page has it in its route context; a 401 here sends the browser
 * to sign in.
 */
export function meQuery(api: Api) {
  return queryOptions({
    queryKey: ["me"],
    queryFn: () => unwrap(api.GET("/v1/me")),
  });
}

/** A workspace of the user's, with its members. */
export function workspaceQuery(api: Api, workspace: string) {
  return queryOptions({
    queryKey: ["workspaces", workspace],
    queryFn: () =>
      unwrap(api.GET("/v1/workspaces/{workspace}", { params: { path: { workspace } } })),
  });
}

/**
 * The harnesses of a workspace, each in its latest version. Under the
 * workspace's key, beside harnessQuery's entries, so saving a harness can
 * invalidate the list without dropping the harness it has just cached.
 */
export function harnessesQuery(api: Api, workspace: string) {
  return queryOptions({
    queryKey: ["workspaces", workspace, "harnesses"],
    queryFn: () =>
      unwrap(api.GET("/v1/workspaces/{workspace}/harnesses", { params: { path: { workspace } } })),
  });
}

/**
 * The key under which the user's own conversations are cached. A function,
 * exported on its own, because writers (cacheStartedRun, the run event
 * stream) invalidate the list without needing an Api to build the options.
 */
export function recentChatsKey() {
  return ["recent-chats"] as const;
}

/**
 * The conversations the user started, in every workspace of theirs, latest
 * activity first, a page at a time: the sidebar's recent chats.
 *
 * Paging is by cursor: each page names the next page's before parameter,
 * and "" stands for the first page, which sends no before at all. The list
 * polls on its own while a chat shows an unfinished run, as the sidebar
 * has no event stream of its own to follow every chat.
 */
export function recentChatsQuery(api: Api) {
  return infiniteQueryOptions({
    queryKey: recentChatsKey(),
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET("/v1/conversations", {
          params: { query: pageParam === "" ? {} : { before: pageParam } },
        }),
      ),
    initialPageParam: "",
    getNextPageParam: (page) =>
      page.next === undefined || page.next === "" ? undefined : page.next,
    // Statuses follow: soon while a chat's run is under way, slowly while one
    // waits for an approval, which only expires on its own.
    refetchInterval: (query) => {
      const statuses = query.state.data?.pages.flatMap((page) =>
        page.conversations.map((chat) => chat.status),
      );
      if (statuses?.some((status) => status === "queued" || status === "running")) {
        return 5000;
      }
      return statuses?.includes("waiting") ? 60_000 : false;
    },
  });
}

/**
 * The key under which the user's conversations, each with its runs, are
 * cached. Kept apart from recentChatsKey: invalidating the list refreshes
 * the sidebar without refetching an open conversation, and the other way
 * round.
 */
function conversationsKey() {
  return ["conversations"] as const;
}

/**
 * A conversation of any of the user's workspaces, which tells its workspace,
 * with its runs, oldest first. Keyed by conversation ID alone, not under its
 * workspace, as a chat's URL (/c/$conversationId) does not name one.
 */
export function conversationQuery(api: Api, id: string) {
  return queryOptions({
    queryKey: [...conversationsKey(), id],
    queryFn: () => unwrap(api.GET("/v1/conversations/{id}", { params: { path: { id } } })),
  });
}

/**
 * The conversation with run in it, in place of the run with its ID, else
 * after the others, and with the status of its latest run.
 *
 * A pure function, so it serves both a cache update from a started run
 * (cacheStartedRun) and one from a run's end (run-events.ts), without either
 * waiting for a refetch.
 */
export function withRun(
  conversation: Schemas["Conversation"],
  run: Schemas["Run"],
): Schemas["Conversation"] {
  const runs = conversation.runs.some((r) => r.id === run.id)
    ? conversation.runs.map((r) => (r.id === run.id ? run : r))
    : [...conversation.runs, run];
  return { ...conversation, runs, status: runs.at(-1)?.status ?? conversation.status };
}

/** The filters of the auditors' run list; each one left out matches every run. */
export interface AuditFilters {
  workspace?: string;
  harness?: string;
  started_by?: string;
  status?: RunStatus;
}

/**
 * The runs of every workspace, newest first, a page at a time, for auditors.
 * The filters are part of the key, so each combination is cached, and paged,
 * on its own.
 */
export function auditRunsQuery(api: Api, filters: AuditFilters) {
  return infiniteQueryOptions({
    queryKey: ["audit", "runs", filters],
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET("/v1/audit/runs", {
          params: { query: { ...filters, ...(pageParam === "" ? {} : { before: pageParam }) } },
        }),
      ),
    initialPageParam: "",
    getNextPageParam: (page) =>
      page.next === undefined || page.next === "" ? undefined : page.next,
  });
}

/** A run of any workspace with its audit records, for auditors. */
export function auditRunQuery(api: Api, id: string) {
  return queryOptions({
    queryKey: ["audit", "run", id],
    queryFn: () => unwrap(api.GET("/v1/audit/runs/{id}", { params: { path: { id } } })),
  });
}

/**
 * A page of audit log events, newest first, read a page at a time: the
 * paging the three audit log views share. Event IDs are the cursor; 0 stands
 * for the first page, which sends no before, and a page without a next one
 * is the last.
 */
function eventsQuery(
  queryKey: readonly unknown[],
  fetch: (before: number | undefined) => Promise<Schemas["AuditLogEventList"]>,
) {
  return infiniteQueryOptions({
    queryKey,
    queryFn: ({ pageParam }) => fetch(pageParam === 0 ? undefined : pageParam),
    initialPageParam: 0,
    getNextPageParam: (page) =>
      page.next === undefined || page.next === 0 ? undefined : page.next,
  });
}

/** What the user did: their own entries in the audit log. */
export function myActivityQuery(api: Api) {
  return eventsQuery(["activity"], (before) =>
    unwrap(
      api.GET("/v1/me/activity", { params: { query: before === undefined ? {} : { before } } }),
    ),
  );
}

/**
 * The changes made to a workspace, in pages of limit events if given, else of
 * the server's default size. The size is part of the key, so a short list of
 * recent changes and the full log do not share pages.
 */
export function workspaceAuditQuery(api: Api, workspace: string, limit?: number) {
  const size = limit === undefined ? {} : { limit };
  return eventsQuery(["workspaces", workspace, "audit", size], (before) =>
    unwrap(
      api.GET("/v1/workspaces/{workspace}/audit", {
        params: {
          path: { workspace },
          query: { ...size, ...(before === undefined ? {} : { before }) },
        },
      }),
    ),
  );
}

/** The filters of the auditors' event log; each one left out matches every event. */
export type EventFilters = {
  actor?: string;
  workspace?: string;
  action?: string;
  run?: string;
};

/** Every event of the audit log, for auditors. */
export function auditEventsQuery(api: Api, filters: EventFilters) {
  return eventsQuery(["audit", "events", filters], (before) =>
    unwrap(
      api.GET("/v1/audit/events", {
        params: { query: { ...filters, ...(before === undefined ? {} : { before }) } },
      }),
    ),
  );
}

/**
 * A run's conversation with the model, its messages in order, with secrets
 * redacted by the server. The run
 * event stream invalidates it as audit records arrive, as each marks a point
 * where the transcript has grown.
 */
export function transcriptQuery(api: Api, workspace: string, id: string) {
  return queryOptions({
    queryKey: ["workspaces", workspace, "run", id, "transcript"],
    queryFn: () =>
      unwrap(
        api.GET("/v1/workspaces/{workspace}/runs/{id}/transcript", {
          params: { path: { workspace, id } },
        }),
      ),
  });
}

/**
 * A workspace's waiting approval requests, oldest first. The event stream of
 * a run refreshes them when it asks for an approval, gets an answer or ends;
 * a stream that stopped leaves them as they are until it is reconnected.
 */
export function approvalsQuery(api: Api, workspace: string) {
  return queryOptions({
    queryKey: ["workspaces", workspace, "approvals"],
    queryFn: () =>
      unwrap(api.GET("/v1/workspaces/{workspace}/approvals", { params: { path: { workspace } } })),
  });
}

/** A harness of a workspace, in its latest version. */
export function harnessQuery(api: Api, workspace: string, name: string) {
  return queryOptions({
    queryKey: ["workspaces", workspace, "harness", name],
    queryFn: () =>
      unwrap(
        api.GET("/v1/workspaces/{workspace}/harnesses/{name}", {
          params: { path: { workspace, name } },
        }),
      ),
  });
}

/**
 * Caches run, just started in workspace, in its conversation: the one it
 * follows up if given, else a new one. Its chat then shows it at once, while
 * the conversation and the recent chats are refreshed.
 *
 * The start answer holds the run only, so the conversation is put together
 * here: the followed one, or one shaped as the server would make it. It is
 * written to the cache first, so the chat page opens without a loading
 * state, then invalidated, so the server's own copy replaces it.
 */
export function cacheStartedRun(
  queryClient: QueryClient,
  api: Api,
  workspace: string,
  run: Schemas["Run"],
  followed?: Schemas["Conversation"],
) {
  const conversation = followed ?? {
    id: run.conversation_id,
    workspace,
    harness: run.harness,
    // Cut as the server cuts a title: to 100 characters, not UTF-16 units.
    title: Array.from(run.input).slice(0, 100).join(""),
    status: run.status,
    runs: [],
  };
  const { queryKey } = conversationQuery(api, run.conversation_id);
  queryClient.setQueryData(queryKey, withRun(conversation, run));
  // Not awaited: the caller navigates to the chat at once, and the refetches
  // land when they land.
  void queryClient.invalidateQueries({ queryKey });
  void queryClient.invalidateQueries({ queryKey: recentChatsKey() });
}
