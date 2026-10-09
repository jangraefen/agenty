import type { Metadata } from "next";
import { redirect } from "next/navigation";
import { Suspense } from "react";
import { Card, CardContent, CardDescription, CardHeader } from "@/components/ui/card";
import { getCurrentUser } from "@/server/auth/session";
import { signInErrorMessage } from "./error-messages";
import { SignInButton } from "./sign-in-button";

export const metadata: Metadata = { title: "Sign in · Agenty" };

export default function SignInPage({ searchParams }: PageProps<"/sign-in">) {
  return (
    <Card className="mx-auto my-auto w-full max-w-md">
      <CardHeader>
        <h1 className="font-semibold text-2xl tracking-tight">Sign in to Agenty</h1>
        <CardDescription>Use your organization's single sign-on.</CardDescription>
      </CardHeader>
      <CardContent>
        <Suspense>
          <SignInButtonWithError searchParams={searchParams} />
        </Suspense>
      </CardContent>
    </Card>
  );
}

/**
 * Sends signed-in users home. Reads only `error`, never `error_description`: that is text from
 * the identity provider.
 */
async function SignInButtonWithError({
  searchParams,
}: {
  searchParams: PageProps<"/sign-in">["searchParams"];
}) {
  if (await getCurrentUser()) redirect("/");

  const { error } = await searchParams;
  const code = Array.isArray(error) ? error[0] : error;
  return <SignInButton initialError={signInErrorMessage(code)} />;
}
