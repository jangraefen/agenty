import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, expect, test } from "vitest";
import { apiUrl } from "@/config";
import { approvalRequest, conversation } from "@/test/fixtures";
import { renderApp } from "@/test/render";
import {
  conversationsHandler,
  emptyWorkspaceHandlers,
  meHandler,
  server,
  TOKEN,
} from "@/test/server";

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
  expect(within(manage).getByRole("link", { name: "Runs" })).toHaveAttribute(
    "href",
    "/w/work/runs",
  );
  expect(within(manage).getByRole("link", { name: "Approvals" })).toHaveAttribute(
    "href",
    "/w/work/approvals",
  );
});

test("offers no workspace switcher with one workspace", async () => {
  server.use(meHandler({ user: "demo", workspaces: ["notes"] }));
  renderApp("/w/notes/harnesses", TOKEN);

  const manage = await screen.findByRole("navigation", { name: "Manage" });
  expect(within(manage).queryByRole("button", { name: /Workspace/ })).not.toBeInTheDocument();
});

test("counts the approvals waiting in the managed workspace", async () => {
  server.use(
    http.get(`${apiUrl}/v1/workspaces/notes/approvals`, () =>
      HttpResponse.json([approvalRequest(), approvalRequest({ id: "a2" })]),
    ),
  );
  renderApp("/w/notes/harnesses", TOKEN);

  const manage = await screen.findByRole("navigation", { name: "Manage" });
  expect(
    await within(manage).findByRole("link", { name: "Approvals (2 waiting)" }),
  ).toBeInTheDocument();
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

  const manage = screen.getByRole("navigation", { name: "Manage" });
  await user.click(within(manage).getByRole("link", { name: "Runs" }));
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
