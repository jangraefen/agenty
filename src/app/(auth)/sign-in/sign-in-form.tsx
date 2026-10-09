"use client";

import { type FormEvent, useEffect, useState } from "react";
import { z } from "zod";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { authClient } from "@/lib/auth-client";
import { signInErrorMessage } from "./error-messages";

const emailSchema = z.email();

export function SignInForm({ initialError }: { initialError?: string | undefined }) {
  const [error, setError] = useState(initialError);
  const [pending, setPending] = useState(false);
  // Before hydration a click would submit the form natively (GET /sign-in?email=...).
  const [hydrated, setHydrated] = useState(false);
  useEffect(() => setHydrated(true), []);

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const parsed = emailSchema.safeParse(new FormData(event.currentTarget).get("email"));
    if (!parsed.success) {
      setError("Enter a valid email address.");
      return;
    }
    setError(undefined);
    setPending(true);
    try {
      const result = await authClient.signIn.sso({
        email: parsed.data,
        callbackURL: "/",
        errorCallbackURL: "/sign-in",
      });
      if (result.error) {
        setError(signInErrorMessage(result.error.code ?? "sign_in_failed"));
        setPending(false);
      }
      // On success the client navigates to the identity provider; stay pending until then.
    } catch {
      setError(signInErrorMessage("sign_in_failed"));
      setPending(false);
    }
  }

  return (
    <form className="flex flex-col gap-4" noValidate onSubmit={onSubmit}>
      <div className="flex flex-col gap-2">
        <Label htmlFor="email">Work email</Label>
        <Input autoComplete="email" id="email" name="email" required type="email" />
      </div>
      {error ? (
        <p className="text-destructive text-sm" role="alert">
          {error}
        </p>
      ) : null}
      <Button disabled={!hydrated || pending} type="submit">
        Continue with SSO
      </Button>
    </form>
  );
}
