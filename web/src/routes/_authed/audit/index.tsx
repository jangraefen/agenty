import { useInfiniteQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { type FormEvent, useEffect, useId, useRef, useState } from "react";
import { type AuditFilters, auditRunsQuery, isRunStatus, runStatuses } from "@/api/queries";
import { RunStatusBadge } from "@/components/run-status";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { formatDuration, formatTime } from "@/lib/format";

const textFilters = ["workspace", "harness", "started_by"] as const;

// The filters in a search, ignoring anything else: the router passes on the
// search parameters no route validates, too.
function auditFilters(search: Record<string, unknown>): AuditFilters {
  const filters: AuditFilters = {};
  for (const name of textFilters) {
    const value = search[name];
    if (typeof value === "string" && value.trim() !== "") {
      filters[name] = value.trim();
    }
  }
  if (isRunStatus(search.status)) {
    filters.status = search.status;
  }
  return filters;
}

// The runs of every workspace, for auditors: who ran which harness, how it
// went, and, a click away, what the gateway recorded. Never what was said.
export const Route = createFileRoute("/_authed/audit/")({
  validateSearch: auditFilters,
  component: AuditRuns,
});

const labels: Record<(typeof textFilters)[number], string> = {
  workspace: "Workspace",
  harness: "Harness",
  started_by: "Started by",
};

function AuditRuns() {
  const filters = auditFilters(Route.useSearch());
  const { api } = Route.useRouteContext();
  const navigate = Route.useNavigate();
  const id = useId();
  const runs = useInfiniteQuery(auditRunsQuery(api, filters));
  const pages = runs.data?.pages ?? [];
  const shown = pages.reduce((count, page) => count + page.runs.length, 0);
  const filtered = Object.keys(filters).length > 0;

  // After loading more, the focus moves to the first run of the page loaded,
  // as the button may be gone; after an empty page, to the table. A page
  // that fails leaves it on the button.
  const table = useRef<HTMLTableElement>(null);
  const [focusRun, setFocusRun] = useState<string | null>(null);
  useEffect(() => {
    if (focusRun === null) {
      return;
    }
    const link = table.current?.querySelector<HTMLElement>(
      `tr[data-run="${CSS.escape(focusRun)}"] a`,
    );
    (link ?? table.current)?.focus();
    setFocusRun(null);
  }, [focusRun]);

  async function loadMore() {
    const result = await runs.fetchNextPage();
    if (result.isSuccess) {
      setFocusRun(result.data.pages.at(-1)?.runs[0]?.id ?? "");
    }
  }

  function filter(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    void navigate({ search: auditFilters(Object.fromEntries(form)), replace: true });
  }

  return (
    <section className="grid gap-6">
      <h1 className="text-xl font-semibold">Audit log</h1>
      {/* Keyed by the filters, so the fields show them after each search. */}
      <form
        key={JSON.stringify(filters)}
        onSubmit={filter}
        className="flex flex-wrap items-end gap-4"
      >
        {textFilters.map((name) => (
          <div key={name} className="grid gap-1">
            <Label htmlFor={`${id}-${name}`} className="font-normal">
              {labels[name]}
            </Label>
            <Input
              id={`${id}-${name}`}
              name={name}
              defaultValue={filters[name] ?? ""}
              className="h-8 w-40"
            />
          </div>
        ))}
        <div className="grid gap-1">
          <Label htmlFor={`${id}-status`} className="font-normal">
            Status
          </Label>
          <select
            id={`${id}-status`}
            name="status"
            defaultValue={filters.status ?? ""}
            className="h-8 rounded-md border bg-background px-2 text-sm"
          >
            <option value="">All</option>
            {runStatuses.map((status) => (
              <option key={status} value={status}>
                {status}
              </option>
            ))}
          </select>
        </div>
        <Button type="submit" variant="outline" size="sm">
          Filter
        </Button>
      </form>

      {runs.isPending && <p className="text-muted-foreground">Loading runs…</p>}
      {runs.isError && (
        <p role="alert" className="text-destructive">
          The runs could not be loaded: {runs.error.message}
        </p>
      )}
      <p role="status" className="sr-only">
        {runs.isFetchingNextPage
          ? "Loading more runs…"
          : runs.data === undefined
            ? ""
            : `${shown} ${shown === 1 ? "run" : "runs"} shown.`}
      </p>
      {runs.isSuccess && shown === 0 && (
        <p className="text-muted-foreground">
          {filtered ? "No runs match these filters." : "No runs yet."}
        </p>
      )}
      {shown > 0 && (
        // Scrolls sideways where the page is narrower than the table.
        <div className="overflow-x-auto">
          <table
            ref={table}
            tabIndex={-1}
            className="w-full text-left text-sm -outline-offset-2 focus-visible:outline-2 focus-visible:outline-ring"
            aria-busy={runs.isFetching}
          >
            <caption className="sr-only">Runs</caption>
            <thead className="border-b text-muted-foreground">
              <tr>
                <th className="py-2 pr-4 font-medium">Harness</th>
                <th className="py-2 pr-4 font-medium">Workspace</th>
                <th className="py-2 pr-4 font-medium">Status</th>
                <th className="py-2 pr-4 font-medium">Started by</th>
                <th className="py-2 pr-4 font-medium">Started</th>
                <th className="py-2 font-medium">Took</th>
              </tr>
            </thead>
            <tbody>
              {pages.flatMap((page) =>
                page.runs.map((run) => (
                  <tr key={run.id} data-run={run.id} className="border-b last:border-0">
                    <td className="py-2 pr-4 whitespace-nowrap">
                      <Link
                        to="/audit/runs/$runId"
                        params={{ runId: run.id }}
                        className="underline-offset-4 hover:underline"
                      >
                        {run.harness} v{run.harness_version}
                      </Link>
                    </td>
                    <td className="py-2 pr-4">{run.workspace}</td>
                    <td className="py-2 pr-4">
                      <RunStatusBadge status={run.status} />
                    </td>
                    <td className="py-2 pr-4">{run.started_by}</td>
                    <td className="py-2 pr-4 whitespace-nowrap">
                      <time dateTime={run.created_at}>{formatTime(run.created_at)}</time>
                    </td>
                    <td className="py-2 whitespace-nowrap">
                      {run.finished_at === undefined ? (
                        <>
                          <span aria-hidden="true">—</span>
                          <span className="sr-only">not finished</span>
                        </>
                      ) : (
                        formatDuration(run.created_at, run.finished_at)
                      )}
                    </td>
                  </tr>
                )),
              )}
            </tbody>
          </table>
        </div>
      )}
      {runs.hasNextPage && (
        <Button
          variant="outline"
          className="justify-self-start"
          aria-disabled={runs.isFetchingNextPage}
          onClick={() => void loadMore()}
        >
          Load more
        </Button>
      )}
    </section>
  );
}
