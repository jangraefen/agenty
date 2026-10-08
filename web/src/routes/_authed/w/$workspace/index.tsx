import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/_authed/w/$workspace/")({
  beforeLoad: ({ params }) => {
    throw redirect({ to: "/w/$workspace/harnesses", params });
  },
});
