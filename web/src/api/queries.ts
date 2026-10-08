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

export interface RunFilters {
  harness?: string;
  status?: RunStatus;
}

/** The key under which every list of a workspace's runs is cached. */
export function runsKey(workspace: string) {
  return ["workspaces", workspace, "runs"] as const;
}

// A workspace's runs, newest first, a page at a time. While one of them runs,
// the pages are refreshed, so its status follows.
export function runsQuery(api: Api, workspace: string, filters: RunFilters) {
  return infiniteQueryOptions({
    queryKey: [...runsKey(workspace), filters],
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET("/v1/workspaces/{workspace}/runs", {
          params: {
            path: { workspace },
            query: { ...filters, ...(pageParam === "" ? {} : { before: pageParam }) },
          },
        }),
      ),
    initialPageParam: "",
    getNextPageParam: (page) =>
      page.next === undefined || page.next === "" ? undefined : page.next,
    refetchInterval: (query) =>
      query.state.data?.pages.some((page) => page.runs.some((run) => unfinished(run.status)))
        ? 5000
        : false,
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

export function auditQuery(api: Api, workspace: string, id: string) {
  return queryOptions({
    queryKey: ["workspaces", workspace, "run", id, "audit"],
    queryFn: () =>
      unwrap(
        api.GET("/v1/workspaces/{workspace}/runs/{id}/audit", {
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
 * so its page shows the conversation at once, and refreshes the lists it joins.
 */
export function cacheStartedRun(
  queryClient: QueryClient,
  api: Api,
  workspace: string,
  run: Schemas["Run"],
  earlier: Schemas["Run"][] = [],
) {
  queryClient.setQueryData(runQuery(api, workspace, run.id).queryKey, run);
  queryClient.setQueryData(conversationQuery(api, workspace, run.id).queryKey, [...earlier, run]);
  void queryClient.invalidateQueries({ queryKey: conversationsKey(workspace) });
  void queryClient.invalidateQueries({ queryKey: runsKey(workspace) });
}
