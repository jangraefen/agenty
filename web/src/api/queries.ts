import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query";
import { type Api, unwrap } from "./client";
import type { components } from "./schema";

export type RunStatus = components["schemas"]["RunStatus"];

export const runStatuses: readonly RunStatus[] = ["running", "succeeded", "failed", "cancelled"];

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

// A workspace's runs, newest first, a page at a time. While one of them runs,
// the pages are refreshed, so its status follows.
export function runsQuery(api: Api, workspace: string, filters: RunFilters) {
  return infiniteQueryOptions({
    queryKey: ["workspaces", workspace, "runs", filters],
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
      query.state.data?.pages.some((page) => page.runs.some((run) => run.status === "running"))
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

export function approvalsQuery(api: Api, workspace: string) {
  return queryOptions({
    queryKey: ["workspaces", workspace, "approvals"],
    queryFn: () =>
      unwrap(api.GET("/v1/workspaces/{workspace}/approvals", { params: { path: { workspace } } })),
  });
}
