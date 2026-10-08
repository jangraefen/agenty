// Experimental in TanStack Query 5: an upgrade may change it, which the tests show.
import { queryOptions, experimental_streamedQuery as streamedQuery } from "@tanstack/react-query";
import { parseEventStream } from "@/lib/sse";
import { type Api, type Schemas, unwrap } from "./client";
import {
  approvalsQuery,
  conversationsKey,
  recentChatsKey,
  runQuery,
  transcriptQuery,
} from "./queries";

// A run's event stream, as its audit records so far, oldest first. The other
// events update the queries they change: an approval request the waiting
// approvals, an audit record the transcript, and the run's end the run and
// its conversation. A
// stream that stops before the run ends fails the query, keeping the records
// read; a refetch reads the stream again from the run's start.
export function runEventsQuery(api: Api, workspace: string, id: string) {
  return queryOptions({
    // Not under the run's key, whose cancelling and invalidating would stop
    // or replay the stream.
    queryKey: ["workspaces", workspace, "run-events", id],
    queryFn: streamedQuery({
      streamFn: async function* ({ client, signal }) {
        const invalidate = (queryKey: readonly unknown[]) =>
          void client.invalidateQueries({ queryKey });
        const approvals = approvalsQuery(api, workspace).queryKey;
        const transcript = transcriptQuery(api, workspace, id).queryKey;
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
        for await (const { event, data } of parseEventStream(body)) {
          switch (event) {
            case "audit": {
              const record: Schemas["AuditRecord"] = JSON.parse(data);
              if (record.event === "approval") {
                invalidate(approvals);
              }
              // The model's messages land in the transcript between tool calls.
              invalidate(transcript);
              yield record;
              break;
            }
            case "approval":
              invalidate(approvals);
              break;
            case "finished": {
              const { queryKey } = runQuery(api, workspace, id);
              // A fetch of the run under way may answer from before its end.
              await client.cancelQueries({ queryKey });
              client.setQueryData(queryKey, JSON.parse(data));
              invalidate(transcript);
              invalidate(approvals);
              invalidate(recentChatsKey());
              invalidate(conversationsKey(workspace));
              return;
            }
          }
        }
        throw new Error("the stream ended before the run did");
      },
    }),
    // Reading the stream again replays it: only a reconnect asks for that.
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });
}
