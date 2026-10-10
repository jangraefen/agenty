import type { Metadata } from "next";
import { redirect } from "next/navigation";
import { Suspense } from "react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader } from "@/components/ui/card";
import { getCurrentUser } from "@/server/auth/session";
import { signIn } from "./actions";
import { signInErrorMessage } from "./error-messages";

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
          <SignInForm searchParams={searchParams} />
        </Suspense>
      </CardContent>
    </Card>
  );
}

const first = (value: string | string[] | undefined) => (Array.isArray(value) ? value[0] : value);

/**
 * Sends signed-in users home. Reads only `error`, never `error_description`: that is text from
 * the identity provider.
 */
async function SignInForm({
  searchParams,
}: {
  searchParams: PageProps<"/sign-in">["searchParams"];
}) {
  if (await getCurrentUser()) redirect("/");

  const { error } = await searchParams;
  const code = first(error);
  const message = signInErrorMessage(code);
  return (
    <form action={signIn} className="flex flex-col gap-4">
      {message ? (
        <p className="text-destructive text-sm" role="alert">
          {message}
        </p>
      ) : null}
      <Button type="submit">Sign in</Button>
    </form>
  );
}
