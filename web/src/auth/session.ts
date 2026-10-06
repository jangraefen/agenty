const KEY = "agenty.token";

// Session holds the signed-in user's API token. It keeps the token in
// storage, localStorage in the browser, so a reload stays signed in, and
// tells subscribers when the user signs in or out. When the browser refuses
// storage, as some do in private windows, the token lives in memory only.
export class Session {
  #token: string | null;
  readonly #storage: Storage;
  readonly #listeners = new Set<() => void>();

  constructor(storage: Storage) {
    this.#storage = storage;
    this.#token = readStored(storage);
  }

  get token(): string | null {
    return this.#token;
  }

  signIn(token: string): void {
    this.#token = token;
    try {
      this.#storage.setItem(KEY, token);
    } catch {
      // Storage refused: the session lasts as long as the page.
    }
    this.#notify();
  }

  signOut(): void {
    this.#token = null;
    try {
      this.#storage.removeItem(KEY);
    } catch {
      // Storage refused, so it holds no token either.
    }
    this.#notify();
  }

  /**
   * Follows the sign-ins and sign-outs of the app's other tabs, which share
   * the storage; returns the function that stops following.
   */
  followOtherTabs(target: Window): () => void {
    const follow = (event: StorageEvent) => {
      // A null key means another tab cleared the storage.
      if (event.key !== KEY && event.key !== null) {
        return;
      }
      const token = readStored(this.#storage);
      if (token !== this.#token) {
        this.#token = token;
        this.#notify();
      }
    };
    target.addEventListener("storage", follow);
    return () => {
      target.removeEventListener("storage", follow);
    };
  }

  /** Calls listener after every sign-in and sign-out; returns the unsubscribe. */
  subscribe(listener: () => void): () => void {
    this.#listeners.add(listener);
    return () => {
      this.#listeners.delete(listener);
    };
  }

  #notify(): void {
    for (const listener of this.#listeners) {
      listener();
    }
  }
}

function readStored(storage: Storage): string | null {
  try {
    return storage.getItem(KEY);
  } catch {
    // Storage refused: nothing was stored.
    return null;
  }
}
