// StoredValue is a value kept under a key of storage, localStorage in the
// browser, so a reload keeps it; parse reads it from what is stored there,
// null for nothing. It tells subscribers when it changes. When the browser
// refuses storage, as some do in private windows, the value lives in memory
// only, as long as the page.
export class StoredValue<T extends string | null> {
  #value: T;
  readonly #storage: Storage;
  readonly #key: string;
  readonly #parse: (stored: string | null) => T;
  readonly #listeners = new Set<() => void>();

  constructor(storage: Storage, key: string, parse: (stored: string | null) => T) {
    this.#storage = storage;
    this.#key = key;
    this.#parse = parse;
    this.#value = this.#read();
  }

  get value(): T {
    return this.#value;
  }

  /** Keeps value, null removing the stored one, and tells subscribers. */
  set(value: T): void {
    this.#value = value;
    try {
      if (value === null) {
        this.#storage.removeItem(this.#key);
      } else {
        this.#storage.setItem(this.#key, value);
      }
    } catch {
      // Storage refused: the value lasts as long as the page.
    }
    this.#notify();
  }

  /**
   * Follows the changes made in the app's other tabs, which share the
   * storage; returns the function that stops following.
   */
  followOtherTabs(target: Window): () => void {
    const follow = (event: StorageEvent) => {
      // A null key means another tab cleared the storage.
      if (event.key !== this.#key && event.key !== null) {
        return;
      }
      const value = this.#read();
      if (value !== this.#value) {
        this.#value = value;
        this.#notify();
      }
    };
    target.addEventListener("storage", follow);
    return () => {
      target.removeEventListener("storage", follow);
    };
  }

  /**
   * Calls listener after every change of the value, in the order they
   * subscribed; returns the unsubscribe. Bound, so React can keep it from
   * one render to the next.
   */
  readonly subscribe = (listener: () => void): (() => void) => {
    this.#listeners.add(listener);
    return () => {
      this.#listeners.delete(listener);
    };
  };

  #read(): T {
    let stored: string | null = null;
    try {
      stored = this.#storage.getItem(this.#key);
    } catch {
      // Storage refused: nothing was stored.
    }
    return this.#parse(stored);
  }

  #notify(): void {
    for (const listener of this.#listeners) {
      listener();
    }
  }
}
