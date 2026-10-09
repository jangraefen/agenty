import { toNextJsHandler } from "better-auth/next-js";
import { connection } from "next/server";
import { getAuth } from "@/server/auth/auth";

// The auth instance is created on the first request, not at import: `next build` runs without
// runtime configuration. connection() keeps Next from trying to prerender the handler.
export const { GET, POST } = toNextJsHandler(async (request: Request) => {
  await connection();
  return getAuth().handler(request);
});
