import { useInfiniteQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { auditEventsQuery, type EventFilters } from "@/api/queries";
import { AuditHeader } from "@/components/audit-header";
import { EventsTable } from "@/components/events-table";
import { FilterForm, textFilters } from "@/components/filter-form";

/**
 * The audit log's events view, at `/audit/events`, for auditors: every event
 * of the log (`GET /v1/audit/events`), newest first, a page at a time: who
 * did what, and when, in every workspace, from tool calls to harness changes
 * and server starts.
 *
 * Like the runs view, its filters live in the URL's search parameters; an
 * event of a run links to that run's audit records.
 */

/** The text filters of the events, by search parameter, with their labels. */
const fields = [
  { name: "actor", label: "Who" },
  { name: "workspace", label: "Workspace" },
  { name: "action", label: "Action" },
  { name: "run", label: "Run" },
] as const;

/** The filters a search holds: the named text filters, trimmed, and nothing else. */
function eventFilters(search: Record<string, unknown>): EventFilters {
  return textFilters(
    search,
    fields.map((field) => field.name),
  );
}

/** Every event of the audit log, for auditors. */
export const Route = createFileRoute("/_authed/audit/events")({
  validateSearch: eventFilters,
  component: AuditEvents,
});

/** The events view: the filters and the table of events, with who acted. */
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
        // Replacing the history entry, as the runs view does.
        onFilter={(form) => void navigate({ search: eventFilters(form), replace: true })}
      />
      <EventsTable
        events={events}
        empty="No events match."
        showActor
        // An event of a run links to the run's audit records; any other
        // names its target, such as a harness.
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
