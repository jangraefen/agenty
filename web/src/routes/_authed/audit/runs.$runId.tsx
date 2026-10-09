import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { auditRunQuery } from "@/api/queries";
import { AuditRecords } from "@/components/audit-records";
import { NotFoundPage } from "@/components/route-states";
import { RunStatusBadge } from "@/components/run-status";
import { formatDuration, formatTime, formatUsage } from "@/lib/format";
import { orNotFound } from "@/lib/not-found";

/**
 * A run as an auditor sees it, at `/audit/runs/{id}`: who ran which harness
 * version in which workspace, how it went, its steps and tokens, and what
 * the gateway recorded for its tool calls (`GET /v1/audit/runs/{id}`, which
 * answers the run and its audit records together). Never what was said in
 * it: the run's input, output and transcript are its owner's, in their chat.
 *
 * The loader loads it before the page shows, so a run that does not exist
 * shows the not-found page; a run that follows up another links to that one,
 * so an auditor can walk a conversation back to its start.
 */
export const Route = createFileRoute("/_authed/audit/runs/$runId")({
  loader: ({ context: { queryClient, api }, params }) =>
    orNotFound(queryClient.ensureQueryData(auditRunQuery(api, params.runId))),
  component: AuditRunPage,
  notFoundComponent: () => (
    <NotFoundPage title="Run not found">
      <Link to="/audit" className="underline">
        All runs
      </Link>
    </NotFoundPage>
  ),
});

/** The page: the run's facts as a description list, then its audit records. */
function AuditRunPage() {
  const { runId } = Route.useParams();
  const { api } = Route.useRouteContext();
  const {
    data: { run, records },
  } = useSuspenseQuery(auditRunQuery(api, runId));
  return (
    <article className="grid max-w-3xl gap-4">
      <Link to="/audit" className="text-sm underline">
        All runs
      </Link>
      <header className="flex flex-wrap items-center gap-3 border-b pb-3">
        <h1 className="text-xl font-semibold">
          {run.harness} v{run.harness_version}
        </h1>
        <RunStatusBadge status={run.status} />
      </header>
      <dl className="grid grid-cols-[max-content_1fr] gap-x-6 gap-y-1 text-sm">
        <dt className="text-muted-foreground">Workspace</dt>
        <dd>{run.workspace}</dd>
        <dt className="text-muted-foreground">Started by</dt>
        <dd>{run.started_by}</dd>
        <dt className="text-muted-foreground">Started</dt>
        <dd>
          <time dateTime={run.created_at}>{formatTime(run.created_at)}</time>
        </dd>
        {run.finished_at !== undefined && (
          <>
            <dt className="text-muted-foreground">Took</dt>
            <dd>{formatDuration(run.created_at, run.finished_at)}</dd>
          </>
        )}
        <dt className="text-muted-foreground">Steps</dt>
        <dd>{run.steps}</dd>
        <dt className="text-muted-foreground">Tokens</dt>
        <dd>{formatUsage(run.usage)}</dd>
        {run.follows !== undefined && (
          <>
            <dt className="text-muted-foreground">Follows</dt>
            <dd>
              <Link to="/audit/runs/$runId" params={{ runId: run.follows }} className="underline">
                the run before it
              </Link>
            </dd>
          </>
        )}
        {run.error !== undefined && run.error !== "" && (
          <>
            <dt className="text-muted-foreground">Error</dt>
            <dd className="wrap-anywhere">{run.error}</dd>
          </>
        )}
      </dl>
      <section className="grid gap-2">
        <h2 className="text-sm font-semibold">Audit records</h2>
        <AuditRecords records={records} />
      </section>
    </article>
  );
}
