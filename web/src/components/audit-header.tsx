import { Link } from "@tanstack/react-router";

const viewClass =
  "rounded-md px-2 py-1 text-sm hover:bg-accent aria-[current=page]:bg-accent aria-[current=page]:font-medium";

// AuditHeader heads the audit log's two views, its runs and its events, and
// switches between them.
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
