import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useEffect, useId, useRef, useState } from "react";
import {
  harnessesQuery,
  isRunStatus,
  type RunFilters,
  runStatuses,
  runsQuery,
} from "@/api/queries";
import { RunStatusBadge } from "@/components/run-status";
import { Button } from "@/components/ui/button";
import { formatDuration, formatTime } from "@/lib/format";

// The filters in a search, ignoring anything else: the router passes on the
// search parameters no route validates, too.
function runFilters(search: Record<string, unknown>): RunFilters {
  const { harness } = search;
  return {
    ...(typeof harness === "string" && harness !== "" ? { harness } : {}),
    ...(isRunStatus(search.status) ? { status: search.status } : {}),
  };
}

export const Route = createFileRoute("/_authed/w/$workspace/runs/")({
  validateSearch: runFilters,
  component: Runs,
});

const selectClass = "h-8 rounded-md border bg-background px-2 text-sm";

function Runs() {
  const { workspace } = Route.useParams();
  const filters = runFilters(Route.useSearch());
  const { api } = Route.useRouteContext();
  const navigate = Route.useNavigate();
  const id = useId();
  const runs = useInfiniteQuery(runsQuery(api, workspace, filters));
  const harnesses = useQuery(harnessesQuery(api, workspace));

  const harnessNames = new Set(harnesses.data?.map((version) => version.harness.name));
  if (filters.harness !== undefined) {
    harnessNames.add(filters.harness);
  }

  function setFilters(next: RunFilters) {
    void navigate({ search: next, replace: true });
  }

  const pages = runs.data?.pages ?? [];
  const shown = pages.reduce((count, page) => count + page.runs.length, 0);

  // After loading more, the focus moves to the first run loaded, as the
  // button may be gone.
  const table = useRef<HTMLTableElement>(null);
  const [focusRow, setFocusRow] = useState<number | null>(null);
  useEffect(() => {
    if (focusRow === null || shown <= focusRow) {
      return;
    }
    table.current?.querySelectorAll<HTMLElement>("tbody tr a")[focusRow]?.focus();
    setFocusRow(null);
  }, [focusRow, shown]);
  const filtered = filters.harness !== undefined || filters.status !== undefined;

  return (
    <section>
      <div className="flex flex-wrap items-center gap-4">
        <h1 className="mr-auto text-xl font-semibold">Runs</h1>
        <label htmlFor={`${id}-harness`} className="text-sm">
          Harness
        </label>
        <select
          id={`${id}-harness`}
          value={filters.harness ?? ""}
          onChange={(event) => {
            const { harness: _, ...rest } = filters;
            const harness = event.target.value;
            setFilters(harness === "" ? rest : { ...rest, harness });
          }}
          className={selectClass}
        >
          <option value="">All</option>
          {harnesses.isError && <option disabled>Harnesses could not be loaded</option>}
          {[...harnessNames].sort().map((name) => (
            <option key={name} value={name}>
              {name}
            </option>
          ))}
        </select>
        <label htmlFor={`${id}-status`} className="text-sm">
          Status
        </label>
        <select
          id={`${id}-status`}
          value={filters.status ?? ""}
          onChange={(event) => {
            const { status: _, ...rest } = filters;
            const status = event.target.value;
            setFilters(isRunStatus(status) ? { ...rest, status } : rest);
          }}
          className={selectClass}
        >
          <option value="">All</option>
          {runStatuses.map((status) => (
            <option key={status} value={status}>
              {status}
            </option>
          ))}
        </select>
      </div>

      {runs.isPending && <p className="mt-6 text-muted-foreground">Loading runs…</p>}
      {runs.isError && (
        <p role="alert" className="mt-6 text-destructive">
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
        <p className="mt-6 text-muted-foreground">
          {filtered ? "No runs match these filters." : "No runs yet."}
        </p>
      )}
      {shown > 0 && (
        <table ref={table} className="mt-6 w-full text-left text-sm" aria-busy={runs.isFetching}>
          <caption className="sr-only">Runs</caption>
          <thead className="border-b text-muted-foreground">
            <tr>
              <th className="py-2 pr-4 font-medium">Harness</th>
              <th className="py-2 pr-4 font-medium">Status</th>
              <th className="py-2 pr-4 font-medium">Input</th>
              <th className="py-2 pr-4 font-medium">Started by</th>
              <th className="py-2 pr-4 font-medium">Started</th>
              <th className="py-2 font-medium">Took</th>
            </tr>
          </thead>
          <tbody>
            {pages.flatMap((page) =>
              page.runs.map((run) => (
                <tr key={run.id} className="border-b last:border-0">
                  <td className="py-2 pr-4 whitespace-nowrap">
                    <Link
                      to="/w/$workspace/runs/$runId"
                      params={{ workspace, runId: run.id }}
                      className="underline-offset-4 hover:underline"
                    >
                      {run.harness} v{run.harness_version}
                    </Link>
                  </td>
                  <td className="py-2 pr-4">
                    <RunStatusBadge status={run.status} />
                  </td>
                  <td className="max-w-md truncate py-2 pr-4" title={run.input}>
                    {run.input}
                  </td>
                  <td className="py-2 pr-4">{run.started_by}</td>
                  <td className="py-2 pr-4 whitespace-nowrap">
                    <time dateTime={run.created_at}>{formatTime(run.created_at)}</time>
                  </td>
                  <td className="py-2 whitespace-nowrap">
                    {run.finished_at === undefined ? (
                      <>
                        <span aria-hidden="true">—</span>
                        <span className="sr-only">still running</span>
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
      )}
      {runs.hasNextPage && (
        <Button
          variant="outline"
          className="mt-4"
          disabled={runs.isFetchingNextPage}
          onClick={() => {
            setFocusRow(shown);
            void runs.fetchNextPage();
          }}
        >
          Load more
        </Button>
      )}
    </section>
  );
}
