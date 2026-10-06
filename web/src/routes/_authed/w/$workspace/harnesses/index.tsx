import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { harnessesQuery } from "@/api/queries";
import { buttonVariants } from "@/components/ui/button";
import { formatTime } from "@/lib/format";

export const Route = createFileRoute("/_authed/w/$workspace/harnesses/")({
  component: Harnesses,
});

function Harnesses() {
  const { workspace } = Route.useParams();
  const { api } = Route.useRouteContext();
  const harnesses = useQuery(harnessesQuery(api, workspace));

  return (
    <section>
      <div className="flex items-center gap-4">
        <h1 className="mr-auto text-xl font-semibold">Harnesses</h1>
        <Link to="/w/$workspace/new-harness" params={{ workspace }} className={buttonVariants()}>
          New harness
        </Link>
      </div>
      {harnesses.isPending && <p className="mt-6 text-muted-foreground">Loading harnesses…</p>}
      {harnesses.isError && (
        <p role="alert" className="mt-6 text-destructive">
          The harnesses could not be loaded: {harnesses.error.message}
        </p>
      )}
      {harnesses.data?.length === 0 && (
        <p className="mt-6 text-muted-foreground">
          No harnesses yet. Create one, or apply a harness file with <code>agenty apply</code>.
        </p>
      )}
      {harnesses.data !== undefined && harnesses.data.length > 0 && (
        <table className="mt-6 w-full text-left text-sm">
          <caption className="sr-only">Harnesses</caption>
          <thead className="border-b text-muted-foreground">
            <tr>
              <th className="py-2 pr-4 font-medium">Name</th>
              <th className="py-2 pr-4 font-medium">Version</th>
              <th className="py-2 pr-4 font-medium">Model</th>
              <th className="py-2 pr-4 font-medium">Tools</th>
              <th className="py-2 font-medium">Changed</th>
            </tr>
          </thead>
          <tbody>
            {harnesses.data.map(({ harness, version, created_at }) => (
              <tr key={harness.name} className="border-b last:border-0">
                <td className="py-2 pr-4">
                  <Link
                    to="/w/$workspace/harnesses/$name"
                    params={{ workspace, name: harness.name }}
                    className="font-medium underline-offset-4 hover:underline"
                  >
                    {harness.name}
                  </Link>
                </td>
                <td className="py-2 pr-4">v{version}</td>
                <td className="py-2 pr-4">
                  {harness.model.provider} / {harness.model.name}
                </td>
                <td className="py-2 pr-4">
                  {harness.tools?.length ?? 0} {harness.tools?.length === 1 ? "tool" : "tools"}
                </td>
                <td className="py-2 whitespace-nowrap">
                  <time dateTime={created_at}>{formatTime(created_at)}</time>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}
