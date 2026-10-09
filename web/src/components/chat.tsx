/**
 * The chat of a conversation: the page `/c/{conversation}` shows
 * (routes/_authed/c.$conversationId.tsx).
 *
 * ConversationChat composes it from three parts:
 *
 * - Header: the conversation's title, harness and workspace, and Cancel run
 *   while its latest run is under way.
 * - Chat: every run as a Turn (components/conversation.tsx), oldest first,
 *   the latest run's waiting approvals in place, how the last answer went
 *   (AnswerNotice), and a Reconnect button when live updates stop.
 * - Composer: the MessageBox that follows up the latest run once it has
 *   finished; in a workspace the user left, a note that the chat is read
 *   only takes its place.
 *
 * Data flow. The conversation with its runs is one query
 * (conversationQuery), which the route's loader filled. Only its latest run
 * can still change, so only that run is followed live: runEventsQuery reads
 * its event stream and, as events arrive, invalidates the queries they
 * change: the run's transcript (each Turn's messages), the workspace's
 * waiting approvals, the recent chats in the sidebar, and, when the run
 * finishes, the conversation itself, which carries the run's final status.
 * A new latest run, after a reply, gives runEventsQuery a new key, so the
 * stream of the run before is left and the new one's is read.
 *
 * Writes go through the workspace's endpoints (cancel, follow-up, answering
 * an approval), as a conversation is continued, cancelled and answered only
 * in its workspace; reading it needs no membership, which is why a chat of a
 * workspace the user left still shows, read only.
 */
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
import { approvalsQuery, cacheStartedRun, conversationQuery, unfinished } from "@/api/queries";
import { runEventsQuery } from "@/api/run-events";
import { AnswerNotice, type AnswerOutcome } from "@/components/answer-notice";
import { Turn } from "@/components/conversation";
import { MessageBox } from "@/components/message-box";
import { Button } from "@/components/ui/button";

type Run = Schemas["Run"];

/**
 * ConversationChat shows the conversation named by its first run, id, as a
 * chat: every run of it, oldest first, each a message from its user and the
 * agent's replies. The latest run is followed live, and a reply follows it up
 * with a new run. The page scrolls to the run focus names, if any.
 *
 * It reads the conversation with useSuspenseQuery, as the route's loader has
 * it in the cache already, and opens the latest run's event stream with
 * useQuery, whose data, the run's audit records, matter here only for the
 * stream's state: the stream's effect is the invalidations it makes.
 */
export function ConversationChat({ id, focus }: { id: string; focus?: string | undefined }) {
  const { api, me } = useRouteContext({ from: "/_authed" });
  // The latest run's events keep the conversation current.
  const conversation = useSuspenseQuery(conversationQuery(api, id));
  const { workspace, runs } = conversation.data;
  // The chat of a workspace the user left is read-only: the server would
  // refuse its writes, so the page offers none (no reply, cancel or answer,
  // no link to the harness's management page).
  const readOnly = !me.workspaces.includes(workspace);
  const latest = latestRun(conversation.data);
  const events = useQuery(runEventsQuery(api, workspace, id, latest.id));

  // A URL with a #run-<id> hash opens the chat at that run rather than at
  // its top.
  useEffect(() => {
    if (focus !== undefined) {
      // jsdom does not scroll.
      document.getElementById(`run-${focus}`)?.scrollIntoView?.({ block: "start" });
    }
  }, [focus]);

  return (
    <article className="mx-auto grid max-w-3xl gap-4">
      {conversation.isError && (
        <p role="alert" className="text-sm text-destructive">
          The conversation could not be refreshed: {conversation.error.message}
        </p>
      )}
      <Header
        workspace={workspace}
        conversation={conversation.data}
        latest={latest}
        readOnly={readOnly}
        // A new latest run gets a fresh Cancel button: the state of
        // cancelling the run before does not carry over to it.
        key={latest.id}
      />
      <Chat workspace={workspace} runs={runs} latest={latest} events={events} />
      {readOnly ? (
        <p className="border-t pt-3 text-sm text-muted-foreground">
          You are no longer a member of {workspace}: this chat is read-only.
        </p>
      ) : (
        <Composer workspace={workspace} conversation={conversation.data} latest={latest} />
      )}
    </article>
  );
}

/**
 * latestRun is the conversation's latest run; a conversation always has one,
 * as it is made by its first run, so an empty one is a bug and throws, which
 * the route's error page shows.
 */
function latestRun(conversation: Schemas["Conversation"]): Run {
  const latest = conversation.runs.at(-1);
  if (latest === undefined) {
    throw new Error(`conversation ${conversation.id} has no runs`);
  }
  return latest;
}

/**
 * Header heads the chat with the conversation's title, its harness and its
 * workspace, and offers Cancel run while the latest run has yet to end.
 *
 * Cancelling only asks the server (`POST .../runs/{id}/cancel`); the run's
 * end, and so the button's leaving, comes through the event stream as for
 * any other end. Until then the button ignores clicks, saying Cancelling
 * once the server took the request; a failed request lets it be tried again.
 * The harness links to its management page, except in a read-only chat,
 * whose workspace's pages the user can no longer open.
 */
function Header({
  workspace,
  conversation,
  latest,
  readOnly,
}: {
  workspace: string;
  conversation: Schemas["ConversationSummary"];
  latest: Run;
  readOnly: boolean;
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
            {readOnly ? (
              conversation.harness
            ) : (
              <Link
                to="/w/$workspace/harnesses/$name"
                params={{ workspace, name: conversation.harness }}
                className="underline-offset-4 hover:underline"
              >
                {conversation.harness}
              </Link>
            )}{" "}
            · {workspace}
          </p>
        </div>
        {running && !readOnly && (
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

/**
 * Chat lists the conversation's runs as turns, with the latest run's waiting
 * approvals in place, and tells how the last answer to one went.
 *
 * The approvals come from the workspace's list of waiting requests
 * (approvalsQuery), filtered to the latest run: an earlier run has ended, so
 * none of its requests can still wait. The list is only fetched while the
 * latest run is under way; the event stream refreshes it when the run asks
 * for an approval, gets an answer or ends.
 *
 * When live updates stop before the run ends, the stream's query fails and
 * a Reconnect button refetches it, which reads the stream again from the
 * run's start.
 */
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
  // Reset while rendering, React's pattern for state derived from a prop,
  // rather than in an effect, which would first show the stale notice.
  const [answered, setAnswered] = useState(latest.id);
  if (answered !== latest.id) {
    setAnswered(latest.id);
    setOutcome(null);
  }

  return (
    <>
      {/* Announces the latest run's status, and calls waiting, as they change. */}
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
      {/* A stream that failed for a run since ended has nothing left to tell. */}
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

/**
 * Composer replies to the conversation by following up its latest run, once
 * that run has finished. A run that failed or was cancelled is continued from
 * where it stopped. Enter sends, Shift+Enter starts a new line.
 *
 * A reply posts `.../runs/{id}/follow-up` for the latest run, and
 * cacheStartedRun adds the new run to the cached conversation at once, which
 * makes it the latest and so the one the chat follows live. The box stays
 * editable while a run is under way, so the next message can be written, but
 * sending is blocked until the run ends, as the server follows up only a
 * finished run.
 */
function Composer({
  workspace,
  conversation,
  latest,
}: {
  workspace: string;
  conversation: Schemas["Conversation"];
  latest: Run;
}) {
  const { api } = useRouteContext({ from: "/_authed" });
  const queryClient = useQueryClient();
  const [text, setText] = useState("");
  const box = useRef<HTMLTextAreaElement>(null);
  // Arriving from the new chat page, whose first message started this
  // conversation, the focus moves here, so the user can write on.
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
      // Only a sent reply clears the box; a failed one keeps the text to
      // send again.
      setText("");
      cacheStartedRun(queryClient, api, workspace, run, conversation);
      box.current?.focus();
    },
    onError: (error) => {
      // Someone replied first: show their run, which is now the latest.
      if (error instanceof ApiError && error.status === 409) {
        void queryClient.invalidateQueries({
          queryKey: conversationQuery(api, conversation.id).queryKey,
        });
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
      blocked={waiting || reply.isPending}
      invalid={reply.isError}
      placeholder="Write a reply…"
      notes={notes}
    />
  );
}
