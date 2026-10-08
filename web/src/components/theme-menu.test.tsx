import { act, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { fakeColorScheme, removeFakeColorScheme } from "@/test/media";
import { renderApp } from "@/test/render";
import { emptyWorkspaceHandlers, meHandler, server, TOKEN } from "@/test/server";

const html = document.documentElement;

beforeEach(() => {
  server.use(
    meHandler({ user: "demo", workspaces: ["notes"] }),
    ...emptyWorkspaceHandlers("notes"),
  );
});

afterEach(() => {
  removeFakeColorScheme();
});

describe("the theme menu", () => {
  test("is in the sidebar and switches to Dark, which is kept", async () => {
    fakeColorScheme("light");
    const { user } = renderApp("/w/notes/harnesses", TOKEN);

    const sidebar = await screen.findByRole("complementary");
    await user.click(within(sidebar).getByRole("button", { name: "Theme: System" }));
    await user.click(await screen.findByRole("menuitemradio", { name: "Dark" }));

    expect(html).toHaveAttribute("data-theme", "dark");
    expect(localStorage.getItem("agenty.theme")).toBe("dark");
    expect(within(sidebar).getByRole("button", { name: "Theme: Dark" })).toBeInTheDocument();
  });

  test("marks the current choice", async () => {
    fakeColorScheme("light");
    localStorage.setItem("agenty.theme", "light");
    const { user } = renderApp("/w/notes/harnesses", TOKEN);

    await user.click(await screen.findByRole("button", { name: "Theme: Light" }));

    expect(await screen.findByRole("menuitemradio", { name: "Light" })).toBeChecked();
    expect(screen.getByRole("menuitemradio", { name: "Dark" })).not.toBeChecked();
    expect(screen.getByRole("menuitemradio", { name: "System" })).not.toBeChecked();
  });

  test("is on the sign-in page, where System follows the operating system", async () => {
    const system = fakeColorScheme("dark");
    const { user } = renderApp("/sign-in", null);

    await user.click(await screen.findByRole("button", { name: "Theme: System" }));
    await user.click(await screen.findByRole("menuitemradio", { name: "Light" }));
    expect(html).toHaveAttribute("data-theme", "light");

    await user.click(screen.getByRole("button", { name: "Theme: Light" }));
    await user.click(await screen.findByRole("menuitemradio", { name: "System" }));
    expect(html).toHaveAttribute("data-theme", "dark");

    system.change("light");
    expect(html).toHaveAttribute("data-theme", "light");
  });

  test("still works when the browser refuses storage", async () => {
    fakeColorScheme("light");
    const refuse = () => {
      throw new DOMException("storage is disabled", "SecurityError");
    };
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(refuse);
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(refuse);
    const { user } = renderApp("/sign-in", null);

    await user.click(await screen.findByRole("button", { name: "Theme: System" }));
    await user.click(await screen.findByRole("menuitemradio", { name: "Dark" }));

    expect(html).toHaveAttribute("data-theme", "dark");
    expect(screen.getByRole("heading", { name: "Sign in" })).toBeInTheDocument();
  });

  test("follows a choice made in another tab", async () => {
    fakeColorScheme("light");
    renderApp("/sign-in", null);
    await screen.findByRole("button", { name: "Theme: System" });

    localStorage.setItem("agenty.theme", "dark");
    act(() => {
      window.dispatchEvent(new StorageEvent("storage", { key: "agenty.theme" }));
    });

    expect(screen.getByRole("button", { name: "Theme: Dark" })).toBeInTheDocument();
    expect(html).toHaveAttribute("data-theme", "dark");
  });
});
