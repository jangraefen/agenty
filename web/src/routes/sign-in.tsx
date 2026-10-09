import { useMutation, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import { type FormEvent, useId } from "react";
import { ApiError, unwrap } from "@/api/client";
import { meQuery } from "@/api/queries";
import { ThemeMenu } from "@/components/theme-menu";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

/**
 * The sign-in page, at `/sign-in`: the only page outside the authed layout,
 * so it has no sidebar, only the theme menu.
 *
 * Sign-in is a stopgap until OIDC: the user pastes the bearer token the
 * operator config gave them. The page checks it with `GET /v1/me` before the
 * session keeps it, then seeds the `me` query with the answer and opens the
 * new chat page, whose authed guard then finds the user in the cache.
 *
 * The token field is uncontrolled and the token goes only into the
 * Authorization header: the Content-Security-Policy allows inline styles,
 * and a value attribute could be read by an injected style's selectors.
 */
export const Route = createFileRoute("/sign-in")({
  // A signed-in user has nothing to do here.
  beforeLoad: ({ context }) => {
    if (context.session.token !== null) {
      throw redirect({ to: "/" });
    }
  },
  component: SignIn,
});

/**
 * The sign-in form. A mutation checks the candidate token and, once the
 * server accepts it, signs the session in; the error under the field says
 * why a token was refused.
 */
function SignIn() {
  const { api, session } = Route.useRouteContext();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const id = useId();

  const signIn = useMutation({
    // The token is checked before it is kept, so a mistyped one is never
    // stored, and only ever travels in the Authorization header.
    mutationFn: (candidate: string) => {
      // A token holding anything but visible ASCII is refused here, with a
      // message that says so, before it is put in a header and sent.
      if (!headerSafe.test(candidate)) {
        throw new InvalidToken();
      }
      return unwrap(api.GET("/v1/me", { headers: { Authorization: `Bearer ${candidate}` } }));
    },
    onSuccess: async (me, candidate) => {
      // After signing in, which clears the cache for the new user, the
      // answer is cached as theirs, so the authed guard need not ask again.
      session.signIn(candidate);
      queryClient.setQueryData(meQuery(api).queryKey, me);
      await navigate({ to: "/" });
    },
  });

  // The token is read from the form on submit, never held in React state.
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const token = new FormData(event.currentTarget).get("token");
    signIn.mutate(typeof token === "string" ? token.trim() : "");
  }

  const error = signIn.isError ? signInError(signIn.error) : null;

  return (
    <>
      <header className="flex justify-end px-6 py-3">
        <ThemeMenu />
      </header>
      <main className="mx-auto mt-10 max-w-sm p-6">
        <h1 className="text-2xl font-semibold">Sign in</h1>
        <p className="mt-2 text-sm text-muted-foreground">
          Paste the API token your operator gave you.
        </p>
        <form method="post" onSubmit={submit} className="mt-6 grid gap-3">
          <Label htmlFor={`${id}-token`}>Token</Label>
          <Input
            id={`${id}-token`}
            type="password"
            autoComplete="off"
            required
            // Uncontrolled, so the token never becomes the field's value
            // attribute, which a style could match character by character.
            name="token"
            aria-invalid={signIn.isError}
            aria-describedby={signIn.isError ? `${id}-error` : undefined}
          />
          {error !== null && (
            <p id={`${id}-error`} role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <Button type="submit" aria-disabled={signIn.isPending}>
            Sign in
          </Button>
        </form>
      </main>
    </>
  );
}

// What an HTTP header can carry: visible ASCII characters, no spaces.
const headerSafe = /^[\x21-\x7e]+$/;

/** Thrown for a candidate token with characters a header cannot carry. */
class InvalidToken extends Error {}

/**
 * The message shown for a failed sign-in: a malformed token, a token the
 * server refused (401), no answer at all (status undefined, the client's own
 * message), or any other failure with the server's message.
 */
function signInError(error: Error): string {
  if (error instanceof InvalidToken) {
    return "A token has only letters, digits and punctuation.";
  }
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
