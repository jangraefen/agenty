import { createContext, useContext, useSyncExternalStore } from "react";

const KEY = "agenty.theme";
const DARK = "(prefers-color-scheme: dark)";

/** The themes a user can choose: System follows the operating system. */
export const themeChoices = ["light", "dark", "system"] as const;
export type ThemeChoice = (typeof themeChoices)[number];

// Theme holds the user's choice of light, dark or the system's theme, a
// per-browser preference kept in localStorage, and applies it as the
// data-theme attribute of <html>, light or dark, which styles.css follows.
// When the browser refuses storage, the choice lasts as long as the page.
export class Theme {
  #choice: ThemeChoice;
  readonly #storage: Storage | null;
  readonly #dark: MediaQueryList | null;
  readonly #root: HTMLElement;
  readonly #listeners = new Set<() => void>();

  /** Reads the stored choice and applies it at once, before anything renders. */
  constructor(target: Window) {
    this.#storage = storageOf(target);
    // jsdom, for one, has no matchMedia; without it, System is light.
    this.#dark = typeof target.matchMedia === "function" ? target.matchMedia(DARK) : null;
    this.#root = target.document.documentElement;
    this.#choice = readStored(this.#storage);
    this.#apply();
  }

  get choice(): ThemeChoice {
    return this.#choice;
  }

  choose(choice: ThemeChoice): void {
    this.#choice = choice;
    try {
      this.#storage?.setItem(KEY, choice);
    } catch {
      // Storage refused: the choice lasts as long as the page.
    }
    this.#apply();
    for (const listener of this.#listeners) {
      listener();
    }
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

  /** Calls listener after every choice; returns the unsubscribe. */
  subscribe(listener: () => void): () => void {
    this.#listeners.add(listener);
    return () => {
      this.#listeners.delete(listener);
    };
  }

  #apply(): void {
    const dark =
      this.#choice === "system" ? (this.#dark?.matches ?? false) : this.#choice === "dark";
    this.#root.dataset.theme = dark ? "dark" : "light";
  }
}

function storageOf(target: Window): Storage | null {
  try {
    return target.localStorage;
  } catch {
    // Some browsers refuse even to hand out storage when it is blocked.
    return null;
  }
}

function readStored(storage: Storage | null): ThemeChoice {
  let stored: string | null = null;
  try {
    stored = storage?.getItem(KEY) ?? null;
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
  const choice = useSyncExternalStore(
    (listener) => theme.subscribe(listener),
    () => theme.choice,
  );
  return { theme, choice };
}
