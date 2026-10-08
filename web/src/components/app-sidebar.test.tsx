import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { act } from "react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { apiUrl } from "@/config";
import { conversation } from "@/test/fixtures";
import { renderApp } from "@/test/render";
import {
  conversationsHandler,
  emptyWorkspaceHandlers,
  meHandler,
  type Schemas,
  server,
  TOKEN,
} from "@/test/server";

afterEach(() => {
  vi.useRealTimers();
});

beforeEach(() => {
  server.use(
    meHandler({ user: "demo", workspaces: ["notes", "work"] }),
    ...emptyWorkspaceHandlers("notes"),
    ...emptyWorkspaceHandlers("work"),
  );
});

test("lists the user's recent chats, latest first", async () => {
  server.use(
    conversationsHandler({
      "": {
        conversations: [
          conversation({ id: "c2", title: "shopping list" }),
          conversation({ id: "c1", title: "tidy my notes" }),
        ],
      },
    }),
  );
  renderApp("/w/notes/harnesses", TOKEN);

  const recent = await screen.findByRole("navigation", { name: "Recent chats" });
  await waitFor(() =>
    expect(
      within(recent)
        .getAllByRole("link")
        .map((link) => link.textContent),
    ).toEqual(["shopping list", "tidy my notes"]),
  );
  expect(within(recent).getByRole("link", { name: "tidy my notes" })).toHaveAttribute(
    "href",
    "/c/c1",
  );
});

test("shows more chats on request", async () => {
  const queries: URLSearchParams[] = [];
  server.use(
    conversationsHandler(
      {
        "": { conversations: [conversation({ id: "c2", title: "newer" })], next: "5.c2" },
        "5.c2": { conversations: [conversation({ id: "c1", title: "older" })] },
      },
      queries,
    ),
  );
  const { user } = renderApp("/w/notes/harnesses", TOKEN);

  const recent = await screen.findByRole("navigation", { name: "Recent chats" });
  await user.click(await within(recent).findByRole("button", { name: "Show more" }));

  expect(await within(recent).findByRole("link", { name: "older" })).toBeInTheDocument();
  expect(within(recent).getByRole("link", { name: "newer" })).toBeInTheDocument();
  expect(within(recent).queryByRole("button", { name: "Show more" })).not.toBeInTheDocument();
  expect(queries.map((query) => query.get("before"))).toEqual([null, "5.c2"]);
});

test("says when there are no chats yet", async () => {
  renderApp("/w/notes/harnesses", TOKEN);

  const recent = await screen.findByRole("navigation", { name: "Recent chats" });
  expect(await within(recent).findByText("No chats yet.")).toBeInTheDocument();
});

test("says why the chats cannot be loaded", async () => {
  server.use(
    http.get(`${apiUrl}/v1/conversations`, () =>
      HttpResponse.json({ error: "boom" }, { status: 500 }),
    ),
  );
  renderApp("/w/notes/harnesses", TOKEN);

  const recent = await screen.findByRole("navigation", { name: "Recent chats" });
  expect(await within(recent).findByRole("alert")).toHaveTextContent("boom");
});

test("manages the workspace of the page", async () => {
  renderApp("/w/work/harnesses", TOKEN);

  const manage = await screen.findByRole("navigation", { name: "Manage" });
  expect(within(manage).getByRole("link", { name: "Harnesses" })).toHaveAttribute(
    "href",
    "/w/work/harnesses",
  );
  expect(within(manage).getByRole("link", { name: "Harnesses" })).toHaveAttribute(
    "aria-current",
    "page",
  );
});

test("manages none of a workspace the user is not a member of", async () => {
  renderApp("/w/secret/harnesses", TOKEN);

  await screen.findByRole("heading", { name: "Workspace not found" });
  const manage = screen.getByRole("navigation", { name: "Manage" });
  expect(within(manage).getByRole("link", { name: "Harnesses" })).toHaveAttribute(
    "href",
    "/w/notes/harnesses",
  );
});

test("offers no workspace switcher with one workspace", async () => {
  server.use(meHandler({ user: "demo", workspaces: ["notes"] }));
  renderApp("/w/notes/harnesses", TOKEN);

  const manage = await screen.findByRole("navigation", { name: "Manage" });
  expect(within(manage).queryByRole("button", { name: /Workspace/ })).not.toBeInTheDocument();
});

test("the menu button shows and hides the sidebar, and a page left hides it", async () => {
  const { user } = renderApp("/w/notes/harnesses", TOKEN);

  const toggle = await screen.findByRole("button", { name: "Open sidebar" });
  expect(toggle).toHaveAttribute("aria-expanded", "false");
  await user.click(toggle);
  expect(screen.getByRole("button", { name: "Close sidebar" })).toHaveAttribute(
    "aria-expanded",
    "true",
  );

  await user.click(screen.getByRole("link", { name: "New chat" }));
  expect(await screen.findByRole("button", { name: "Open sidebar" })).toHaveAttribute(
    "aria-expanded",
    "false",
  );
});

test("signs out from the sidebar", async () => {
  const { user, history, session } = renderApp("/w/notes/harnesses", TOKEN);

  await user.click(await screen.findByRole("button", { name: "Sign out" }));

  expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
  expect(history.location.pathname).toBe("/sign-in");
  expect(session.token).toBeNull();
  expect(screen.queryByRole("complementary")).not.toBeInTheDocument();
});

test("marks a chat that waits for approval", async () => {
  server.use(
    conversationsHandler({
      "": {
        conversations: [
          conversation({ id: "c2", title: "write the notes", status: "waiting" }),
          conversation({ id: "c1", title: "tidy my notes" }),
        ],
      },
    }),
  );
  renderApp("/w/notes/harnesses", TOKEN);

  const recent = await screen.findByRole("navigation", { name: "Recent chats" });
  expect(
    await within(recent).findByRole("link", {
      name: "write the notes (waiting for approval)",
    }),
  ).toHaveAttribute("href", "/c/c2");
  expect(within(recent).getByRole("link", { name: "tidy my notes" })).toBeInTheDocument();
});

test("offers the audit log to auditors only", async () => {
  renderApp("/w/notes/harnesses", TOKEN);
  await screen.findByRole("navigation", { name: "Manage" });
  expect(screen.queryByRole("navigation", { name: "Compliance" })).not.toBeInTheDocument();
});

test("an auditor finds the audit log in the sidebar", async () => {
  server.use(meHandler({ user: "demo", workspaces: ["notes"], auditor: true }));
  renderApp("/w/notes/harnesses", TOKEN);

  const compliance = await screen.findByRole("navigation", { name: "Compliance" });
  expect(within(compliance).getByRole("link", { name: "Audit log" })).toHaveAttribute(
    "href",
    "/audit",
  );
});

test("follows a chat's status soon while it runs, slowly while it waits, and not once done", async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  let status: Schemas["RunStatus"] = "running";
  let requests = 0;
  server.use(
    http.get(`${apiUrl}/v1/conversations`, () => {
      requests += 1;
      return HttpResponse.json<Schemas["ConversationList"]>({
        conversations: [conversation({ title: "write the notes", status })],
      });
    }),
  );
  renderApp("/w/notes/harnesses", TOKEN);
  const recent = await screen.findByRole("navigation", { name: "Recent chats" });
  await within(recent).findByRole("link", { name: "write the notes" });
  const tick = (ms: number) => act(() => vi.advanceTimersByTimeAsync(ms));

  status = "waiting";
  await tick(5000);
  expect(
    await within(recent).findByRole("link", { name: "write the notes (waiting for approval)" }),
  ).toBeInTheDocument();
  const waiting = requests;
  await tick(30_000);
  expect(requests, "a waiting chat is not asked about soon").toBe(waiting);
  status = "succeeded";
  await tick(30_000);
  expect(requests, "but within a minute").toBe(waiting + 1);
  expect(await within(recent).findByRole("link", { name: "write the notes" })).toBeInTheDocument();
  await tick(120_000);
  expect(requests, "nor at all once every chat is done").toBe(waiting + 1);
});
