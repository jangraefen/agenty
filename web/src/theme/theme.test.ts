import { afterEach, describe, expect, test, vi } from "vitest";
import { fakeColorScheme, removeFakeColorScheme } from "@/test/media";
import earlyScript from "../../public/theme.js?raw";
import { Theme, type ThemeChoice } from "./theme";

const html = document.documentElement;

afterEach(() => {
  removeFakeColorScheme();
});

function refuseStorage() {
  const refuse = () => {
    throw new DOMException("storage is disabled", "SecurityError");
  };
  vi.spyOn(Storage.prototype, "getItem").mockImplementation(refuse);
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(refuse);
}

// Runs public/theme.js, which index.html loads before the app.
function runEarlyScript() {
  new Function(earlyScript)();
}

describe("Theme", () => {
  test("follows the system by default", () => {
    fakeColorScheme("dark");
    const theme = new Theme(localStorage, window);

    expect(theme.choice).toBe("system");
    expect(html).toHaveAttribute("data-theme", "dark");
  });

  test.each([
    { stored: "light", system: "dark", choice: "light", applied: "light" },
    { stored: "dark", system: "light", choice: "dark", applied: "dark" },
    { stored: "system", system: "dark", choice: "system", applied: "dark" },
    { stored: "purple", system: "light", choice: "system", applied: "light" },
  ] as const)("applies a stored $stored on load", ({ stored, system, choice, applied }) => {
    fakeColorScheme(system);
    localStorage.setItem("agenty.theme", stored);

    const theme = new Theme(localStorage, window);

    expect(theme.choice).toBe(choice);
    expect(html).toHaveAttribute("data-theme", applied);
  });

  test("choosing a theme applies it, keeps it and tells subscribers", () => {
    fakeColorScheme("light");
    const theme = new Theme(localStorage, window);
    const listener = vi.fn();
    theme.subscribe(listener);

    theme.choose("dark");

    expect(theme.choice).toBe("dark");
    expect(html).toHaveAttribute("data-theme", "dark");
    expect(localStorage.getItem("agenty.theme")).toBe("dark");
    expect(listener).toHaveBeenCalledOnce();
  });

  test("System follows the operating system as it changes", () => {
    const system = fakeColorScheme("light");
    const theme = new Theme(localStorage, window);
    const stop = theme.followSystem();

    system.change("dark");
    expect(html).toHaveAttribute("data-theme", "dark");
    system.change("light");
    expect(html).toHaveAttribute("data-theme", "light");

    stop();
    expect(system.listening).toBe(0);
  });

  test("a chosen theme ignores the operating system", () => {
    const system = fakeColorScheme("light");
    const theme = new Theme(localStorage, window);
    const stop = theme.followSystem();
    theme.choose("light");

    system.change("dark");

    expect(html).toHaveAttribute("data-theme", "light");
    stop();
  });

  test("follows the choices of the app's other tabs", () => {
    fakeColorScheme("light");
    const theme = new Theme(localStorage, window);
    const listener = vi.fn();
    theme.subscribe(listener);
    const stop = theme.followOtherTabs(window);

    // Another tab writes the storage, and the browser tells this one.
    localStorage.setItem("agenty.theme", "dark");
    window.dispatchEvent(new StorageEvent("storage", { key: "agenty.theme" }));
    expect(theme.choice).toBe("dark");
    expect(html).toHaveAttribute("data-theme", "dark");
    expect(listener).toHaveBeenCalledOnce();

    // Another key, here the token, leaves the theme alone.
    window.dispatchEvent(new StorageEvent("storage", { key: "agenty.token" }));
    expect(listener).toHaveBeenCalledOnce();

    // A cleared storage, signalled by a null key, means System again.
    localStorage.clear();
    window.dispatchEvent(new StorageEvent("storage", { key: null }));
    expect(theme.choice).toBe("system");
    expect(html).toHaveAttribute("data-theme", "light");

    stop();
    localStorage.setItem("agenty.theme", "dark");
    window.dispatchEvent(new StorageEvent("storage", { key: "agenty.theme" }));
    expect(theme.choice).toBe("system");
  });

  test("works when the browser refuses storage", () => {
    fakeColorScheme("dark");
    refuseStorage();

    const theme = new Theme(localStorage, window);
    expect(html).toHaveAttribute("data-theme", "dark");
    theme.choose("light");

    expect(theme.choice).toBe("light");
    expect(html).toHaveAttribute("data-theme", "light");
  });

  test("without matchMedia, System is light", () => {
    const theme = new Theme(localStorage, window);

    expect(theme.choice).toBe("system");
    expect(html).toHaveAttribute("data-theme", "light");
    expect(() => theme.followSystem()()).not.toThrow();
  });
});

// public/theme.js applies the stored theme before the first paint, which the
// app's module script runs too late for; it must agree with Theme.
describe("the early theme script", () => {
  test.each([
    { choice: "light", system: "dark", applied: "light" },
    { choice: "dark", system: "light", applied: "dark" },
    { choice: "system", system: "dark", applied: "dark" },
    { choice: "system", system: "light", applied: "light" },
  ] as const)(
    "applies a stored $choice on a $system system as Theme does",
    ({ choice, system, applied }) => {
      fakeColorScheme(system);
      new Theme(localStorage, window).choose(choice satisfies ThemeChoice);
      delete html.dataset.theme;

      runEarlyScript();

      expect(html).toHaveAttribute("data-theme", applied);
    },
  );

  test("applies an unknown stored value as System", () => {
    fakeColorScheme("dark");
    localStorage.setItem("agenty.theme", "purple");

    runEarlyScript();

    expect(html).toHaveAttribute("data-theme", "dark");
  });

  test("works when the browser refuses storage", () => {
    fakeColorScheme("dark");
    refuseStorage();

    runEarlyScript();

    expect(html).toHaveAttribute("data-theme", "dark");
  });

  test("without matchMedia, System is light", () => {
    runEarlyScript();

    expect(html).toHaveAttribute("data-theme", "light");
  });
});
