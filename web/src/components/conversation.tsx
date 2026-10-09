/**
 * One run of a conversation, as a stretch of the chat: what ConversationChat
 * (components/chat.tsx) lists, a Turn per run.
 *
 * A Turn is a run of list items in the chat's ordered list: the user's
 * input, the agent's replies (Reply), each with the tool calls it made
 * (ToolCall) and their results folded in, the run's waiting approvals
 * (WaitingApprovals, of ApprovalCards), a Working indicator while it runs,
 * how it ended if it failed or was cancelled (RunEnd), and a footer with its
 * status, steps, duration and token usage.
 *
 * Each Turn loads its own run's transcript (transcriptQuery). For the latest
 * run, the event stream invalidates it as the run records tool calls and
 * ends, so its replies appear as the model writes them, between calls.
 *
 * Everything shown here comes from the user, the model or tools, which a
 * prompt injection can shape: it is rendered as React text children only,
 * never as HTML, and model text is shown as plain text, not as rendered
 * Markdown. The Content-Security-Policy is the second line of defence.
 */
import { useQuery } from "@tanstack/react-query";
import { useRouteContext } from "@tanstack/react-router";
import { Fragment, useId } from "react";
import type { Schemas } from "@/api/client";
import { transcriptQuery, unfinished } from "@/api/queries";
import type { AnswerOutcome } from "@/components/answer-notice";
import { ApprovalCard } from "@/components/approval-card";
import { CodeBlock } from "@/components/code-block";
import { Json } from "@/components/json";
import { RunStatusBadge } from "@/components/run-status";
import { formatDuration, formatTime, formatUsage, formatUsageExactly } from "@/lib/format";
import { cn } from "@/lib/utils";

type Run = Schemas["Run"];
type Message = Schemas["TranscriptMessage"];
type ToolResult = Schemas["ToolResult"];

/**
 * Turn shows one run of a conversation as list items of a chat: the user's
 * input, the agent's replies with the tool calls they made, and how the run
 * went. Everything in it comes from the user, the model or tools, so it is
 * only ever rendered as text. waiting holds the run's approval requests;
 * onOutcome tells the page how answering one went.
 *
 * The tool results come in the transcript as messages of their own, after
 * the reply that made the calls; Turn indexes them by call ID, so each
 * ToolCall shows its result inside the reply that asked for it.
 */
export function Turn({
  run,
  workspace,
  waiting,
  onOutcome,
}: {
  run: Run;
  workspace: string;
  waiting: Schemas["ApprovalRequest"][];
  onOutcome: (outcome: AnswerOutcome) => void;
}) {
  const { api } = useRouteContext({ from: "/_authed" });
  const transcript = useQuery(transcriptQuery(api, workspace, run.id));
  // The input is the run's; the transcript, once loaded, adds the replies.
  const replies = transcript.data?.slice(1) ?? [];
  // Each result by the call it answers, for the replies to look up.
  const results = new Map<string, ToolResult>();
  for (const message of replies) {
    for (const result of message.tool_results ?? []) {
      results.set(result.call_id, result);
    }
  }
  // Whether the transcript holds the agent's answer: a reply that asks for
  // no more tool calls. Until it does, the run's output stands in for it.
  const answered = replies.some((m) => m.role === "assistant" && (m.tool_calls ?? []).length === 0);
  const running = unfinished(run.status);

  return (
    <>
      {/* The id is the target of a #run-<id> link to this run. */}
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
          message={{ position: -1, role: "assistant", text: run.output }}
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
          {Object.values(run.usage).some((n) => n > 0) && (
            <span title={formatUsageExactly(run.usage)}>{formatUsage(run.usage)}</span>
          )}
        </div>
      </li>
    </>
  );
}

/**
 * Reply is a message of the agent: its text, if any, as a chat bubble of
 * plain text, then the tool calls it asked for. The first of a run's replies
 * names the agent, by its harness; the others name it to screen readers
 * only, so each reply can be told apart when read on its own.
 */
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

/**
 * ToolCall shows a call the model asked for, folded, with its result once
 * there is one. A denied call's result is an error, as the model saw it.
 *
 * Folded, it shows only the tool's name and a state: done, error, waiting
 * while the run goes on without a result yet, or no result once the run has
 * ended without one, as when it was cancelled before the result came. A
 * native details element folds it, which needs no state of its own and
 * keeps the arguments and result out of the way of the conversation.
 */
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

/**
 * RunEnd says that a run failed or was cancelled, with the server's error,
 * which says why it failed or who cancelled it. The composer below then
 * offers to continue from where the run stopped.
 */
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

/**
 * WaitingApprovals holds the run's calls that wait for approval, each as an
 * ApprovalCard, in a highlighted section in place in the chat, where the
 * call that needs it was made, rather than on a separate page.
 */
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
            onOutcome={onOutcome}
          />
        ))}
      </ul>
    </section>
  );
}

/**
 * breakable lets a tool name wrap after its underscores, as in
 * files_read_text_file, before it wraps anywhere else: it splits the name
 * and puts a <wbr> after each underscore, keeping the name as text.
 */
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
