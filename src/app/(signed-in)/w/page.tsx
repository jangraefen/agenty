import { redirect } from "next/navigation";
import { requireUser } from "@/server/auth/session";
import { ensurePersonalWorkspace } from "@/server/workspaces/workspaces";

/**
 * Where sign-in lands: sends the user on to their personal workspace. It sits in the signed-in
 * layout so the sidebar frame shows from the first paint (via `/` the signed-out layout would show
 * first).
 */
export default async function SignedInHomePage() {
  const user = await requireUser();
  // Sign-in created the personal workspace, so this only looks its id up.
  redirect(`/w/${await ensurePersonalWorkspace(user.id)}`);
}
