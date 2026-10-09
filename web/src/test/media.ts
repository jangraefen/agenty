/**
 * A fake prefers-color-scheme media query, for the theme tests: they switch
 * the operating system's scheme and check that Theme follows it, and that
 * it stops listening when told to.
 */

/**
 * A stand-in for the browser's prefers-color-scheme query, which jsdom does
 * not implement: window.matchMedia answers it until removeFakeColorScheme.
 * Any other query throws, so a new use of matchMedia does not go untested.
 */
export function fakeColorScheme(scheme: "light" | "dark") {
  let dark = scheme === "dark";
  const listeners = new Set<(event: MediaQueryListEvent) => void>();
  const query = {
    get matches() {
      return dark;
    },
    media: "(prefers-color-scheme: dark)",
    addEventListener: (_type: "change", listener: (event: MediaQueryListEvent) => void) => {
      listeners.add(listener);
    },
    removeEventListener: (_type: "change", listener: (event: MediaQueryListEvent) => void) => {
      listeners.delete(listener);
    },
  };
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    value: (media: string) => {
      if (media !== query.media) {
        throw new Error(`unexpected media query ${media}`);
      }
      return query as unknown as MediaQueryList;
    },
  });
  return {
    /** Switches the operating system to scheme, as its settings would. */
    change(next: "light" | "dark") {
      dark = next === "dark";
      for (const listener of listeners) {
        listener({ matches: dark, media: query.media } as MediaQueryListEvent);
      }
    },
    /** How many listeners follow the query. */
    get listening() {
      return listeners.size;
    },
  };
}

/** Removes the fake, leaving window without matchMedia, as jsdom has it. */
export function removeFakeColorScheme() {
  Reflect.deleteProperty(window, "matchMedia");
}
