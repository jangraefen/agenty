// Drives an OIDC sign-in against the mock IdP over HTTP, without a browser.
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

/** Starts a sign-in at /sign-in/social. Returns the IdP's authorize URL and the state cookies. */
export async function startSignIn(
  auth: Auth,
  baseURL: string,
): Promise<{ url: string; cookie: string }> {
  const response = await auth.handler(
    new Request(`${baseURL}/api/auth/sign-in/social`, {
      method: "POST",
      headers: { "content-type": "application/json", origin: baseURL },
      body: JSON.stringify({ provider: "oidc", callbackURL: "/", errorCallbackURL: "/sign-in" }),
    }),
  );
  if (response.status !== 200) {
    throw new Error(`sign-in start answered ${response.status}`);
  }
  const { url } = (await response.json()) as { url: string };
  const jar = new CookieJar();
  jar.store(response);
  return { url, cookie: jar.header() };
}

/**
 * Signs in at the mock IdP's login form as `sub` with the given claims and finishes at the app's
 * callback. Returns the callback's status and location and the cookies collected, including the
 * session cookie on success.
 */
export async function signInViaMock(
  auth: Auth,
  opts: { baseURL: string; sub: string; email: string; name?: string },
): Promise<{ status: number; location: string; cookie: string }> {
  const start = await startSignIn(auth, opts.baseURL);

  const idp = await fetch(start.url, {
    method: "POST",
    body: new URLSearchParams({
      username: opts.sub,
      claims: JSON.stringify({
        email: opts.email,
        name: opts.name ?? "Test User",
        email_verified: true,
      }),
    }),
    redirect: "manual",
  });
  const callbackUrl = idp.headers.get("location") ?? "";
  if (idp.status !== 302 || !callbackUrl) {
    throw new Error(`mock IdP answered ${idp.status} instead of a redirect`);
  }

  const jar = new CookieJar(start.cookie);
  const response = await auth.handler(
    new Request(callbackUrl, { headers: { cookie: start.cookie } }),
  );
  jar.store(response);
  return {
    status: response.status,
    location: response.headers.get("location") ?? "",
    cookie: jar.header(),
  };
}
