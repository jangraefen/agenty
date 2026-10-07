import { act, screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, test } from "vitest";
import { apiUrl } from "@/config";
import { renderApp } from "@/test/render";
import { emptyWorkspaceHandlers, meHandler, server, TOKEN } from "@/test/server";

beforeEach(() => {
  server.use(...emptyWorkspaceHandlers("notes"), ...emptyWorkspaceHandlers("ops"));
});

describe("signing in", () => {
  test("without a token, every page asks to sign in", async () => {
    const { history } = renderApp("/w/notes/runs", null);

    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
    expect(history.location.pathname).toBe("/sign-in");
  });

  test("a valid token signs in, is stored, and never appears in the URL", async () => {
    server.use(meHandler({ user: "demo", workspaces: ["notes"] }));
    const { history, session, user } = renderApp("/", null);

    await user.type(await screen.findByLabelText("Token"), TOKEN);
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    expect(await screen.findByRole("heading", { name: "Runs" })).toBeInTheDocument();
    expect(history.location.pathname).toBe("/w/notes/runs");
    expect(session.token).toBe(TOKEN);
    expect(localStorage.getItem("agenty.token")).toBe(TOKEN);
    expect(history.location.href).not.toContain(TOKEN);
  });

  test("signing in asks the server who the user is once", async () => {
    let asked = 0;
    server.use(
      http.get(`${apiUrl}/v1/me`, () => {
        asked += 1;
        return HttpResponse.json({ user: "demo", workspaces: ["notes"] });
      }),
    );
    const { user } = renderApp("/sign-in", null);

    await user.type(await screen.findByLabelText("Token"), TOKEN);
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    await screen.findByText("No runs yet.");

    expect(asked).toBe(1);
  });

  test("the token field hides what is typed", async () => {
    renderApp("/sign-in", null);

    expect(await screen.findByLabelText("Token")).toHaveAttribute("type", "password");
  });

  test("an invalid token is refused and not stored", async () => {
    server.use(meHandler({ user: "demo", workspaces: ["notes"] }));
    const { session, user } = renderApp("/sign-in", null);

    await user.type(await screen.findByLabelText("Token"), "not-the-token");
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("This token is not valid.");
    expect(session.token).toBeNull();
    expect(localStorage.getItem("agenty.token")).toBeNull();
  });

  test("an unreachable server is reported", async () => {
    server.use(http.get(`${apiUrl}/v1/me`, () => HttpResponse.error()));
    const { user } = renderApp("/sign-in", null);

    await user.type(await screen.findByLabelText("Token"), TOKEN);
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("The server cannot be reached.");
  });

  test("a stored token the server no longer accepts signs out", async () => {
    server.use(meHandler({ user: "demo", workspaces: ["notes"] }));
    const { history, session } = renderApp("/w/notes/runs", "a-revoked-token");

    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
    expect(history.location.pathname).toBe("/sign-in");
    expect(session.token).toBeNull();
  });

  test("signing out forgets the token", async () => {
    server.use(meHandler({ user: "demo", workspaces: ["notes"] }));
    const { history, session, user } = renderApp("/w/notes/runs", TOKEN);

    await user.click(await screen.findByRole("button", { name: "Sign out" }));

    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
    expect(history.location.pathname).toBe("/sign-in");
    expect(session.token).toBeNull();
    expect(localStorage.getItem("agenty.token")).toBeNull();
  });

  test("a token with characters a header cannot carry is refused before it is sent", async () => {
    const { session, user } = renderApp("/sign-in", null);

    await user.type(await screen.findByLabelText("Token"), "tökén");
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "A token has only letters, digits and punctuation.",
    );
    expect(session.token).toBeNull();
  });

  test("signing out in another tab signs out this one", async () => {
    server.use(meHandler({ user: "demo", workspaces: ["notes"] }));
    const { history } = renderApp("/w/notes/runs", TOKEN);
    await screen.findByRole("heading", { name: "Runs" });

    act(() => {
      localStorage.removeItem("agenty.token");
      window.dispatchEvent(
        new StorageEvent("storage", { key: "agenty.token", oldValue: TOKEN, newValue: null }),
      );
    });

    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
    expect(history.location.pathname).toBe("/sign-in");
  });

  test("a signed-in user visiting the sign-in page goes home", async () => {
    server.use(meHandler({ user: "demo", workspaces: ["notes"] }));
    const { history } = renderApp("/sign-in", TOKEN);

    await waitFor(() => {
      expect(history.location.pathname).toBe("/w/notes/runs");
    });
  });
});

describe("workspaces", () => {
  test("a user with several workspaces picks one", async () => {
    server.use(meHandler({ user: "demo", workspaces: ["notes", "ops"] }));
    const { history, user } = renderApp("/", TOKEN);

    expect(await screen.findByRole("heading", { name: "Workspaces" })).toBeInTheDocument();
    await user.click(screen.getByRole("link", { name: "ops" }));

    await waitFor(() => {
      expect(history.location.pathname).toBe("/w/ops/runs");
    });
  });

  test("a user without workspaces is told so", async () => {
    server.use(meHandler({ user: "demo", workspaces: [] }));
    renderApp("/", TOKEN);

    expect(
      await screen.findByText("You are not a member of any workspace yet."),
    ).toBeInTheDocument();
  });

  test("the header names the user and switches workspaces", async () => {
    server.use(meHandler({ user: "demo", workspaces: ["notes", "ops"] }));
    const { history, user } = renderApp("/w/notes/runs", TOKEN);

    expect(await screen.findByText("demo")).toBeInTheDocument();
    const nav = screen.getByRole("navigation", { name: "Workspaces" });
    expect(within(nav).getByRole("link", { name: "notes" })).toHaveAttribute("aria-current");
    expect(within(nav).getByRole("link", { name: "ops" })).not.toHaveAttribute("aria-current");

    await user.click(within(nav).getByRole("link", { name: "ops" }));

    await waitFor(() => {
      expect(history.location.pathname).toBe("/w/ops/runs");
    });
  });

  test("Escape closes the workspace menu and returns to its button", async () => {
    server.use(meHandler({ user: "demo", workspaces: ["notes", "ops"] }));
    const { user } = renderApp("/w/notes/runs", TOKEN);

    const summary = (await screen.findByText("Workspace:")).closest("summary");
    if (summary === null) {
      throw new Error("no menu button");
    }
    await user.click(summary);
    const menu = summary.closest("details");
    expect(menu).toHaveAttribute("open");
    await user.tab();
    await user.keyboard("{Escape}");

    expect(menu).not.toHaveAttribute("open");
    expect(summary).toHaveFocus();
  });

  test("a click elsewhere closes the workspace menu", async () => {
    server.use(meHandler({ user: "demo", workspaces: ["notes", "ops"] }));
    const { user } = renderApp("/w/notes/runs", TOKEN);

    const summary = (await screen.findByText("Workspace:")).closest("summary");
    if (summary === null) {
      throw new Error("no menu button");
    }
    await user.click(summary);
    await user.click(screen.getByRole("heading", { name: "Runs" }));

    expect(summary.closest("details")).not.toHaveAttribute("open");
  });

  test("a different user signing in in another tab replaces what this one shows", async () => {
    const other = "another-test-token-of-at-least-32-chars";
    server.use(
      http.get(`${apiUrl}/v1/me`, ({ request }) => {
        const auth = request.headers.get("Authorization");
        if (auth === `Bearer ${TOKEN}`) {
          return HttpResponse.json({ user: "demo", workspaces: ["notes"] });
        }
        if (auth === `Bearer ${other}`) {
          return HttpResponse.json({ user: "ana", workspaces: ["notes"] });
        }
        return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
      }),
    );
    renderApp("/w/notes/runs", TOKEN);
    expect(await screen.findByText("demo")).toBeInTheDocument();

    act(() => {
      localStorage.setItem("agenty.token", other);
      window.dispatchEvent(new StorageEvent("storage", { key: "agenty.token", newValue: other }));
    });

    expect(await screen.findByText("ana")).toBeInTheDocument();
  });

  test("a workspace the user is not a member of is not found", async () => {
    server.use(meHandler({ user: "demo", workspaces: ["notes"] }));
    renderApp("/w/secret/runs", TOKEN);

    expect(await screen.findByRole("heading", { name: "Workspace not found" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Your workspaces" })).toBeInTheDocument();
  });
});
