import { useInfiniteQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { auditEventsQuery, type EventFilters } from "@/api/queries";
import { AuditHeader } from "@/components/audit-header";
import { EventsTable } from "@/components/events-table";
import { FilterForm, textFilters } from "@/components/filter-form";

const fields = [
  { name: "actor", label: "Who" },
  { name: "workspace", label: "Workspace" },
  { name: "action", label: "Action" },
  { name: "run", label: "Run" },
] as const;

function eventFilters(search: Record<string, unknown>): EventFilters {
  return textFilters(
    search,
    fields.map((field) => field.name),
  );
}

// Every event of the audit log, for auditors: who did what, and when.
export const Route = createFileRoute("/_authed/audit/events")({
  validateSearch: eventFilters,
  component: AuditEvents,
});

function AuditEvents() {
  const filters = eventFilters(Route.useSearch());
  const { api } = Route.useRouteContext();
  const navigate = Route.useNavigate();
  const events = useInfiniteQuery(auditEventsQuery(api, filters));
  return (
    <section className="grid gap-6">
      <AuditHeader />
      <FilterForm
        fields={fields}
        values={filters}
        onFilter={(form) => void navigate({ search: eventFilters(form), replace: true })}
      />
      <EventsTable
        events={events}
        empty="No events match."
        showActor
        subject={(event) =>
          event.run_id === "" ? (
            event.target
          ) : (
            <Link
              to="/audit/runs/$runId"
              params={{ runId: event.run_id }}
              className="underline-offset-4 hover:underline"
            >
              {event.run_id}
            </Link>
          )
        }
      />
    </section>
  );
}
