import { useInfiniteQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { myActivityQuery } from "@/api/queries";
import { EventsTable } from "@/components/events-table";

// What the user did, from the audit log: their runs' calls, their answers,
// cancels and harness changes. Not what others did.
export const Route = createFileRoute("/_authed/activity")({
  component: Activity,
});

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
