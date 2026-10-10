import Link from "next/link";
import { Suspense } from "react";
import { buttonVariants } from "@/components/ui/button";
import { Card, CardHeader, CardTitle } from "@/components/ui/card";
import { getCurrentUser } from "@/server/auth/session";

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

  return (
    <Card>
      <CardHeader>
        <CardTitle>Agents arrive in the next milestone</CardTitle>
      </CardHeader>
    </Card>
  );
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
