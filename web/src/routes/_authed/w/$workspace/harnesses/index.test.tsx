import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, expect, test } from "vitest";
import { apiUrl } from "@/config";
import { storedHarness } from "@/test/fixtures";
import { renderApp } from "@/test/render";
import { meHandler, server, TOKEN } from "@/test/server";

const base = `${apiUrl}/v1/workspaces/notes`;

beforeEach(() => {
  server.use(
    meHandler({ user: "demo", workspaces: ["notes"] }),
    http.get(`${base}/approvals`, () => HttpResponse.json([])),
  );
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

test("the header links to the harnesses", async () => {
  server.use(http.get(`${base}/harnesses`, () => HttpResponse.json([])));
  renderApp("/w/notes/harnesses", TOKEN);

  const pages = await screen.findByRole("navigation", { name: "Pages" });
  expect(within(pages).getByRole("link", { name: "Harnesses" })).toHaveAttribute(
    "aria-current",
    "page",
  );
});
