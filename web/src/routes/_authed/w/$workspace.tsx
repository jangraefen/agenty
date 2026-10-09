import { createFileRoute, Link, notFound, Outlet } from "@tanstack/react-router";
import { NotFoundPage } from "@/components/route-states";

/**
 * The layout of a workspace's management, at `/w/{ws}`. Its children are the
 * overview (w/$workspace/index.tsx), the harnesses with each harness's page
 * and edit page, the new-harness form, and the workspace's audit log.
 *
 * The membership check reads `me.workspaces` from the context the authed
 * layout loaded, so it runs before any of its pages loads its data. The
 * server refuses a workspace the user is not a member of as not found, and
 * so does this check, saying so on a page of its own instead of each child
 * failing its requests. A workspace the user left thus has no management
 * pages, while its chats stay readable at `/c/{conversation}`.
 *
 * The sidebar reads the `workspace` parameter of these URLs to name the
 * workspace it manages.
 */
export const Route = createFileRoute("/_authed/w/$workspace")({
  beforeLoad: ({ context, params }) => {
    if (!context.me.workspaces.includes(params.workspace)) {
      throw notFound();
    }
  },
  component: Outlet,
  notFoundComponent: () => (
    <NotFoundPage
      title="Workspace not found"
      detail="It does not exist, or you are not a member of it."
      className="mx-auto max-w-xl"
    >
      <Link to="/" className="underline">
        Start a new chat
      </Link>
    </NotFoundPage>
  ),
});
