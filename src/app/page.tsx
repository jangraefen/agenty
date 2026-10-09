import Link from "next/link";
import { Suspense } from "react";
import { buttonVariants } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { SignOutButton } from "@/components/user-menu";
import { ForbiddenError, getCurrentSignedIn, UnauthorizedError } from "@/server/auth/tenant";

export default function HomePage() {
  return (
    <Suspense>
      <Home />
    </Suspense>
  );
}

async function Home() {
  const access = await getCurrentSignedIn().then(
    () => "member" as const,
    (error: unknown) => {
      if (error instanceof UnauthorizedError) return "signed-out" as const;
      if (error instanceof ForbiddenError) return "no-organization" as const;
      throw error;
    },
  );
  if (access === "signed-out") return <Landing />;
  if (access === "no-organization") return <NoOrganization />;

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

function NoOrganization() {
  return (
    <div className="mx-auto my-auto flex w-full max-w-2xl flex-col gap-6">
      <Card>
        <CardHeader>
          <CardTitle>No active organization</CardTitle>
          <CardDescription>
            Your account has no active organization. Contact your administrator.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <SignOutButton />
        </CardContent>
      </Card>
    </div>
  );
}
