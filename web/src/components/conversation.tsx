import { useQuery } from "@tanstack/react-query";
import { useRouteContext } from "@tanstack/react-router";
import { Fragment, useId, useState } from "react";
import type { Schemas } from "@/api/client";
import { auditQuery, transcriptQuery } from "@/api/queries";
import type { AnswerOutcome } from "@/components/answer-notice";
import { ApprovalCard } from "@/components/approval-card";
import { CodeBlock } from "@/components/code-block";
import { Json } from "@/components/json";
import { RunStatusBadge } from "@/components/run-status";
import { formatDuration, formatTime, formatUsage } from "@/lib/format";
import { cn } from "@/lib/utils";

type Run = Schemas["Run"];
type Message = Schemas["TranscriptMessage"];
type ToolResult = Schemas["ToolResult"];

// Turn shows one run of a conversation as list items of a chat: the user's
// input, the agent's replies with the tool calls they made, and how the run
// went. Everything in it comes from the user, the model or tools, so it is
// only ever rendered as text.
//
// live holds the audit records of the conversation's latest run, from its
// event stream; any other run reads its audit log when it is opened. waiting
// holds the run's approval requests.
export function Turn({
  run,
  workspace,
  live,
  waiting,
  onOutcome,
}: {
  run: Run;
  workspace: string;
  live: Schemas["AuditRecord"][] | undefined;
  waiting: Schemas["ApprovalRequest"][];
  onOutcome: (outcome: AnswerOutcome) => void;
}) {
  const { api } = useRouteContext({ from: "/_authed/w/$workspace" });
  const transcript = useQuery(transcriptQuery(api, workspace, run.id));
  // The input is the run's; the transcript, once loaded, adds the replies.
  const replies = transcript.data?.slice(1) ?? [];
  const results = new Map<string, ToolResult>();
  for (const message of replies) {
    for (const result of message.tool_results ?? []) {
      results.set(result.call_id, result);
    }
  }
  const answered = replies.some((m) => m.role === "assistant" && (m.tool_calls ?? []).length === 0);
  const running = run.status === "running";

  return (
    <>
      <li id={`run-${run.id}`} data-from="user" className="grid justify-items-end gap-1">
        <span className="text-xs text-muted-foreground">
          {run.started_by} · <time dateTime={run.created_at}>{formatTime(run.created_at)}</time>
        </span>
        <p className="max-w-[85%] rounded-2xl rounded-br-sm bg-primary px-4 py-2 whitespace-pre-wrap wrap-anywhere text-primary-foreground">
          {run.input}
        </p>
      </li>
      {replies
        .filter((m) => m.role === "assistant")
        .map((message, index) => (
          <Reply
            key={message.position}
            first={index === 0}
            harness={run.harness}
            message={message}
            results={results}
            running={running}
          />
        ))}
      {!answered && run.output !== "" && (
        // The transcript is not loaded, or could not be.
        <Reply
          first={replies.length === 0}
          harness={run.harness}
          message={{ position: -1, role: "assistant", text: run.output, created_at: "" }}
          results={results}
          running={false}
        />
      )}
      {transcript.isError && (
        <li className="text-sm text-destructive">
          The messages of this run could not be loaded: {transcript.error.message}
        </li>
      )}
      {waiting.length > 0 && (
        <li>
          <WaitingApprovals waiting={waiting} workspace={workspace} onOutcome={onOutcome} />
        </li>
      )}
      {running && (
        <li className="flex items-center gap-2 text-sm text-muted-foreground">
          <span aria-hidden="true" className="flex gap-1">
            <span className="size-1.5 animate-bounce rounded-full bg-current" />
            <span className="size-1.5 animate-bounce rounded-full bg-current [animation-delay:150ms]" />
            <span className="size-1.5 animate-bounce rounded-full bg-current [animation-delay:300ms]" />
          </span>
          Working…
        </li>
      )}
      {(run.status === "failed" || run.status === "cancelled") && <RunEnd run={run} />}
      <li className="grid gap-1 border-b pb-4 text-xs text-muted-foreground">
        <div className="flex flex-wrap items-center gap-2">
          <RunStatusBadge status={run.status} />
          <span>
            {run.steps} {run.steps === 1 ? "step" : "steps"}
          </span>
          {run.finished_at !== undefined && (
            <span>took {formatDuration(run.created_at, run.finished_at)}</span>
          )}
          {Object.values(run.usage).some((n) => n > 0) && <span>{formatUsage(run.usage)}</span>}
        </div>
        <AuditLog run={run} workspace={workspace} live={live} />
      </li>
    </>
  );
}

// Reply is a message of the agent. The first of a run's replies names the
// agent; the others name it to screen readers only.
function Reply({
  first,
  harness,
  message,
  results,
  running,
}: {
  first: boolean;
  harness: string;
  message: Message;
  results: Map<string, ToolResult>;
  running: boolean;
}) {
  const text = message.text ?? "";
  const calls = message.tool_calls ?? [];
  return (
    <li data-from="agent" className="grid justify-items-start gap-1">
      <span className={cn("text-xs font-medium text-muted-foreground", !first && "sr-only")}>
        {harness}
      </span>
      {text !== "" && (
        <p className="max-w-[85%] rounded-2xl rounded-bl-sm bg-muted px-4 py-2 whitespace-pre-wrap wrap-anywhere">
          {text}
        </p>
      )}
      {calls.length > 0 && (
        <div className="grid w-full max-w-[85%] gap-1">
          {calls.map((call) => (
            <ToolCall key={call.id} call={call} result={results.get(call.id)} running={running} />
          ))}
        </div>
      )}
    </li>
  );
}

// ToolCall shows a call the model asked for, folded, with its result once
// there is one. A denied call's result is an error, as the model saw it.
function ToolCall({
  call,
  result,
  running,
}: {
  call: Schemas["ToolCall"];
  result: ToolResult | undefined;
  running: boolean;
}) {
  const id = useId();
  let state = "no result";
  if (result !== undefined) {
    state = result.is_error === true ? "error" : "done";
  } else if (running) {
    state = "waiting";
  }
  return (
    <details aria-labelledby={id} className="group rounded-lg border text-sm">
      <summary
        id={id}
        className="flex cursor-pointer list-none items-center gap-2 px-3 py-1.5 [&::-webkit-details-marker]:hidden"
      >
        <span aria-hidden="true" className="text-muted-foreground transition group-open:rotate-90">
          ›
        </span>
        <span className="text-muted-foreground">Called</span>
        <code className="min-w-0 font-semibold wrap-anywhere">{breakable(call.name)}</code>
        <span
          className={cn(
            "ml-auto shrink-0 rounded px-1.5 text-xs",
            state === "error"
              ? "bg-red-100 text-red-900 dark:bg-red-950 dark:text-red-200"
              : "bg-muted",
          )}
        >
          {state}
        </span>
      </summary>
      <div className="grid gap-2 border-t px-3 py-2">
        <span className="text-xs font-medium text-muted-foreground">Arguments</span>
        <Json value={call.args ?? {}} />
        {result !== undefined && (
          <>
            <span className="text-xs font-medium text-muted-foreground">Result</span>
            <CodeBlock className={cn(result.is_error === true && "text-destructive")}>
              {result.content}
            </CodeBlock>
          </>
        )}
      </div>
    </details>
  );
}

function RunEnd({ run }: { run: Run }) {
  const id = useId();
  return (
    <li
      aria-labelledby={id}
      className="rounded-md border border-red-300 bg-red-50 px-4 py-3 text-sm dark:border-red-900 dark:bg-red-950"
    >
      <h2 id={id} className="font-semibold">
        {run.status === "failed" ? "The run failed" : "The run was cancelled"}
      </h2>
      {run.error !== undefined && run.error !== "" && (
        <p className="mt-1 whitespace-pre-wrap wrap-anywhere">{run.error}</p>
      )}
    </li>
  );
}

function WaitingApprovals({
  waiting,
  workspace,
  onOutcome,
}: {
  waiting: Schemas["ApprovalRequest"][];
  workspace: string;
  onOutcome: (outcome: AnswerOutcome) => void;
}) {
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

// AuditLog shows what the gateway recorded for the run's tool calls, folded.
// A run other than the live one reads its log once it is opened.
function AuditLog({
  run,
  workspace,
  live,
}: {
  run: Run;
  workspace: string;
  live: Schemas["AuditRecord"][] | undefined;
}) {
  const { api } = useRouteContext({ from: "/_authed/w/$workspace" });
  const [open, setOpen] = useState(false);
  const stored = useQuery({
    ...auditQuery(api, workspace, run.id),
    enabled: open && live === undefined,
  });
  const records = live ?? stored.data ?? [];
  return (
    <details onToggle={(event) => setOpen(event.currentTarget.open)}>
      <summary className="cursor-pointer">Audit log</summary>
      {stored.isError && (
        <p className="mt-1 text-destructive">
          The audit log could not be loaded: {stored.error.message}
        </p>
      )}
      {records.length === 0 && !stored.isFetching && (
        <p className="mt-1">No tool calls{run.status === "running" ? " yet" : ""}.</p>
      )}
      <ol aria-label="Audit log" className="mt-2 grid gap-3 text-foreground">
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
    </details>
  );
}

// breakable lets a tool name wrap after its underscores, as in
// files_read_text_file, before it wraps anywhere else.
function breakable(name: string) {
  return name.split("_").map((part, i, parts) => (
    // biome-ignore lint/suspicious/noArrayIndexKey: the parts never reorder.
    <Fragment key={i}>
      {part}
      {i < parts.length - 1 && (
        <>
          _<wbr />
        </>
      )}
    </Fragment>
  ));
}
