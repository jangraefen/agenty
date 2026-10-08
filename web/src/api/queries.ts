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
