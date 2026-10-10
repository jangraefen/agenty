import { createAuthClient } from "better-auth/react";

/** Browser client for /api/auth (sign-out); sign-in starts in a server action. */
export const authClient = createAuthClient();
