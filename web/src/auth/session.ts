/**
 * Who is signed in: the auth/ directory holds the frontend's session, the
 * only place the API token is kept.
 *
 * Agenty's API authenticates with a bearer token; the frontend has no cookie
 * or login flow of its own. The sign-in page checks a token the user enters
 * against GET /v1/me and keeps it here. client.ts reads the token from the
 * Session for every request and signs it out on a 401; App.tsx subscribes to
 * it to clear the query cache and re-run the router's sign-in checks; the
 * signed-in layout route redirects to sign in when it holds no token.
 *
 * The token lives in localStorage so a reload stays signed in. That makes it
 * readable by any script the page runs, which is why the built frontend's
 * Content-Security-Policy (csp.ts) lets the page run only its own scripts
 * and connect only to the API. The token must never be logged or put in
 * a URL; Biome's noConsole rule keeps console logging out of the app.
 */
import { StoredValue } from "@/lib/stored-value";

/** The storage key the token is kept under. */
const KEY = "agenty.token";

/**
 * Session holds the signed-in user's API token. It keeps the token in
 * storage, localStorage in the browser, so a reload stays signed in, and
 * tells subscribers when the user signs in or out. When the browser refuses
 * storage, as some do in private windows, the token lives in memory only.
 *
 * A thin layer over lib/stored-value.ts, which does the storing, the
 * notifying and the following of other tabs; Session gives it the sign-in
 * vocabulary and takes storage as a parameter, so tests pass jsdom's.
 */
export class Session {
  readonly #token: StoredValue<string | null>;

  constructor(storage: Storage) {
    this.#token = new StoredValue(storage, KEY, (stored) => stored);
  }

  /** The API token, or null when no one is signed in. */
  get token(): string | null {
    return this.#token.value;
  }

  /** Keeps token, which the caller has checked with the server, and notifies. */
  signIn(token: string): void {
    this.#token.set(token);
  }

  /** Forgets the token, in storage too, and notifies. */
  signOut(): void {
    this.#token.set(null);
  }

  /**
   * Follows the sign-ins and sign-outs of the app's other tabs, which share
   * the storage; returns the function that stops following.
   */
  followOtherTabs(target: Window): () => void {
    return this.#token.followOtherTabs(target);
  }

  /** Calls listener after every sign-in and sign-out; returns the unsubscribe. */
  subscribe(listener: () => void): () => void {
    return this.#token.subscribe(listener);
  }
}
