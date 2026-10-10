const MESSAGES: Record<string, string> = {
  forbidden: "Only admins can do that.",
  personal_workspace: "Your personal workspace can't be renamed, shared or deleted.",
  last_admin:
    "A workspace needs at least one admin. Make someone else an admin first, or delete the workspace.",
  already_member: "That user is already a member.",
  already_invited: "That user is already invited.",
  user_not_found: "That user doesn't exist.",
  invalid_name: "Names need 1 to 80 characters.",
  invalid_query: "Search needs 2 to 100 characters.",
  invalid_role: "Choose admin or member.",
  confirmation_mismatch: "Type the workspace name exactly to delete it.",
};
const FALLBACK = "Something went wrong. Please try again.";

/** Fixed text for an error code from the URL; unknown codes get the generic message. */
export function workspaceErrorMessage(code: string | null | undefined): string | undefined {
  if (!code) return undefined;
  return Object.hasOwn(MESSAGES, code) ? MESSAGES[code] : FALLBACK;
}
