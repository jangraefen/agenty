import { createAuthClient } from "better-auth/react";

/** Browser client for /api/auth; signIn.social also covers the genericOAuth provider. */
export const authClient = createAuthClient();
