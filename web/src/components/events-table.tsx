import type { InfiniteData, UseInfiniteQueryResult } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { Schemas } from "@/api/client";
import { Json } from "@/components/json";
import { Button } from "@/components/ui/button";
import { formatTime } from "@/lib/format";

type Event = Schemas["AuditLogEvent"];

const labels: Record<string, string> = {
  "tool.decision": "Policy decision",
  "tool.approval": "Approval",
  "tool.result": "Tool result",
  "run.started": "Run started",
  "run.cancel_requested": "Cancel requested",
  "run.finished": "Run finished",
  "harness.changed": "Harness changed",
  "server.started": "Server started",
};

// text is a detail of an event as text, or "" when it has none.
function text(value: unknown): string {
  return typeof value === "string" || typeof value === "number" ? String(value) : "";
}

// summary says in a few words what an event's details hold; the whole of
// them is a click away.
function summary({ action, details: d }: Event): string {
  switch (action) {
    case "harness.changed":
      return `version ${text(d.version)}`;
    case "run.started":
      return `${text(d.harness)} v${text(d.version)}`;
    case "run.finished":
      return text(d.error) === "" ? text(d.status) : `${text(d.status)}: ${text(d.error)}`;
    case "tool.decision":
    case "tool.approval":
    case "tool.result":
      return `${text(d.tool)}: ${text(d.decision)}`;
    default:
      return "";
  }
}

// EventsTable lists audit log events, newest first, and loads more on
// request. Their details come from users, models and tools, so they are
// shown as text only. showActor adds who acted, showWorkspace where;
// subject links an event's run or names its target.
export function EventsTable({
  events,
  empty,
  showActor,
  showWorkspace = true,
  subject = (event) => event.run_id || event.target,
}: {
  events: UseInfiniteQueryResult<InfiniteData<Schemas["AuditLogEventList"]>>;
  empty: string;
  showActor: boolean;
  showWorkspace?: boolean;
  subject?: (event: Event) => ReactNode;
}) {
  const listed = events.data?.pages.flatMap((page) => page.events) ?? [];
  return (
    <>
      {events.isPending && <p className="text-muted-foreground">Loading events…</p>}
      {events.isError && (
        <p role="alert" className="text-destructive">
          The events could not be loaded: {events.error.message}
        </p>
      )}
      {events.isSuccess && listed.length === 0 && <p className="text-muted-foreground">{empty}</p>}
      {listed.length > 0 && (
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm" aria-busy={events.isFetching}>
            <caption className="sr-only">Events</caption>
            <thead className="border-b text-muted-foreground">
              <tr>
                <th className="py-2 pr-4 font-medium">When</th>
                {showActor && <th className="py-2 pr-4 font-medium">Who</th>}
                <th className="py-2 pr-4 font-medium">What</th>
                {showWorkspace && <th className="py-2 pr-4 font-medium">Workspace</th>}
                <th className="py-2 pr-4 font-medium">Of</th>
                <th className="py-2 font-medium">Details</th>
              </tr>
            </thead>
            <tbody>
              {listed.map((event) => (
                <tr key={event.id} className="border-b align-top last:border-0">
                  <td className="py-2 pr-4 whitespace-nowrap">
                    <time dateTime={event.recorded_at}>{formatTime(event.recorded_at)}</time>
                  </td>
                  {showActor && (
                    <td className="py-2 pr-4">
                      {event.actor === "" ? (
                        <span className="text-muted-foreground">the server</span>
                      ) : (
                        event.actor
                      )}
                    </td>
                  )}
                  <td className="py-2 pr-4 whitespace-nowrap">
                    {labels[event.action] ?? event.action}
                  </td>
                  {showWorkspace && <td className="py-2 pr-4">{event.workspace}</td>}
                  <td className="py-2 pr-4">{subject(event)}</td>
                  <td className="py-2">
                    <details>
                      <summary className="cursor-pointer wrap-anywhere">
                        {summary(event) || "Details"}
                      </summary>
                      <Json value={event.details} />
                    </details>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {events.hasNextPage && (
        <Button
          variant="outline"
          className="justify-self-start"
          aria-disabled={events.isFetchingNextPage}
          onClick={() => {
            if (!events.isFetchingNextPage) {
              void events.fetchNextPage();
            }
          }}
        >
          Load more
        </Button>
      )}
    </>
  );
}
