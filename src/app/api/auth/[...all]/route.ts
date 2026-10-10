import { toNextJsHandler } from "better-auth/next-js";
import { getAuth } from "@/server/auth/auth";

// Better Auth's handler, with the auth instance created on the first request instead of at import:
// `next build` imports this module without runtime configuration, and getAuth() rebuilds the
// instance when the identity provider was unreachable (see AGENTS.md, Auth).
export const { GET, POST } = toNextJsHandler(async (request: Request) =>
  (await getAuth()).handler(request),
);
