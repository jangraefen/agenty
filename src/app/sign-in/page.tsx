import type { Metadata } from "next";
import { Suspense } from "react";
import { Card, CardContent, CardDescription, CardHeader } from "@/components/ui/card";
import { signInErrorMessage } from "./error-messages";
import { SignInForm } from "./sign-in-form";

export const metadata: Metadata = { title: "Sign in · Agenty" };

export default function SignInPage({ searchParams }: PageProps<"/sign-in">) {
  return (
    <Card className="mx-auto my-auto w-full max-w-md">
      <CardHeader>
        <h1 className="font-semibold text-2xl tracking-tight">Sign in to Agenty</h1>
        <CardDescription>Use your organization's single sign-on.</CardDescription>
      </CardHeader>
      <CardContent>
        <Suspense fallback={<SignInForm />}>
          <SignInFormWithError searchParams={searchParams} />
        </Suspense>
      </CardContent>
    </Card>
  );
}

async function SignInFormWithError({
  searchParams,
}: {
  searchParams: PageProps<"/sign-in">["searchParams"];
}) {
  const { error } = await searchParams;
  const code = Array.isArray(error) ? error[0] : error;
  return <SignInForm initialError={signInErrorMessage(code)} />;
}
