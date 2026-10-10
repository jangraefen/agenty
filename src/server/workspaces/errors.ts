import "server-only";

export const WORKSPACE_ERROR_CODES = [
  "not_found",
  "forbidden",
  "personal_workspace",
  "last_admin",
  "already_member",
  "already_invited",
  "user_not_found",
  "invalid_name",
  "invalid_query",
  "invalid_role",
  "confirmation_mismatch",
] as const;
export type WorkspaceErrorCode = (typeof WORKSPACE_ERROR_CODES)[number];

/** A broken workspace rule. Carries only a fixed code, never internal details. */
export class WorkspaceError extends Error {
  constructor(readonly code: WorkspaceErrorCode) {
    super(code);
    this.name = "WorkspaceError";
  }
}
