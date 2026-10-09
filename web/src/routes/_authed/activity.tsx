import { useInfiniteQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { myActivityQuery } from "@/api/queries";
import { EventsTable } from "@/components/events-table";

/**
 * The user's own activity, at `/activity`, reached from their name at the
 * foot of the sidebar: what they did, from the audit log (`GET
 * /v1/me/activity`): their runs' calls, their answers, cancels and harness
 * changes. Not what others did, which is the auditors' to see.
 *
 * It needs no loader: EventsTable shows its own loading and error states and
 * loads further pages on request.
 */
export const Route = createFileRoute("/_authed/activity")({
  component: Activity,
});

/** The activity page: the user's events, without a Who column, as every one is theirs. */
function Activity() {
  const { api } = Route.useRouteContext();
  const events = useInfiniteQuery(myActivityQuery(api));
  return (
    <section className="grid gap-6">
      <h1 className="text-xl font-semibold">Your activity</h1>
      <EventsTable events={events} empty="Nothing yet." showActor={false} />
    </section>
  );
}
