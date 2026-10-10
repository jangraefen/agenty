import Link from "next/link";
import { redirect } from "next/navigation";
import { buttonVariants } from "@/components/ui/button";
import { getCurrentUser } from "@/server/auth/session";
import { ensurePersonalWorkspace } from "@/server/workspaces/workspaces";

export default async function HomePage() {
  const user = await getCurrentUser();
  if (!user) return <Landing />;
  // Sign-in created the personal workspace, so this only looks its id up.
  redirect(`/w/${await ensurePersonalWorkspace(user.id)}`);
}

function Landing() {
  return (
    <div className="mx-auto my-auto flex w-full max-w-2xl flex-col gap-6">
      <div className="flex flex-col gap-2">
        <h1 className="font-semibold text-4xl tracking-tight">Agenty</h1>
        <p className="text-lg text-muted-foreground">
          Self-hosted AI agents for your organization.
        </p>
      </div>
      <div>
        <Link className={buttonVariants()} href="/sign-in">
          Sign in
        </Link>
      </div>
    </div>
  );
}
