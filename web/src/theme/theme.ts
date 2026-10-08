import { createContext, useContext, useSyncExternalStore } from "react";
import { StoredValue } from "@/lib/stored-value";

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
  readonly #choice: StoredValue<ThemeChoice>;
  readonly #dark: MediaQueryList | null;
  readonly #root: HTMLElement;

  /** Reads the stored choice and applies it at once. */
  constructor(storage: Storage, target: Window) {
    // jsdom, for one, has no matchMedia; without it, System is light.
    this.#dark = typeof target.matchMedia === "function" ? target.matchMedia(DARK) : null;
    this.#root = target.document.documentElement;
    this.#choice = new StoredValue(
      storage,
      KEY,
      (stored) => themeChoices.find((choice) => choice === stored) ?? "system",
    );
    // Subscribed first, so a choice applies before the subscribers hear of it.
    this.#choice.subscribe(() => this.#apply());
    this.#apply();
  }

  get choice(): ThemeChoice {
    return this.#choice.value;
  }

  choose(choice: ThemeChoice): void {
    this.#choice.set(choice);
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
    return this.#choice.followOtherTabs(target);
  }

  /**
   * Calls listener after every change of the choice; returns the
   * unsubscribe. Bound, so React can keep it from one render to the next.
   */
  readonly subscribe = (listener: () => void): (() => void) => this.#choice.subscribe(listener);

  #apply(): void {
    const choice = this.#choice.value;
    const dark = choice === "system" ? (this.#dark?.matches ?? false) : choice === "dark";
    this.#root.dataset.theme = dark ? "dark" : "light";
  }
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
