import {
  type UseQueryResult,
  useMutation,
  useQuery,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { Link, useLocation, useRouteContext } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { ApiError, type Schemas, unwrap } from "@/api/client";
import {
  approvalsQuery,
  cacheStartedRun,
  conversationQuery,
  conversationsKey,
  runQuery,
  unfinished,
} from "@/api/queries";
import { runEventsQuery } from "@/api/run-events";
import { AnswerNotice, type AnswerOutcome } from "@/components/answer-notice";
import { Turn } from "@/components/conversation";
import { MessageBox } from "@/components/message-box";
import { Button } from "@/components/ui/button";

type Run = Schemas["Run"];

// ConversationChat shows a conversation, named by its first run, as a chat:
// every run of it, oldest first, each a message from its user and the
// agent's replies. The latest run is followed live, and a reply follows it up
// with a new run. The page scrolls to the run focus names, if any, once the
// conversation is known.
export function ConversationChat({
  conversation: summary,
  focus,
}: {
  conversation: Schemas["ConversationSummary"];
  focus?: string | undefined;
}) {
  const { api } = useRouteContext({ from: "/_authed" });
  const { workspace } = summary;
  const run = useSuspenseQuery(runQuery(api, workspace, summary.id));
  const conversation = useQuery(conversationQuery(api, workspace, summary.id));
  const listed = conversation.data ?? [run.data];
  const last = listed.at(-1) ?? run.data;
  // The latest run as its own query, which its events keep current.
  const latest = useQuery({ ...runQuery(api, workspace, last.id), placeholderData: last });
  const current = latest.data ?? last;
  const runs = listed.map((r) => (r.id === current.id ? current : r));
  // Until the conversation is known, its latest run is not: following the
  // first run before then could follow the wrong one.
  const events = useQuery({
    ...runEventsQuery(api, workspace, current.id),
    enabled: !conversation.isPending,
  });

  const known = conversation.isSuccess;
  useEffect(() => {
    if (focus !== undefined && known) {
      // jsdom does not scroll.
      document.getElementById(`run-${focus}`)?.scrollIntoView?.({ block: "start" });
    }
  }, [focus, known]);

  return (
    <article className="mx-auto grid max-w-3xl gap-4">
      {run.isError && (
        <p role="alert" className="text-sm text-destructive">
          The first run could not be refreshed: {run.error.message}
        </p>
      )}
      {conversation.isError && (
        <p role="alert" className="text-sm text-destructive">
          The rest of the conversation could not be loaded: {conversation.error.message}
        </p>
      )}
      <Header workspace={workspace} conversation={summary} latest={current} key={current.id} />
      <Chat workspace={workspace} runs={runs} latest={current} events={events} />
      <Composer
        workspace={workspace}
        runs={runs}
        latest={current}
        ready={!conversation.isPending}
      />
    </article>
  );
}

function Header({
  workspace,
  conversation,
  latest,
}: {
  workspace: string;
  conversation: Schemas["ConversationSummary"];
  latest: Run;
}) {
  const { api } = useRouteContext({ from: "/_authed" });
  const running = unfinished(latest.status);
  const cancel = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/v1/workspaces/{workspace}/runs/{id}/cancel", {
          params: { path: { workspace, id: latest.id } },
        }),
      ),
  });
  return (
    <>
      <header className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b pb-3">
        <div className="min-w-0">
          <h1 className="truncate text-xl font-semibold">{conversation.title}</h1>
          <p className="text-sm text-muted-foreground">
            <Link
              to="/w/$workspace/harnesses/$name"
              params={{ workspace, name: conversation.harness }}
              className="underline-offset-4 hover:underline"
            >
              {conversation.harness}
            </Link>{" "}
            · {workspace}
          </p>
        </div>
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
      {cancel.isError && (
        <p role="alert" className="text-sm text-destructive">
          The run could not be cancelled: {cancel.error.message}
        </p>
      )}
    </>
  );
}

function Chat({
  workspace,
  runs,
  latest,
  events,
}: {
  workspace: string;
  runs: Run[];
  latest: Run;
  events: UseQueryResult<Schemas["AuditRecord"][]>;
}) {
  const { api } = useRouteContext({ from: "/_authed" });
  const running = unfinished(latest.status);
  const approvals = useQuery({ ...approvalsQuery(api, workspace), enabled: running });
  const waiting = running
    ? (approvals.data?.filter((request) => request.run_id === latest.id) ?? [])
    : [];
  const [outcome, setOutcome] = useState<AnswerOutcome | null>(null);
  // An answer is news about the run it was given in, not a later one.
  const [answered, setAnswered] = useState(latest.id);
  if (answered !== latest.id) {
    setAnswered(latest.id);
    setOutcome(null);
  }

  return (
    <>
      <p role="status" className="sr-only">
        The latest run is {latest.status}.
        {waiting.length > 0 &&
          ` ${waiting.length} ${waiting.length === 1 ? "call waits" : "calls wait"} for approval.`}
      </p>
      <ol aria-label="Conversation" className="grid gap-4">
        {runs.map((run) => (
          <Turn
            key={run.id}
            run={run}
            workspace={workspace}
            waiting={run.id === latest.id ? waiting : []}
            onOutcome={setOutcome}
          />
        ))}
      </ol>
      <AnswerNotice outcome={outcome} />
      {events.isError && running && (
        <div role="alert" className="flex items-center gap-3 text-sm text-destructive">
          Live updates stopped: {events.error.message}
          <Button variant="outline" size="sm" onClick={() => void events.refetch()}>
            Reconnect
          </Button>
        </div>
      )}
    </>
  );
}

// Composer replies to the conversation by following up its latest run, once
// that run has finished and the conversation is known, so it is the latest.
// A run that failed or was cancelled is continued from where it stopped.
// Enter sends, Shift+Enter starts a new line.
function Composer({
  workspace,
  runs,
  latest,
  ready,
}: {
  workspace: string;
  runs: Run[];
  latest: Run;
  ready: boolean;
}) {
  const { api } = useRouteContext({ from: "/_authed" });
  const queryClient = useQueryClient();
  const [text, setText] = useState("");
  const box = useRef<HTMLTextAreaElement>(null);
  const focusOnArrival = useLocation({
    select: (location) => location.state.focusMessage === true,
  });
  useEffect(() => {
    if (focusOnArrival) {
      box.current?.focus();
    }
  }, [focusOnArrival]);
  const reply = useMutation({
    mutationFn: (input: string) =>
      unwrap(
        api.POST("/v1/workspaces/{workspace}/runs/{id}/follow-up", {
          params: { path: { workspace, id: latest.id } },
          body: { input },
        }),
      ),
    onSuccess: (run) => {
      setText("");
      cacheStartedRun(queryClient, api, workspace, run, runs);
      box.current?.focus();
    },
    onError: (error) => {
      // Someone replied first: show their run, which is now the latest.
      if (error instanceof ApiError && error.status === 409) {
        void queryClient.invalidateQueries({ queryKey: conversationsKey(workspace) });
      }
    },
  });

  const waiting = unfinished(latest.status);
  const ended = latest.status === "failed" || latest.status === "cancelled";
  const notes = {
    waiting: waiting && (
      <p className="text-xs text-muted-foreground">You can reply once the agent has answered.</p>
    ),
    ended: ended && (
      <p className="text-xs text-muted-foreground">
        The last run {latest.status === "failed" ? "failed" : "was cancelled"}. A reply continues
        from where it stopped.
      </p>
    ),
    error: reply.isError && (
      <p role="alert" className="text-sm text-destructive">
        The reply could not be sent: {reply.error.message}
      </p>
    ),
  };

  return (
    <MessageBox
      ref={box}
      value={text}
      onChange={setText}
      onSend={(message) => reply.mutate(message)}
      blocked={waiting || !ready || reply.isPending}
      invalid={reply.isError}
      placeholder="Write a reply…"
      notes={notes}
    />
  );
}
