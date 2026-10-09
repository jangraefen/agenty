import { Link } from "@tanstack/react-router";

/**
 * The heading of the auditors' audit log (`/audit`), with its two views as
 * tabs of links: the runs of every workspace and every event.
 */

/** The classes of a view's link; the router marks the current view's with aria-current. */
const viewClass =
  "rounded-md px-2 py-1 text-sm hover:bg-accent aria-[current=page]:bg-accent aria-[current=page]:font-medium";

/**
 * AuditHeader heads the audit log's two views, its runs and its events, and
 * switches between them. Each link is current whatever the view's filters,
 * which switching drops, as the two views filter differently.
 */
export function AuditHeader() {
  return (
    <header className="flex flex-wrap items-center gap-4">
      <h1 className="text-xl font-semibold">Audit log</h1>
      <nav aria-label="Audit log views" className="flex gap-1">
        <Link
          to="/audit"
          activeOptions={{ exact: true, includeSearch: false }}
          className={viewClass}
        >
          Runs
        </Link>
        <Link to="/audit/events" activeOptions={{ includeSearch: false }} className={viewClass}>
          Events
        </Link>
      </nav>
    </header>
  );
}
