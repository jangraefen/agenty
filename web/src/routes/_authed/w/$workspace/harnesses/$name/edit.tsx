import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { harnessQuery } from "@/api/queries";
import { HarnessForm } from "@/components/harness-form";
import { useSaveHarness } from "@/hooks/use-save-harness";
import { fromHarness } from "@/lib/harness-form";

/**
 * The edit page of a harness, at `/w/{ws}/harnesses/{name}/edit`.
 *
 * It fills the shared HarnessForm from the harness's latest version, which
 * the parent route loaded, and saves through useSaveHarness, which stores
 * the values as the harness's next version and opens its page. Every
 * version is immutable: saving never changes what a run or a conversation
 * already uses, as a conversation stays on the version it started with.
 */
export const Route = createFileRoute("/_authed/w/$workspace/harnesses/$name/edit")({
  component: EditHarness,
});

/** The edit page: the form, its name read-only, saving as a new version. */
function EditHarness() {
  const { workspace, name } = Route.useParams();
  const { api } = Route.useRouteContext();
  const { data: harness } = useSuspenseQuery(harnessQuery(api, workspace, name));
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
      <p className="text-sm text-muted-foreground">
        Editing version {harness.version}. Saving stores the changes as the next version; runs keep
        the version they ran.
      </p>
      <HarnessForm
        // A form starts from its initial values once; a newer version starts
        // a new form.
        key={harness.version}
        initial={fromHarness(harness.harness)}
        creating={false}
        submitLabel="Save as a new version"
        error={error}
        onSubmit={save}
      />
    </article>
  );
}
