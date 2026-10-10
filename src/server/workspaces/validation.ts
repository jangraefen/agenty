import "server-only";
import { z } from "zod";
import { workspaceRoles } from "@/server/db/schema";

export const PERSONAL_WORKSPACE_NAME = "Personal";

export const workspaceNameSchema = z.string().trim().min(1).max(80);
export const workspaceIdSchema = z.uuid();
export const workspaceRoleSchema = z.enum(workspaceRoles);
export const userSearchQuerySchema = z.string().trim().min(2).max(100);

/** An ILIKE pattern matching `query` anywhere, with `\`, `%` and `_` taken literally. */
export function likePattern(query: string): string {
  return `%${query.replace(/[\\%_]/g, (char) => `\\${char}`)}%`;
}
