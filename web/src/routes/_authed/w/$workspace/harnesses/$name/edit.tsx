import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { harnessQuery } from "@/api/queries";
import { HarnessForm } from "@/components/harness-form";
import { useSaveHarness } from "@/hooks/use-save-harness";
import { fromHarness } from "@/lib/harness-form";

export const Route = createFileRoute("/_authed/w/$workspace/harnesses/$name/edit")({
  component: EditHarness,
});

function EditHarness() {
  const { workspace, name } = Route.useParams();
  const { api } = Route.useRouteContext();
  const harness = useQuery(harnessQuery(api, workspace, name));
  const { save, error } = useSaveHarness(api, workspace, false);

  return (
    <article className="grid max-w-6xl gap-6">
      <Link
        to="/w/$workspace/harnesses/$name"
        params={{ workspace, name }}
        className="text-sm underline"
      >
        Back to {name}
      </Link>
      <h1 className="text-xl font-semibold">Edit {name}</h1>
      {harness.isPending && <p className="text-muted-foreground">Loading the harness…</p>}
      {harness.isError && (
        <p role="alert" className="text-destructive">
          The harness could not be loaded: {harness.error.message}
        </p>
      )}
      {harness.data !== undefined && (
        <>
          <p className="text-sm text-muted-foreground">
            Editing version {harness.data.version}. Saving stores the changes as the next version;
            runs keep the version they ran.
          </p>
          <HarnessForm
            // A form starts from its initial values once; a newer version
            // starts a new form.
            key={harness.data.version}
            initial={fromHarness(harness.data.harness)}
            creating={false}
            submitLabel="Save as a new version"
            error={error}
            onSubmit={save}
          />
        </>
      )}
    </article>
  );
}
