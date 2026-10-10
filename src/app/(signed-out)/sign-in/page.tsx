import type { Metadata } from "next";
import { redirect } from "next/navigation";
import { connection } from "next/server";
import { Suspense } from "react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader } from "@/components/ui/card";
import { getCurrentUser } from "@/server/auth/session";
import { signIn } from "./actions";
import { signInErrorLogLine, signInErrorMessage } from "./error-messages";

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
 * Sends signed-in users to /w (their workspace). `error_description` is IdP text: it is logged on the server, never
 * shown.
 */
async function SignInForm({
  searchParams,
}: {
  searchParams: PageProps<"/sign-in">["searchParams"];
}) {
  if (await getCurrentUser()) redirect("/w");

  const params = await searchParams;
  const code = first(params.error);
  if (code) {
    // With Partial Prefetching, Next renders the page twice per request (once more for the
    // runtime prefetch). Only the request render gets past connection(), so this logs once.
    await connection();
    console.warn(signInErrorLogLine(code, first(params.error_description)));
  }
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
