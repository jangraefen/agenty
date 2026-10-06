import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { type Api, unwrap } from "@/api/client";
import { approvalsQuery, runQuery, transcriptQuery } from "@/api/queries";
import type { components } from "@/api/schema";
import { parseEventStream } from "@/lib/sse";

type Schemas = components["schemas"];

interface RunEvents {
  /** The run's audit records so far, oldest first. */
  records: Schemas["AuditRecord"][];
  /** Why the stream stopped before the run finished, if it did. */
  error: string | null;
  /** Reads the stream again from the run's start. */
  reconnect: () => void;
}

// useRunEvents follows a run's event stream. The audit records it returns;
// the other events update the queries they change: an approval request the
// waiting approvals, a tool result the transcript, and the run's end the run.
export function useRunEvents(api: Api, workspace: string, id: string): RunEvents {
  const queryClient = useQueryClient();
  const [records, setRecords] = useState<Schemas["AuditRecord"][]>([]);
  const [error, setError] = useState<string | null>(null);
  const [connection, setConnection] = useState(0);

  // biome-ignore lint/correctness/useExhaustiveDependencies: a new connection number reads the stream again.
  useEffect(() => {
    const abort = new AbortController();
    const invalidate = (queryKey: readonly unknown[]) =>
      void queryClient.invalidateQueries({ queryKey });
    const approvals = approvalsQuery(api, workspace).queryKey;
    const transcript = transcriptQuery(api, workspace, id).queryKey;

    function handle(event: string, data: string): boolean {
      switch (event) {
        case "audit": {
          const record: Schemas["AuditRecord"] = JSON.parse(data);
          setRecords((previous) => [...previous, record]);
          if (record.event === "approval") {
            invalidate(approvals);
          } else if (record.event === "result") {
            invalidate(transcript);
          }
          return false;
        }
        case "approval":
          invalidate(approvals);
          return false;
        case "finished": {
          const run: Schemas["Run"] = JSON.parse(data);
          queryClient.setQueryData(runQuery(api, workspace, id).queryKey, run);
          invalidate(transcript);
          invalidate(approvals);
          invalidate(["workspaces", workspace, "runs"]);
          return true;
        }
        default:
          return false;
      }
    }

    async function follow() {
      const body = await unwrap(
        api.GET("/v1/workspaces/{workspace}/runs/{id}/events", {
          params: { path: { workspace, id } },
          parseAs: "stream",
          signal: abort.signal,
        }),
      );
      if (body === null) {
        throw new Error("the server sent no events");
      }
      for await (const { event, data } of parseEventStream(body)) {
        if (handle(event, data)) {
          return;
        }
      }
      throw new Error("the stream ended before the run did");
    }

    setRecords([]);
    setError(null);
    follow().catch((cause: unknown) => {
      if (!abort.signal.aborted) {
        setError(cause instanceof Error ? cause.message : String(cause));
      }
    });
    return () => {
      abort.abort();
    };
  }, [api, queryClient, workspace, id, connection]);

  return { records, error, reconnect: () => setConnection((n) => n + 1) };
}
