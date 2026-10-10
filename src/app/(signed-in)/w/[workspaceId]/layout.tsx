import Link from "next/link";
import { Suspense } from "react";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { getWorkspaceAccess } from "@/server/workspaces/access";

type Props = LayoutProps<"/w/[workspaceId]">;

/**
 * Workspace name and navigation. Presentation only: layouts don't re-render on navigation and
 * don't protect nested pages or actions, so every page checks membership itself.
 */
export default function WorkspaceLayout({ children, params }: Props) {
  return (
    <>
      <Suspense>
        <WorkspaceNav params={params} />
      </Suspense>
      {children}
    </>
  );
}

async function WorkspaceNav({ params }: Pick<Props, "params">) {
  const { workspaceId } = await params;
  const access = await getWorkspaceAccess(workspaceId);
  if (!access) return null;
  const base = `/w/${access.workspace.id}`;

  return (
    <nav aria-label="Workspace" className="flex items-center justify-between gap-4 border-b pb-3">
      <h1 className="font-semibold text-2xl tracking-tight">{access.workspace.name}</h1>
      <div className="flex gap-4 text-sm">
        <Link className="hover:underline" href={base}>
          Start
        </Link>
        {access.workspace.personal ? (
          <Tooltip>
            <TooltipTrigger
              render={
                // A disabled link: focusable, so keyboard users get the tooltip too.
                // biome-ignore lint/a11y/useSemanticElements: an <a> without href can't be focused
                <span
                  aria-disabled="true"
                  className="cursor-not-allowed text-muted-foreground"
                  role="link"
                  tabIndex={0}
                />
              }
            >
              Settings
            </TooltipTrigger>
            <TooltipContent>Your personal workspace can't be changed.</TooltipContent>
          </Tooltip>
        ) : (
          <Link className="hover:underline" href={`${base}/settings`}>
            Settings
          </Link>
        )}
      </div>
    </nav>
  );
}
