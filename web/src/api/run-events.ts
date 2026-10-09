/**
 * The live view of a run: its server-sent event stream as a TanStack query.
 *
 * The chat (components/chat.tsx) uses runEventsQuery to show a run's tool
 * calls and decisions as they happen. The stream is read with the typed
 * client's fetch (client.ts), not the browser's EventSource, which cannot
 * send an Authorization header: the token would have to go into the URL,
 * where it would be logged. lib/sse.ts parses the body with
 * eventsource-parser. streamedQuery turns the stream into a query whose data
 * grows with every audit record, and the stream's other events invalidate
 * the queries of queries.ts that they make stale, so the rest of the page
 * follows the run without polling.
 */
// Experimental in TanStack Query 5: an upgrade may change it, which the tests show.
import { queryOptions, experimental_streamedQuery as streamedQuery } from "@tanstack/react-query";
import { parseEventStream } from "@/lib/sse";
import { type Api, type Schemas, unwrap } from "./client";
import {
  approvalsQuery,
  conversationQuery,
  recentChatsKey,
  transcriptQuery,
  withRun,
} from "./queries";

/**
 * The event stream of run id of conversation, as its audit records so far,
 * oldest first. The other events update the queries they change: an approval
 * request the waiting approvals, an audit record the transcript, and the
 * run's end its conversation. A stream that stops before the run ends fails
 * the query, keeping the records read; a refetch reads the stream again from
 * the run's start.
 *
 * Event data is JSON as the spec describes it. Records hold what tools and
 * the model wrote, so they must be rendered as text, never as HTML, which
 * Biome's noDangerouslySetInnerHtml rule enforces for the whole frontend.
 */
export function runEventsQuery(api: Api, workspace: string, conversation: string, id: string) {
  return queryOptions({
    // Not under the run's key, whose cancelling and invalidating would stop
    // or replay the stream.
    queryKey: ["workspaces", workspace, "run-events", id],
    queryFn: streamedQuery({
      streamFn: async function* ({ client, signal }) {
        // Invalidations are not awaited: the stream goes on reading while the
        // queries refetch.
        const invalidate = (queryKey: readonly unknown[]) =>
          void client.invalidateQueries({ queryKey });
        const approvals = approvalsQuery(api, workspace).queryKey;
        const transcript = transcriptQuery(api, workspace, id).queryKey;
        // The signal is the query's: the query being cancelled, or no longer
        // used, aborts the fetch and so ends the stream.
        const body = await unwrap(
          api.GET("/v1/workspaces/{workspace}/runs/{id}/events", {
            params: { path: { workspace, id } },
            parseAs: "stream",
            signal,
          }),
        );
        if (body === null) {
          throw new Error("the server sent no events");
        }
        // Events of other names, should the server add any, are skipped.
        for await (const { event, data } of parseEventStream(body)) {
          switch (event) {
            case "audit": {
              const record: Schemas["AuditRecord"] = JSON.parse(data);
              if (record.event === "approval") {
                // Answered, here or elsewhere: the run waits no longer.
                invalidate(approvals);
                invalidate(recentChatsKey());
              }
              // The model's messages land in the transcript between tool calls.
              invalidate(transcript);
              yield record;
              break;
            }
            case "approval":
              // Not yielded: the chat shows approval requests from the waiting
              // approvals query, which this refreshes.
              invalidate(approvals);
              // The run waits: its chat is marked in the recent chats.
              invalidate(recentChatsKey());
              break;
            case "finished": {
              const run: Schemas["Run"] = JSON.parse(data);
              // Not the run's own conversation_id: a server that could not
              // read the run back sends what it knows of it, without one.
              const { queryKey } = conversationQuery(api, conversation);
              // A fetch of the conversation under way may answer from before
              // the run's end.
              await client.cancelQueries({ queryKey });
              client.setQueryData(queryKey, (cached) => cached && withRun(cached, run));
              invalidate(queryKey);
              invalidate(transcript);
              invalidate(approvals);
              invalidate(recentChatsKey());
              // The run has ended: the query succeeds with the records read.
              return;
            }
          }
        }
        // The body ended without a finished event: the connection dropped or
        // the server stopped. Failing the query lets the chat say so and offer
        // to reconnect, rather than show a run that seems to have ended.
        throw new Error("the stream ended before the run did");
      },
    }),
    // Reading the stream again replays it: only a reconnect asks for that.
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });
}
