import { createFileRoute, notFound, Outlet } from "@tanstack/react-router";

/**
 * The layout of the audit log, at `/audit`, for auditors only: to anyone
 * else its pages do not exist. Its children are the runs of every workspace
 * (audit/index.tsx), a run's audit records (audit/runs.$runId.tsx) and every
 * event of the log (audit/events.tsx).
 *
 * The check reads `me.auditor` from the context the authed layout loaded, so
 * it runs before any child asks for audit data. It is a convenience, not the
 * control: the server refuses the audit endpoints to a user who is not an
 * auditor. A not-found, rather than a "forbidden" page, matches the sidebar,
 * which shows its Compliance section only to auditors.
 */
export const Route = createFileRoute("/_authed/audit")({
  beforeLoad: ({ context }) => {
    if (!context.me.auditor) {
      throw notFound();
    }
  },
  component: Outlet,
});
