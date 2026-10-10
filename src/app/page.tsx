import Link from "next/link";
import { redirect } from "next/navigation";
import { connection } from "next/server";
import { Suspense } from "react";
import { buttonVariants } from "@/components/ui/button";
import { getCurrentUser } from "@/server/auth/session";
import { ensurePersonalWorkspace } from "@/server/workspaces/workspaces";

export default function HomePage() {
  return (
    <Suspense>
      <Home />
    </Suspense>
  );
}

async function Home() {
  const user = await getCurrentUser();
  if (!user) return <Landing />;

  // Sign-in already ensured the personal workspace; this covers users without one (e.g. a failed
  // setup at an earlier sign-in). After connection(): Partial Prefetching renders twice per request.
  await connection();
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
