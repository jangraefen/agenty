import { infiniteQueryOptions, type QueryClient, queryOptions } from "@tanstack/react-query";
import { type Api, type Schemas, unwrap } from "./client";

export type RunStatus = Schemas["RunStatus"];

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

export function isRunStatus(value: unknown): value is RunStatus {
  return runStatuses.some((status) => status === value);
}

export function meQuery(api: Api) {
  return queryOptions({
    queryKey: ["me"],
    queryFn: () => unwrap(api.GET("/v1/me")),
  });
}

// A workspace of the user's, with its members.
export function workspaceQuery(api: Api, workspace: string) {
  return queryOptions({
    queryKey: ["workspaces", workspace],
    queryFn: () =>
      unwrap(api.GET("/v1/workspaces/{workspace}", { params: { path: { workspace } } })),
  });
}

export function harnessesQuery(api: Api, workspace: string) {
  return queryOptions({
    queryKey: ["workspaces", workspace, "harnesses"],
    queryFn: () =>
      unwrap(api.GET("/v1/workspaces/{workspace}/harnesses", { params: { path: { workspace } } })),
  });
}

/** The key under which the user's own conversations are cached. */
export function recentChatsKey() {
  return ["recent-chats"] as const;
}

// The conversations the user started, in every workspace of theirs, latest
// activity first, a page at a time.
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

/** The key under which the user's conversations, each with its runs, are cached. */
function conversationsKey() {
  return ["conversations"] as const;
}

// A conversation of any of the user's workspaces, which tells its workspace,
// with its runs, oldest first.
export function conversationQuery(api: Api, id: string) {
  return queryOptions({
    queryKey: [...conversationsKey(), id],
    queryFn: () => unwrap(api.GET("/v1/conversations/{id}", { params: { path: { id } } })),
  });
}

/**
 * The conversation with run in it, in place of the run with its ID, else
 * after the others, and with the status of its latest run.
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

export interface AuditFilters {
  workspace?: string;
  harness?: string;
  started_by?: string;
  status?: RunStatus;
}

// The runs of every workspace, newest first, a page at a time, for auditors.
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

// A run of any workspace with its audit records, for auditors.
export function auditRunQuery(api: Api, id: string) {
  return queryOptions({
    queryKey: ["audit", "run", id],
    queryFn: () => unwrap(api.GET("/v1/audit/runs/{id}", { params: { path: { id } } })),
  });
}

// A page of audit log events, newest first, read a page at a time.
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

// What the user did.
export function myActivityQuery(api: Api) {
  return eventsQuery(["activity"], (before) =>
    unwrap(
      api.GET("/v1/me/activity", { params: { query: before === undefined ? {} : { before } } }),
    ),
  );
}

// The changes made to a workspace, in pages of limit events if given, else of
// the server's default size.
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

export type EventFilters = {
  actor?: string;
  workspace?: string;
  action?: string;
  run?: string;
};

// Every event of the audit log, for auditors.
export function auditEventsQuery(api: Api, filters: EventFilters) {
  return eventsQuery(["audit", "events", filters], (before) =>
    unwrap(
      api.GET("/v1/audit/events", {
        params: { query: { ...filters, ...(before === undefined ? {} : { before }) } },
      }),
    ),
  );
}

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

// A workspace's waiting approval requests, oldest first. The event stream of
// a run refreshes them when it asks for an approval, gets an answer or ends;
// a stream that stopped leaves them as they are until it is reconnected.
export function approvalsQuery(api: Api, workspace: string) {
  return queryOptions({
    queryKey: ["workspaces", workspace, "approvals"],
    queryFn: () =>
      unwrap(api.GET("/v1/workspaces/{workspace}/approvals", { params: { path: { workspace } } })),
  });
}

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
  void queryClient.invalidateQueries({ queryKey });
  void queryClient.invalidateQueries({ queryKey: recentChatsKey() });
}
