import "server-only";
import { headers } from "next/headers";
import { redirect } from "next/navigation";
import { cache } from "react";
import { getAuth } from "./auth";

export type CurrentUser = { id: string; name: string; email: string };

/** The signed-in user of the current request, or null. Looked up once per request. */
export const getCurrentUser = cache(async (): Promise<CurrentUser | null> => {
  // Request headers first: during prerender this suspends before getAuth() reads the env, which
  // `next build` does not have.
  const requestHeaders = await headers();
  const session = await (await getAuth()).api.getSession({ headers: requestHeaders });
  if (!session) return null;
  return { id: session.user.id, name: session.user.name, email: session.user.email };
});

/** The signed-in user; sends everyone else to the sign-in page. */
export async function requireUser(): Promise<CurrentUser> {
  const user = await getCurrentUser();
  if (!user) redirect("/sign-in");
  return user;
}
