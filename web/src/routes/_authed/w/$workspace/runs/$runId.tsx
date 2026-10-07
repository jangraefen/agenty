import {
  type UseQueryResult,
  useMutation,
  useQuery,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useId, useState } from "react";
import { type Schemas, unwrap } from "@/api/client";
import { approvalsQuery, runQuery, transcriptQuery } from "@/api/queries";
import { runEventsQuery } from "@/api/run-events";
import { AnswerNotice, type AnswerOutcome } from "@/components/answer-notice";
import { ApprovalCard } from "@/components/approval-card";
import { Json } from "@/components/json";
import { RunStatusBadge } from "@/components/run-status";
import { Button } from "@/components/ui/button";
import { formatDuration, formatTime } from "@/lib/format";
import { orNotFound } from "@/lib/not-found";

export const Route = createFileRoute("/_authed/w/$workspace/runs/$runId")({
  loader: ({ context: { queryClient, api }, params }) =>
    orNotFound(queryClient.ensureQueryData(runQuery(api, params.workspace, params.runId))),
  component: RunPage,
  notFoundComponent: RunNotFound,
});

function RunPage() {
  const { workspace, runId } = Route.useParams();
  const { api } = Route.useRouteContext();
  const run = useSuspenseQuery(runQuery(api, workspace, runId));
  const events = useQuery(runEventsQuery(api, workspace, runId));
  return (
    <>
      {run.isError && (
        <p role="alert" className="mb-4 text-sm text-destructive">
          The run could not be refreshed: {run.error.message}
        </p>
      )}
      <RunDetails run={run.data} events={events} />
    </>
  );
}

function RunNotFound() {
  const { workspace } = Route.useParams();
  return (
    <section>
      <h1 className="text-xl font-semibold">Run not found</h1>
      <Link to="/w/$workspace/runs" params={{ workspace }} className="mt-2 inline-block underline">
        All runs
      </Link>
    </section>
  );
}

function RunDetails({
  run,
  events,
}: {
  run: Schemas["Run"];
  events: UseQueryResult<Schemas["AuditRecord"][]>;
}) {
  const { workspace, runId } = Route.useParams();
  const { api } = Route.useRouteContext();
  const running = run.status === "running";
  const approvals = useQuery({ ...approvalsQuery(api, workspace), enabled: running });
  const waiting = running
    ? (approvals.data?.filter((request) => request.run_id === runId) ?? [])
    : [];
  const [outcome, setOutcome] = useState<AnswerOutcome | null>(null);
  const cancel = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/v1/workspaces/{workspace}/runs/{id}/cancel", {
          params: { path: { workspace, id: runId } },
        }),
      ),
  });

  return (
    <article className="grid max-w-4xl gap-6">
      <Link to="/w/$workspace/runs" params={{ workspace }} className="text-sm underline">
        All runs
      </Link>
      <header className="flex flex-wrap items-center gap-3">
        <h1 className="text-xl font-semibold">
          {run.harness} v{run.harness_version}
        </h1>
        <RunStatusBadge status={run.status} />
        {running && (
          <span className="ml-auto">
            <Button
              variant="destructive"
              size="sm"
              aria-disabled={!cancel.isIdle && !cancel.isError}
              onClick={() => cancel.mutate()}
            >
              {cancel.isSuccess ? "Cancelling…" : "Cancel run"}
            </Button>
          </span>
        )}
      </header>
      <p role="status" className="sr-only">
        The run is {run.status}.
        {waiting.length > 0 &&
          ` ${waiting.length} ${waiting.length === 1 ? "call waits" : "calls wait"} for approval.`}
      </p>
      {cancel.isError && (
        <p role="alert" className="text-sm text-destructive">
          The run could not be cancelled: {cancel.error.message}
        </p>
      )}
      <dl className="grid grid-cols-[max-content_1fr] gap-x-6 gap-y-1 text-sm">
        <dt className="text-muted-foreground">Started by</dt>
        <dd>{run.started_by}</dd>
        <dt className="text-muted-foreground">Started</dt>
        <dd>
          <time dateTime={run.created_at}>{formatTime(run.created_at)}</time>
        </dd>
        <dt className="text-muted-foreground">Took</dt>
        <dd>
          {run.finished_at === undefined
            ? "still running"
            : formatDuration(run.created_at, run.finished_at)}
        </dd>
        <dt className="text-muted-foreground">Steps</dt>
        <dd>{run.steps}</dd>
      </dl>

      <Text title="Input" text={run.input} />
      {run.output !== "" && <Text title="Output" text={run.output} />}
      {run.error !== undefined && run.error !== "" && <Text title="Error" text={run.error} />}

      <AnswerNotice outcome={outcome} />
      {waiting.length > 0 && <WaitingApprovals waiting={waiting} onOutcome={setOutcome} />}

      {events.isError && running && (
        <div role="alert" className="flex items-center gap-3 text-sm text-destructive">
          Live updates stopped: {events.error.message}
          <Button variant="outline" size="sm" onClick={() => void events.refetch()}>
            Reconnect
          </Button>
        </div>
      )}
      <Activity records={events.data ?? []} />
      <Transcript />
    </article>
  );
}

function Text({ title, text }: { title: string; text: string }) {
  return (
    <section>
      <h2 className="text-sm font-semibold">{title}</h2>
      <p className="mt-1 whitespace-pre-wrap break-words">{text}</p>
    </section>
  );
}

function WaitingApprovals({
  waiting,
  onOutcome,
}: {
  waiting: Schemas["ApprovalRequest"][];
  onOutcome: (outcome: AnswerOutcome) => void;
}) {
  const { workspace } = Route.useParams();
  const heading = useId();
  return (
    <section
      aria-labelledby={heading}
      className="rounded-md border border-amber-300 bg-amber-50 p-4 dark:border-amber-800 dark:bg-amber-950"
    >
      <h2 id={heading} className="font-semibold">
        Waiting for approval
      </h2>
      <ul className="mt-2 grid gap-3">
        {waiting.map((request) => (
          <ApprovalCard
            key={request.id}
            request={request}
            workspace={workspace}
            showRun={false}
            onOutcome={onOutcome}
          />
        ))}
      </ul>
    </section>
  );
}

const eventLabels: Record<Schemas["AuditEvent"], string> = {
  decision: "Policy decision",
  approval: "Approval",
  result: "Result",
};

function Activity({ records }: { records: Schemas["AuditRecord"][] }) {
  return (
    <section>
      <h2 className="font-semibold">Activity</h2>
      {records.length === 0 && (
        <p className="mt-1 text-sm text-muted-foreground">No tool calls yet.</p>
      )}
      <ol aria-label="Activity" className="mt-2 grid gap-3">
        {records.map((record, index) => (
          // Records have no id of their own; their order never changes.
          // biome-ignore lint/suspicious/noArrayIndexKey: see above.
          <li key={index} className="grid gap-1 border-l-2 pl-3 text-sm">
            <div className="flex flex-wrap items-baseline gap-2">
              <time dateTime={record.recorded_at} className="text-muted-foreground">
                {formatTime(record.recorded_at)}
              </time>
              <span>{eventLabels[record.event]}</span>
              <code className="font-semibold">{record.tool}</code>
              <span className="rounded bg-muted px-1.5 text-xs">{record.decision}</span>
              {record.approver !== undefined && <span>by {record.approver}</span>}
            </div>
            {record.reason !== undefined && record.reason !== "" && <p>{record.reason}</p>}
            {record.event === "decision" && record.args !== undefined && (
              <Json value={record.args} />
            )}
            {record.result !== undefined && <Json value={record.result} />}
            {record.error !== undefined && record.error !== "" && (
              <p className="text-destructive">{record.error}</p>
            )}
          </li>
        ))}
      </ol>
    </section>
  );
}

function Transcript() {
  const { workspace, runId } = Route.useParams();
  const { api } = Route.useRouteContext();
  const transcript = useQuery(transcriptQuery(api, workspace, runId));
  return (
    <section>
      <h2 className="font-semibold">Transcript</h2>
      {transcript.isError && (
        <p className="mt-1 text-sm text-destructive">
          The transcript could not be loaded: {transcript.error.message}
        </p>
      )}
      <ol aria-label="Transcript" className="mt-2 grid gap-3">
        {transcript.data?.map((message) => (
          <li key={message.position} className="grid gap-1 rounded-md border p-3 text-sm">
            <h3 className="text-xs font-semibold text-muted-foreground uppercase">
              {messageTitle(message)}
            </h3>
            {message.text !== undefined && message.text !== "" && (
              <p className="whitespace-pre-wrap break-words">{message.text}</p>
            )}
            {message.tool_calls?.map((call) => (
              <div key={call.id} className="grid gap-1">
                <span>
                  Calls <code className="font-semibold">{call.name}</code>
                </span>
                {call.args !== undefined && <Json value={call.args} />}
              </div>
            ))}
            {message.tool_results?.map((result) => (
              <div key={result.call_id} className="grid gap-1">
                {result.is_error === true && (
                  <span className="text-xs font-medium text-destructive">error</span>
                )}
                <p className="whitespace-pre-wrap break-words font-mono text-xs">
                  {result.content}
                </p>
              </div>
            ))}
          </li>
        ))}
      </ol>
    </section>
  );
}

function messageTitle(message: Schemas["TranscriptMessage"]): string {
  if (message.role === "assistant") {
    return "Model";
  }
  if (message.tool_results !== undefined && message.tool_results.length > 0) {
    return "Tool results";
  }
  return message.position === 0 ? "Input" : "User";
}
