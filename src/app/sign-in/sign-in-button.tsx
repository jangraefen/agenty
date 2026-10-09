"use client";

import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { authClient } from "@/lib/auth-client";
import { signInErrorMessage } from "./error-messages";

export function SignInButton({ initialError }: { initialError?: string | undefined }) {
  const [error, setError] = useState(initialError);
  const [pending, setPending] = useState(false);
  // Before hydration a click would do nothing.
  const [hydrated, setHydrated] = useState(false);
  useEffect(() => setHydrated(true), []);

  async function signIn() {
    setError(undefined);
    setPending(true);
    try {
      const result = await authClient.signIn.social({
        provider: "oidc", // OIDC_PROVIDER_ID (src/server/auth/auth.ts is server-only)
        callbackURL: "/",
        errorCallbackURL: "/sign-in",
      });
      if (result.error) {
        // While the IdP's discovery has failed, the provider is missing.
        const code =
          result.error.code === "PROVIDER_NOT_FOUND" ? "sign_in_unavailable" : result.error.code;
        setError(signInErrorMessage(code ?? "sign_in_failed"));
        setPending(false);
      }
      // On success the client navigates to the identity provider; stay pending until then.
    } catch {
      setError(signInErrorMessage("sign_in_failed"));
      setPending(false);
    }
  }

  return (
    <div className="flex flex-col gap-4">
      {error ? (
        <p className="text-destructive text-sm" role="alert">
          {error}
        </p>
      ) : null}
      <Button disabled={!hydrated || pending} onClick={signIn}>
        Sign in
      </Button>
    </div>
  );
}
