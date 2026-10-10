import { z } from "zod";

/** Response of GET /api/workspaces/[workspaceId]/users, checked by the handler and the client. */
export const userSearchResponseSchema = z.object({
  users: z
    .array(
      z.object({
        id: z.string(),
        name: z.string(),
        email: z.string(),
        status: z.enum(["member", "invited"]).nullable(),
      }),
    )
    .max(10),
});
export type UserSearchResponse = z.infer<typeof userSearchResponseSchema>;
