import { createFileRoute, Link } from "@tanstack/react-router";
import { HarnessForm } from "@/components/harness-form";
import { useSaveHarness } from "@/hooks/use-save-harness";
import { emptyHarnessValues } from "@/lib/harness-form";

/**
 * The form for a new harness, at `/w/{ws}/new-harness`, reached from the
 * overview and the harness list.
 *
 * It is not under `/harnesses`, where `/harnesses/new` would hide a harness
 * named "new". It starts the shared HarnessForm from empty values and saves
 * through useSaveHarness in its creating mode, which refuses a name another
 * harness already has instead of quietly storing a new version of it.
 */
export const Route = createFileRoute("/_authed/w/$workspace/new-harness")({
  component: NewHarness,
});

/** The new-harness page: the form, with a name field to fill. */
function NewHarness() {
  const { workspace } = Route.useParams();
  const { api } = Route.useRouteContext();
  const { save, error } = useSaveHarness(api, workspace, true);
  return (
    <article className="grid max-w-6xl gap-6">
      <Link to="/w/$workspace/harnesses" params={{ workspace }} className="text-sm underline">
        All harnesses
      </Link>
      <h1 className="text-xl font-semibold">New harness</h1>
      <HarnessForm
        initial={emptyHarnessValues()}
        creating
        submitLabel="Create harness"
        error={error}
        onSubmit={save}
      />
    </article>
  );
}
