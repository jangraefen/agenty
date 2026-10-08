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

// A conversation of any of the user's workspaces, which tells its workspace.
export function conversationSummaryQuery(api: Api, id: string) {
  return queryOptions({
    queryKey: ["conversation-summary", id],
    queryFn: () => unwrap(api.GET("/v1/conversations/{id}", { params: { path: { id } } })),
    // A conversation stays in its workspace, and its title is its first input.
    staleTime: Number.POSITIVE_INFINITY,
  });
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

// The changes made to a workspace.
export function workspaceAuditQuery(api: Api, workspace: string) {
  return eventsQuery(["workspaces", workspace, "audit"], (before) =>
    unwrap(
      api.GET("/v1/workspaces/{workspace}/audit", {
        params: { path: { workspace }, query: before === undefined ? {} : { before } },
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
  return {
    ...eventsQuery(["audit", "events", filters], (before) =>
      unwrap(
        api.GET("/v1/audit/events", {
          params: { query: { ...filters, ...(before === undefined ? {} : { before }) } },
        }),
      ),
    ),
  };
}

export function runQuery(api: Api, workspace: string, id: string) {
  return queryOptions({
    queryKey: ["workspaces", workspace, "run", id],
    queryFn: () =>
      unwrap(
        api.GET("/v1/workspaces/{workspace}/runs/{id}", { params: { path: { workspace, id } } }),
      ),
  });
}

/** The key under which the conversations of a workspace's runs are cached. */
export function conversationsKey(workspace: string) {
  return ["workspaces", workspace, "conversation"] as const;
}

// The runs of the conversation the run belongs to, oldest first.
export function conversationQuery(api: Api, workspace: string, id: string) {
  return queryOptions({
    queryKey: [...conversationsKey(workspace), id],
    queryFn: () =>
      unwrap(
        api.GET("/v1/workspaces/{workspace}/runs/{id}/conversation", {
          params: { path: { workspace, id } },
        }),
      ),
  });
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

// A workspace's waiting approval requests, oldest first. There is no stream
// of them for a whole workspace, so the pages that show them poll.
export function approvalsQuery(api: Api, workspace: string) {
  return queryOptions({
    queryKey: ["workspaces", workspace, "approvals"],
    queryFn: () =>
      unwrap(api.GET("/v1/workspaces/{workspace}/approvals", { params: { path: { workspace } } })),
    refetchInterval: 10_000,
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
 * Caches run, just started after earlier, the runs of its conversation so far,
 * so its chat shows it at once, and refreshes the lists it joins.
 */
export function cacheStartedRun(
  queryClient: QueryClient,
  api: Api,
  workspace: string,
  run: Schemas["Run"],
  earlier: Schemas["Run"][] = [],
) {
  queryClient.setQueryData(runQuery(api, workspace, run.id).queryKey, run);
  queryClient.setQueryData(conversationQuery(api, workspace, run.conversation_id).queryKey, [
    ...earlier,
    run,
  ]);
  void queryClient.invalidateQueries({ queryKey: conversationsKey(workspace) });
  void queryClient.invalidateQueries({ queryKey: recentChatsKey() });
}
