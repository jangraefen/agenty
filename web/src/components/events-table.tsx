import type { InfiniteData, UseInfiniteQueryResult } from "@tanstack/react-query";
import { type ReactNode, useId, useState } from "react";
import { LuChevronRight } from "react-icons/lu";
import type { Schemas } from "@/api/client";
import { Json } from "@/components/json";
import { LoadMore } from "@/components/load-more";
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
// subject links an event's run or names its target. limit shows only the
// newest events, without loading more.
export function EventsTable({
  events,
  empty,
  showActor,
  showWorkspace = true,
  subject = (event) => event.run_id || event.target,
  limit,
}: {
  events: UseInfiniteQueryResult<InfiniteData<Schemas["AuditLogEventList"]>>;
  empty: string;
  showActor: boolean;
  showWorkspace?: boolean;
  subject?: (event: Event) => ReactNode;
  limit?: number;
}) {
  const listed = (events.data?.pages.flatMap((page) => page.events) ?? []).slice(0, limit);
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
                <th className="w-0 py-2 pr-2">
                  <span className="sr-only">Show details</span>
                </th>
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
                <EventRow
                  key={event.id}
                  event={event}
                  showActor={showActor}
                  showWorkspace={showWorkspace}
                  subject={subject}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}
      {limit === undefined && (
        <LoadMore query={events} variant="outline" className="justify-self-start" />
      )}
    </>
  );
}

// EventRow is an event in a row of its own; a toggle at its start shows
// the whole of its details in a row below, as wide as the table. The
// toggle is named after the row's cells, so its name says which event it
// opens.
function EventRow({
  event,
  showActor,
  showWorkspace,
  subject,
}: {
  event: Event;
  showActor: boolean;
  showWorkspace: boolean;
  subject: (event: Event) => ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const id = useId();
  const columns = 4 + Number(showActor) + Number(showWorkspace);
  const labelledBy = ["toggle", "what", ...(showWorkspace ? ["workspace"] : []), "of", "summary"]
    .map((cell) => `${id}-${cell}`)
    .join(" ");
  return (
    <>
      <tr className={open ? "align-top" : "border-b align-top last:border-0"}>
        <td className="py-1 pr-2">
          <button
            type="button"
            aria-expanded={open}
            aria-controls={id}
            aria-labelledby={labelledBy}
            onClick={() => setOpen(!open)}
            className="rounded-sm p-1 text-muted-foreground hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
          >
            <LuChevronRight
              aria-hidden="true"
              className={`size-4 transition-transform ${open ? "rotate-90" : ""}`}
            />
            <span id={`${id}-toggle`} className="sr-only">
              Details of
            </span>
          </button>
        </td>
        <td className="py-2 pr-4 whitespace-nowrap">
          <time dateTime={event.recorded_at}>{formatTime(event.recorded_at)}</time>
        </td>
        {showActor && (
          <td className="py-2 pr-4 whitespace-nowrap">
            {event.actor === "" ? (
              <span className="text-muted-foreground">the server</span>
            ) : (
              event.actor
            )}
          </td>
        )}
        <td id={`${id}-what`} className="py-2 pr-4 whitespace-nowrap">
          {labels[event.action] ?? event.action}
        </td>
        {showWorkspace && (
          <td id={`${id}-workspace`} className="py-2 pr-4">
            {event.workspace}
          </td>
        )}
        <td id={`${id}-of`} className="py-2 pr-4">
          {subject(event)}
        </td>
        <td id={`${id}-summary`} className="py-2 wrap-anywhere">
          {summary(event)}
        </td>
      </tr>
      {open && (
        <tr id={id} className="border-b last:border-0">
          <td />
          <td colSpan={columns} className="pb-3">
            <Json value={event.details} />
          </td>
        </tr>
      )}
    </>
  );
}
