import { useInfiniteQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { type AuditFilters, auditRunsQuery, isRunStatus, runStatuses } from "@/api/queries";
import { AuditHeader } from "@/components/audit-header";
import { FilterForm, textFilters } from "@/components/filter-form";
import { LoadMore } from "@/components/load-more";
import { RunStatusBadge } from "@/components/run-status";
import { Label } from "@/components/ui/label";
import { formatDuration, formatTime } from "@/lib/format";

/**
 * The audit log's runs view, at `/audit`, the auditors' landing page from
 * the sidebar's Compliance section.
 *
 * It lists the runs of every workspace (`GET /v1/audit/runs`), newest first,
 * a page at a time: who ran which harness, how it went and how long it took,
 * each linking to its audit records at `/audit/runs/{id}`. It shows nothing
 * that was said in a run: auditors see what happened, not inputs, replies or
 * transcripts, and the API does not give them those.
 *
 * The filters live in the URL's search parameters, so a filtered view can be
 * shared and survives a reload; FilterForm writes them, auditFilters reads
 * them back, and each set of filters is a query of its own in the cache.
 */

/** The text filters of the runs, by search parameter, with their labels. */
const fields = [
  { name: "workspace", label: "Workspace" },
  { name: "harness", label: "Harness" },
  { name: "started_by", label: "Started by" },
] as const;

/**
 * The filters a search holds: the text filters, trimmed, and a status only
 * when it is one of the run statuses. It validates the route's search and
 * reads the filter form, so both pass the API only what it accepts.
 */
function auditFilters(search: Record<string, unknown>): AuditFilters {
  const filters: AuditFilters = textFilters(
    search,
    fields.map((field) => field.name),
  );
  if (isRunStatus(search.status)) {
    filters.status = search.status;
  }
  return filters;
}

/** The runs of every workspace, for auditors; audit.tsx above keeps everyone else out. */
export const Route = createFileRoute("/_authed/audit/")({
  validateSearch: auditFilters,
  component: AuditRuns,
});

/** The runs view: the filters, the table of runs, and Load more. */
function AuditRuns() {
  const filters = auditFilters(Route.useSearch());
  const { api } = Route.useRouteContext();
  const navigate = Route.useNavigate();
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

  return (
    <section className="grid gap-6">
      <AuditHeader />
      <FilterForm
        fields={fields}
        values={filters}
        // Filtering replaces the history entry, so Back leaves the audit log
        // instead of stepping through every filter tried.
        onFilter={(form) => void navigate({ search: auditFilters(form), replace: true })}
      >
        {(id) => (
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
        )}
      </FilterForm>

      {runs.isPending && <p className="text-muted-foreground">Loading runs…</p>}
      {runs.isError && (
        <p role="alert" className="text-destructive">
          The runs could not be loaded: {runs.error.message}
        </p>
      )}
      {/* Tells screen readers how many runs are shown once a page loads. */}
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
      <LoadMore
        query={runs}
        // An empty last page gives "", which matches no row, so the focus
        // goes to the table.
        onLoaded={(data) => setFocusRun(data.pages.at(-1)?.runs[0]?.id ?? "")}
        variant="outline"
        className="justify-self-start"
      />
    </section>
  );
}
