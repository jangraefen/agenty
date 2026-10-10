import "server-only";
import { refresh } from "next/cache";
import { notFound, redirect } from "next/navigation";
import { WorkspaceError } from "@/server/workspaces/errors";

/** `path` with `?error=<code>` added (other query parameters, like `q`, are kept). */
export function withError(path: string, code: string): string {
  const url = new URL(path, "http://localhost");
  url.searchParams.set("error", code);
  return `${url.pathname}${url.search}`;
}

/**
 * Runs a workspace operation for a server action. Rule violations go back to `path` with their
 * code (not_found shows the not-found page); anything else is logged by class and shown as the
 * generic message. After a success the client router is refreshed: the redirect that follows
 * keeps layouts (workspace name, header invitation count) that would otherwise show stale data.
 */
export async function runWorkspaceAction<T>(path: string, operation: () => Promise<T>): Promise<T> {
  let result: T;
  try {
    result = await operation();
  } catch (error) {
    if (error instanceof WorkspaceError) {
      if (error.code === "not_found") notFound();
      redirect(withError(path, error.code));
    }
    console.error(`Workspace action failed: ${error instanceof Error ? error.name : typeof error}`);
    redirect(withError(path, "unexpected"));
  }
  refresh();
  return result;
}

/** A form field as a string ("" when missing, or when a tampered call sent no FormData). */
export const field = (formData: unknown, name: string) =>
  formData instanceof FormData ? String(formData.get(name) ?? "") : "";
