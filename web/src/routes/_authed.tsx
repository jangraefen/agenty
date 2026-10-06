import { createFileRoute, Outlet } from "@tanstack/react-router";
import { loadMe } from "@/auth/load-me";

// The pages for a signed-in user.
export const Route = createFileRoute("/_authed")({
  beforeLoad: ({ context }) => loadMe(context),
  component: Outlet,
});
