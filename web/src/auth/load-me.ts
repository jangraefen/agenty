import { redirect } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { meQuery } from "@/api/queries";
import type { RouterContext } from "@/router";

// loadMe loads the signed-in user for a route that needs one, sending the
// browser to sign in when there is no token or the server refuses it.
export async function loadMe({ queryClient, session, api }: RouterContext) {
  if (session.token === null) {
    throw redirect({ to: "/sign-in" });
  }
  try {
    return await queryClient.ensureQueryData(meQuery(api));
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      throw redirect({ to: "/sign-in" });
    }
    throw error;
  }
}
