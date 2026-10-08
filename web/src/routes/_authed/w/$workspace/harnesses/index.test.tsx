import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, expect, test } from "vitest";
import { apiUrl } from "@/config";
import { storedHarness } from "@/test/fixtures";
import { renderApp } from "@/test/render";
import { meHandler, server, TOKEN } from "@/test/server";

const base = `${apiUrl}/v1/workspaces/notes`;

beforeEach(() => {
  server.use(meHandler({ user: "demo", workspaces: ["notes"] }));
});

test("lists the workspace's harnesses", async () => {
  server.use(
    http.get(`${base}/harnesses`, () =>
      HttpResponse.json([
        storedHarness(
          { name: "notes", model: { provider: "anthropic", name: "a-model" }, tools: ["a", "b"] },
          { version: 4 },
        ),
        storedHarness({ name: "triage" }),
      ]),
    ),
  );
  renderApp("/w/notes/harnesses", TOKEN);

  const table = await screen.findByRole("table", { name: "Harnesses" });
  const [, notes, triage] = within(table).getAllByRole("row");
  expect(within(notes as HTMLElement).getByRole("link", { name: "notes" })).toHaveAttribute(
    "href",
    "/w/notes/harnesses/notes",
  );
  expect(notes).toHaveTextContent("v4");
  expect(notes).toHaveTextContent("anthropic / a-model");
  expect(notes).toHaveTextContent("2 tools");
  expect(triage).toHaveTextContent("triage");
});

test("says when the workspace has no harnesses", async () => {
  server.use(http.get(`${base}/harnesses`, () => HttpResponse.json([])));
  renderApp("/w/notes/harnesses", TOKEN);

  expect(await screen.findByText(/No harnesses yet/)).toBeInTheDocument();
});

test("the sidebar links to the harnesses", async () => {
  server.use(http.get(`${base}/harnesses`, () => HttpResponse.json([])));
  renderApp("/w/notes/harnesses", TOKEN);

  const pages = await screen.findByRole("navigation", { name: "Manage" });
  expect(within(pages).getByRole("link", { name: "Harnesses" })).toHaveAttribute(
    "aria-current",
    "page",
  );
});

test("says why the harnesses cannot be loaded, beside the sidebar", async () => {
  server.use(
    http.get(`${base}/harnesses`, () => HttpResponse.json({ error: "boom" }, { status: 500 })),
  );
  renderApp("/w/notes/harnesses", TOKEN);

  expect(await screen.findByRole("alert")).toHaveTextContent("boom");
  expect(screen.getByRole("navigation", { name: "Manage" })).toBeInTheDocument();
});

test("a page that could not be loaded can be tried again", async () => {
  let down = true;
  server.use(
    http.get(`${base}/harnesses`, () =>
      down
        ? HttpResponse.json({ error: "boom" }, { status: 500 })
        : HttpResponse.json([storedHarness({ name: "notes" })]),
    ),
  );
  const { user } = renderApp("/w/notes/harnesses", TOKEN);
  await screen.findByRole("alert");

  down = false;
  await user.click(screen.getByRole("button", { name: "Try again" }));

  expect(await screen.findByRole("link", { name: "notes" })).toBeInTheDocument();
});
