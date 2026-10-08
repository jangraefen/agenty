import { createContext, useContext, useSyncExternalStore } from "react";

// public/theme.js reads the same key and query, before the app loads.
const KEY = "agenty.theme";
const DARK = "(prefers-color-scheme: dark)";

/** The themes a user can choose: System follows the operating system. */
export const themeChoices = ["light", "dark", "system"] as const;
export type ThemeChoice = (typeof themeChoices)[number];

// Theme holds the user's choice of light, dark or the system's theme, a
// per-browser preference kept in storage, localStorage in the browser, and
// applies it as the data-theme attribute of <html>, light or dark, which
// styles.css follows. public/theme.js applies the stored choice before the
// first paint; Theme takes over from there. When the browser refuses
// storage, the choice lasts as long as the page.
export class Theme {
  #choice: ThemeChoice;
  readonly #storage: Storage;
  readonly #dark: MediaQueryList | null;
  readonly #root: HTMLElement;
  readonly #listeners = new Set<() => void>();

  /** Reads the stored choice and applies it at once. */
  constructor(storage: Storage, target: Window) {
    this.#storage = storage;
    // jsdom, for one, has no matchMedia; without it, System is light.
    this.#dark = typeof target.matchMedia === "function" ? target.matchMedia(DARK) : null;
    this.#root = target.document.documentElement;
    this.#choice = readStored(storage);
    this.#apply();
  }

  get choice(): ThemeChoice {
    return this.#choice;
  }

  choose(choice: ThemeChoice): void {
    try {
      this.#storage.setItem(KEY, choice);
    } catch {
      // Storage refused: the choice lasts as long as the page.
    }
    this.#change(choice);
  }

  /**
   * Follows the operating system's theme while System is chosen; returns the
   * function that stops following.
   */
  followSystem(): () => void {
    const dark = this.#dark;
    if (dark === null) {
      return () => undefined;
    }
    const follow = () => this.#apply();
    dark.addEventListener("change", follow);
    return () => {
      dark.removeEventListener("change", follow);
    };
  }

  /**
   * Follows the choices made in the app's other tabs, which share the
   * storage; returns the function that stops following.
   */
  followOtherTabs(target: Window): () => void {
    const follow = (event: StorageEvent) => {
      // A null key means another tab cleared the storage.
      if (event.key !== KEY && event.key !== null) {
        return;
      }
      const choice = readStored(this.#storage);
      if (choice !== this.#choice) {
        this.#change(choice);
      }
    };
    target.addEventListener("storage", follow);
    return () => {
      target.removeEventListener("storage", follow);
    };
  }

  /**
   * Calls listener after every change of the choice; returns the
   * unsubscribe. Bound, so React can keep it from one render to the next.
   */
  readonly subscribe = (listener: () => void): (() => void) => {
    this.#listeners.add(listener);
    return () => {
      this.#listeners.delete(listener);
    };
  };

  #change(choice: ThemeChoice): void {
    this.#choice = choice;
    this.#apply();
    for (const listener of this.#listeners) {
      listener();
    }
  }

  #apply(): void {
    const dark =
      this.#choice === "system" ? (this.#dark?.matches ?? false) : this.#choice === "dark";
    this.#root.dataset.theme = dark ? "dark" : "light";
  }
}

function readStored(storage: Storage): ThemeChoice {
  let stored: string | null = null;
  try {
    stored = storage.getItem(KEY);
  } catch {
    // Storage refused: nothing was stored.
  }
  return themeChoices.find((choice) => choice === stored) ?? "system";
}

export const ThemeContext = createContext<Theme | null>(null);

/** The app's theme and the user's current choice of it. */
export function useTheme(): { theme: Theme; choice: ThemeChoice } {
  const theme = useContext(ThemeContext);
  if (theme === null) {
    throw new Error("useTheme needs a ThemeContext provider");
  }
  const choice = useSyncExternalStore(theme.subscribe, () => theme.choice);
  return { theme, choice };
}
