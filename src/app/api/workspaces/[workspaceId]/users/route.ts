import { z } from "zod";
import { userSearchResponseSchema } from "@/lib/user-search";
import { getCurrentUser } from "@/server/auth/session";
import { WorkspaceError, type WorkspaceErrorCode } from "@/server/workspaces/errors";
import { searchUsers } from "@/server/workspaces/invitations";

// The query is validated again by the service; this only shapes the request.
const querySchema = z.object({ q: z.string().max(1000) });

const STATUS: Partial<Record<WorkspaceErrorCode, number>> = {
  invalid_query: 400,
  forbidden: 403,
  personal_workspace: 403,
  not_found: 404,
};

const noStore = { "cache-control": "no-store" };
const fail = (status: number, error: string) =>
  Response.json({ error }, { status, headers: noStore });

/**
 * Invite search for the settings page (R6). A route handler rather than a server action: server
 * actions are meant for mutations and run one at a time per client, while this is a read that
 * fires as the admin types. The workspace id is a lookup key; the service checks the session
 * user's membership and role.
 */
export async function GET(
  request: Request,
  ctx: RouteContext<"/api/workspaces/[workspaceId]/users">,
) {
  const user = await getCurrentUser();
  if (!user) return fail(401, "unauthorized");
  const query = querySchema.safeParse({ q: new URL(request.url).searchParams.get("q") ?? "" });
  if (!query.success) return fail(400, "invalid_query");
  const { workspaceId } = await ctx.params;

  try {
    const users = await searchUsers(user.id, workspaceId, query.data.q);
    return Response.json(userSearchResponseSchema.parse({ users }), { headers: noStore });
  } catch (error) {
    if (error instanceof WorkspaceError) return fail(STATUS[error.code] ?? 400, error.code);
    console.error("User search failed:", error instanceof Error ? error.name : "unknown");
    return fail(500, "internal");
  }
}
