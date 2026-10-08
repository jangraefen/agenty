import { StoredValue } from "@/lib/stored-value";

const KEY = "agenty.token";

// Session holds the signed-in user's API token. It keeps the token in
// storage, localStorage in the browser, so a reload stays signed in, and
// tells subscribers when the user signs in or out. When the browser refuses
// storage, as some do in private windows, the token lives in memory only.
export class Session {
  readonly #token: StoredValue<string | null>;

  constructor(storage: Storage) {
    this.#token = new StoredValue(storage, KEY, (stored) => stored);
  }

  get token(): string | null {
    return this.#token.value;
  }

  signIn(token: string): void {
    this.#token.set(token);
  }

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
