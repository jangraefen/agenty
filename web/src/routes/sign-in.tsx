import { useMutation, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import { type FormEvent, useId, useState } from "react";
import { ApiError, unwrap } from "@/api/client";
import { meQuery } from "@/api/queries";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

export const Route = createFileRoute("/sign-in")({
  beforeLoad: ({ context }) => {
    if (context.session.token !== null) {
      throw redirect({ to: "/" });
    }
  },
  component: SignIn,
});

function SignIn() {
  const { api, session } = Route.useRouteContext();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [token, setToken] = useState("");
  const id = useId();

  const signIn = useMutation({
    // The token is checked before it is kept, so a mistyped one is never
    // stored, and only ever travels in the Authorization header.
    mutationFn: (candidate: string) =>
      unwrap(api.GET("/v1/me", { headers: { Authorization: `Bearer ${candidate}` } })),
    onSuccess: async (me, candidate) => {
      queryClient.setQueryData(meQuery(api).queryKey, me);
      session.signIn(candidate);
      await navigate({ to: "/" });
    },
  });

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    signIn.mutate(token.trim());
  }

  return (
    <main className="mx-auto mt-24 max-w-sm p-6">
      <h1 className="text-2xl font-semibold">Sign in</h1>
      <p className="mt-2 text-sm text-muted-foreground">
        Paste the API token your operator gave you.
      </p>
      <form onSubmit={submit} className="mt-6 grid gap-3">
        <Label htmlFor={`${id}-token`}>Token</Label>
        <Input
          id={`${id}-token`}
          type="password"
          autoComplete="off"
          required
          value={token}
          onChange={(event) => setToken(event.target.value)}
          aria-invalid={signIn.isError}
          aria-describedby={signIn.isError ? `${id}-error` : undefined}
        />
        {signIn.isError && (
          <p id={`${id}-error`} role="alert" className="text-sm text-destructive">
            {signInError(signIn.error)}
          </p>
        )}
        <Button type="submit" disabled={signIn.isPending}>
          Sign in
        </Button>
      </form>
    </main>
  );
}

function signInError(error: Error): string {
  if (error instanceof ApiError) {
    if (error.status === 401) {
      return "This token is not valid.";
    }
    if (error.status === undefined) {
      return error.message;
    }
  }
  return `The server could not sign you in: ${error.message}`;
}
