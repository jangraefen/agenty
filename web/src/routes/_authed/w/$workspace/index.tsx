import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { harnessesQuery, workspaceAuditQuery, workspaceQuery } from "@/api/queries";
import { EventsTable } from "@/components/events-table";
import { buttonVariants } from "@/components/ui/button";

/**
 * The overview of a workspace, at `/w/{ws}`: the page the sidebar's
 * workspace switcher and Overview link open.
 *
 * It shows who is in the workspace (workspaceQuery, `GET /v1/workspaces/{ws}`),
 * its harnesses (harnessesQuery) and its latest changes (workspaceAuditQuery,
 * fetched in a page of `latestChanges` events), linking to the harness pages,
 * the new-harness form and the workspace's full audit log.
 *
 * It has no loader: each section loads, and fails, on its own, so a slow or
 * failing audit log does not keep the members and harnesses from showing.
 */
export const Route = createFileRoute("/_authed/w/$workspace/")({
  component: Overview,
});

// How many of the latest changes the overview shows, and so fetches.
const latestChanges = 5;

/** The overview page: the workspace's name, then its three sections. */
function Overview() {
  const { workspace } = Route.useParams();
  const { api } = Route.useRouteContext();
  const detail = useQuery(workspaceQuery(api, workspace));
  const harnesses = useQuery(harnessesQuery(api, workspace));
  const changes = useInfiniteQuery(workspaceAuditQuery(api, workspace, latestChanges));

  return (
    <div className="grid gap-8">
      <div>
        <p className="text-sm text-muted-foreground">Workspace</p>
        <h1 className="text-xl font-semibold">{workspace}</h1>
      </div>
      <Section id="members" title="Members">
        {detail.isPending && <p className="text-muted-foreground">Loading members…</p>}
        {detail.isError && (
          <p role="alert" className="text-destructive">
            The members could not be loaded: {detail.error.message}
          </p>
        )}
        {detail.isSuccess && (
          <ul aria-labelledby="members" className="flex flex-wrap gap-2">
            {detail.data.members.map((member) => (
              <li key={member} className="rounded-md border px-2 py-1 text-sm">
                {member}
              </li>
            ))}
          </ul>
        )}
      </Section>
      <Section
        id="harnesses"
        title="Harnesses"
        action={
          <Link
            to="/w/$workspace/new-harness"
            params={{ workspace }}
            className={buttonVariants({ variant: "outline", size: "sm" })}
          >
            New harness
          </Link>
        }
      >
        {harnesses.isPending && <p className="text-muted-foreground">Loading harnesses…</p>}
        {harnesses.isError && (
          <p role="alert" className="text-destructive">
            The harnesses could not be loaded: {harnesses.error.message}
          </p>
        )}
        {harnesses.isSuccess && harnesses.data.length === 0 && (
          <p className="text-muted-foreground">No harnesses yet.</p>
        )}
        {harnesses.isSuccess && harnesses.data.length > 0 && (
          <ul aria-labelledby="harnesses" className="grid gap-1 text-sm">
            {harnesses.data.map(({ harness, version }) => (
              <li key={harness.name} className="flex gap-2">
                <Link
                  to="/w/$workspace/harnesses/$name"
                  params={{ workspace, name: harness.name }}
                  className="font-medium underline-offset-4 hover:underline"
                >
                  {harness.name}
                </Link>
                <span className="text-muted-foreground">v{version}</span>
              </li>
            ))}
          </ul>
        )}
      </Section>
      <Section
        id="changes"
        title="Recent changes"
        action={
          <Link
            to="/w/$workspace/audit"
            params={{ workspace }}
            className="text-sm underline-offset-4 hover:underline"
          >
            All changes
          </Link>
        }
      >
        <EventsTable
          events={changes}
          empty="No changes yet."
          showActor
          showWorkspace={false}
          limit={latestChanges}
        />
      </Section>
    </div>
  );
}

/**
 * A titled section of the overview, with an optional action, such as a link,
 * beside its heading. The heading names the section for assistive
 * technology, and its id names the list inside, too.
 */
function Section({
  id,
  title,
  action,
  children,
}: {
  id: string;
  title: string;
  action?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section aria-labelledby={id} className="grid gap-3">
      <div className="flex items-center gap-4">
        <h2 id={id} className="mr-auto font-semibold">
          {title}
        </h2>
        {action}
      </div>
      {children}
    </section>
  );
}
