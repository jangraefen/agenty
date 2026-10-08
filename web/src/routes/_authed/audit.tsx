import { createFileRoute, notFound, Outlet } from "@tanstack/react-router";

// The audit log, for auditors only: to anyone else its pages do not exist.
export const Route = createFileRoute("/_authed/audit")({
  beforeLoad: ({ context }) => {
    if (!context.me.auditor) {
      throw notFound();
    }
  },
  component: Outlet,
});
