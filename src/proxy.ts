import { getSessionCookie } from "better-auth/cookies";
import { type NextRequest, NextResponse } from "next/server";

/** Optimistic check only: no session cookie → sign-in. Pages validate the session themselves. */
export function proxy(request: NextRequest) {
  if (!getSessionCookie(request)) return NextResponse.redirect(new URL("/sign-in", request.url));
  return NextResponse.next();
}

export const config = { matcher: ["/settings/:path*"] };
