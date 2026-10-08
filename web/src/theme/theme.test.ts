import { afterEach, describe, expect, test, vi } from "vitest";
import { fakeColorScheme, removeFakeColorScheme } from "@/test/media";
import { Theme } from "./theme";

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

describe("Theme", () => {
  test("follows the system by default", () => {
    fakeColorScheme("dark");
    const theme = new Theme(window);

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

    const theme = new Theme(window);

    expect(theme.choice).toBe(choice);
    expect(html).toHaveAttribute("data-theme", applied);
  });

  test("choosing a theme applies it, keeps it and tells subscribers", () => {
    fakeColorScheme("light");
    const theme = new Theme(window);
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
    const theme = new Theme(window);
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
    const theme = new Theme(window);
    theme.followSystem();
    theme.choose("light");

    system.change("dark");

    expect(html).toHaveAttribute("data-theme", "light");
  });

  test("works when the browser refuses storage", () => {
    fakeColorScheme("dark");
    refuseStorage();

    const theme = new Theme(window);
    expect(html).toHaveAttribute("data-theme", "dark");
    theme.choose("light");

    expect(theme.choice).toBe("light");
    expect(html).toHaveAttribute("data-theme", "light");
  });

  test("without matchMedia, System is light", () => {
    const theme = new Theme(window);

    expect(theme.choice).toBe("system");
    expect(html).toHaveAttribute("data-theme", "light");
    expect(() => theme.followSystem()()).not.toThrow();
  });
});
