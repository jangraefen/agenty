import Link from "next/link";
import { getWorkspaceAccess } from "@/server/workspaces/access";

/**
 * Workspace name and navigation. Presentation only: layouts don't re-render on navigation and
 * don't protect nested pages or actions, so every page checks membership itself.
 */
export default async function WorkspaceLayout({
  children,
  params,
}: LayoutProps<"/w/[workspaceId]">) {
  const { workspaceId } = await params;
  const access = await getWorkspaceAccess(workspaceId);
  if (!access) return children;
  const base = `/w/${access.workspace.id}`;

  return (
    <>
      <nav aria-label="Workspace" className="flex items-center justify-between gap-4 border-b pb-3">
        <h1 className="font-semibold text-2xl tracking-tight">{access.workspace.name}</h1>
        <div className="flex gap-4 text-sm">
          <Link className="hover:underline" href={base}>
            Home
          </Link>
          <Link className="hover:underline" href={`${base}/settings`}>
            Settings
          </Link>
        </div>
      </nav>
      {children}
    </>
  );
}
