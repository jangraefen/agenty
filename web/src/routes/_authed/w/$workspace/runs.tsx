import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_authed/w/$workspace/runs")({
  component: Runs,
});

function Runs() {
  return <h1 className="text-xl font-semibold">Runs</h1>;
}
