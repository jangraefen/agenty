"use server";

import { APIError } from "better-auth/api";
import { headers } from "next/headers";
import { redirect } from "next/navigation";
import { getAuth, OIDC_PROVIDER_ID } from "@/server/auth/auth";

/**
 * Starts the OIDC sign-in and sends the browser to the identity provider. Better Auth's state
 * cookie reaches the browser through the `nextCookies` plugin.
 */
export async function signIn(): Promise<never> {
  redirect(await signInTarget());
}

/** The IdP's authorization URL, or the sign-in page with an error code. */
async function signInTarget(): Promise<string> {
  try {
    const { url } = await (await getAuth()).api.signInSocial({
      body: { provider: OIDC_PROVIDER_ID, callbackURL: "/", errorCallbackURL: "/sign-in" },
      headers: await headers(),
    });
    if (url) return url;
    console.error("Sign-in failed: no authorization URL");
  } catch (error) {
    // While the IdP's discovery has failed, the provider is missing.
    if (error instanceof APIError && error.body?.code === "PROVIDER_NOT_FOUND") {
      return "/sign-in?error=sign_in_unavailable";
    }
    console.error(`Sign-in failed: ${error instanceof Error ? error.name : typeof error}`);
  }
  return "/sign-in?error=sign_in_failed";
}
