import { createFileRoute, Link } from "@tanstack/react-router";
import { HarnessForm } from "@/components/harness-form";
import { useSaveHarness } from "@/hooks/use-save-harness";
import { emptyHarnessValues } from "@/lib/harness-form";

// Not under /harnesses, where it would hide a harness named "new".
export const Route = createFileRoute("/_authed/w/$workspace/new-harness")({
  component: NewHarness,
});

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
