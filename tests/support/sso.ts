// Drives an SSO sign-in against the mock IdP over HTTP, without a browser.
import type { Auth } from "@/server/auth/auth";

/** A minimal cookie jar: name -> value from Set-Cookie headers. */
class CookieJar {
  private readonly cookies = new Map<string, string>();

  constructor(header = "") {
    for (const part of header.split(";")) {
      const [name, ...value] = part.trim().split("=");
      if (name) this.cookies.set(name, value.join("="));
    }
  }

  store(response: Response) {
    for (const cookie of response.headers.getSetCookie()) {
      const [pair = ""] = cookie.split(";");
      const [name, ...value] = pair.trim().split("=");
      if (!name) continue;
      const joined = value.join("=");
      if (joined) this.cookies.set(name, joined);
      else this.cookies.delete(name);
    }
  }

  header() {
    return [...this.cookies].map(([name, value]) => `${name}=${value}`).join("; ");
  }
}

export type SignInResult = {
  status: number;
  location: string;
  cookie: string;
  callbackUrl?: string;
  body?: unknown;
};

/**
 * Starts a sign-in at /sign-in/sso, logs in at the mock IdP's form with the given claims and
 * (unless `stopAfterIdp`) finishes at the app's callback. Returns the final status and location
 * and the cookies collected, including the session cookie on success.
 */
export async function signInViaMock(
  auth: Auth,
  opts: {
    baseURL: string;
    email: string;
    name?: string;
    claims?: Record<string, unknown>;
    idpEmail?: string;
    stopAfterIdp?: boolean;
  },
): Promise<SignInResult> {
  const jar = new CookieJar();
  const start = await auth.handler(
    new Request(`${opts.baseURL}/api/auth/sign-in/sso`, {
      method: "POST",
      headers: { "content-type": "application/json", origin: opts.baseURL },
      body: JSON.stringify({ email: opts.email, callbackURL: "/", errorCallbackURL: "/sign-in" }),
    }),
  );
  const body: unknown = await start.json().catch(() => undefined);
  if (start.status !== 200) return { status: start.status, location: "", cookie: "", body };
  jar.store(start);
  const { url } = body as { url: string };

  const idpEmail = opts.idpEmail ?? opts.email;
  const idp = await fetch(url, {
    method: "POST",
    body: new URLSearchParams({
      username: idpEmail,
      claims: JSON.stringify({
        email: idpEmail,
        name: opts.name ?? "Test User",
        email_verified: true,
        ...opts.claims,
      }),
    }),
    redirect: "manual",
  });
  const callbackUrl = idp.headers.get("location") ?? "";
  if (idp.status !== 302 || !callbackUrl) {
    throw new Error(`mock IdP answered ${idp.status} instead of a redirect`);
  }
  if (opts.stopAfterIdp) {
    return { status: idp.status, location: "", cookie: jar.header(), callbackUrl, body };
  }
  return finishCallback(auth, callbackUrl, jar.header());
}

/** Delivers the IdP's redirect to the app's callback with the cookies of the sign-in start. */
export async function finishCallback(
  auth: Auth,
  callbackUrl: string,
  cookie: string,
): Promise<SignInResult> {
  const jar = new CookieJar(cookie);
  const response = await auth.handler(new Request(callbackUrl, { headers: { cookie } }));
  jar.store(response);
  return {
    status: response.status,
    location: response.headers.get("location") ?? "",
    cookie: jar.header(),
  };
}
