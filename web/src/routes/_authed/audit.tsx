import { createFileRoute, Link, notFound, Outlet } from "@tanstack/react-router";

// The audit log, for auditors only: to anyone else its pages do not exist.
// Its runs and its events are two views of it.
export const Route = createFileRoute("/_authed/audit")({
  beforeLoad: ({ context }) => {
    if (!context.me.auditor) {
      throw notFound();
    }
  },
  component: AuditLog,
});

const viewClass =
  "rounded-md px-2 py-1 text-sm hover:bg-accent aria-[current=page]:bg-accent aria-[current=page]:font-medium";

function AuditLog() {
  return (
    <div className="grid gap-6">
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
      <Outlet />
    </div>
  );
}
