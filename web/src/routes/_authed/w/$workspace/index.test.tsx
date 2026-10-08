import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, expect, test } from "vitest";
import { apiUrl } from "@/config";
import { logEvent, storedHarness } from "@/test/fixtures";
import { renderApp } from "@/test/render";
import { emptyWorkspaceHandlers, meHandler, type Schemas, server, TOKEN } from "@/test/server";

beforeEach(() => {
  server.use(
    meHandler({ user: "demo", workspaces: ["notes"] }),
    ...emptyWorkspaceHandlers("notes"),
  );
});

test("shows the workspace's members, harnesses and latest changes", async () => {
  const limits: (string | null)[] = [];
  server.use(
    http.get(`${apiUrl}/v1/workspaces/notes`, () =>
      HttpResponse.json<Schemas["WorkspaceDetail"]>({ name: "notes", members: ["ana", "demo"] }),
    ),
    http.get(`${apiUrl}/v1/workspaces/notes/harnesses`, () =>
      HttpResponse.json<Schemas["HarnessVersion"][]>([storedHarness({ name: "tidy" })]),
    ),
    // The page size asked for, of seven events.
    http.get(`${apiUrl}/v1/workspaces/notes/audit`, ({ request }) => {
      const limit = new URL(request.url).searchParams.get("limit");
      limits.push(limit);
      const size = Math.min(Number(limit ?? 50), 7);
      return HttpResponse.json<Schemas["AuditLogEventList"]>({
        events: Array.from({ length: size }, (_, i) =>
          logEvent({ id: 7 - i, actor: "ana", target: "tidy", details: { version: 7 - i } }),
        ),
        next: 8 - size,
      });
    }),
  );
  const { history } = renderApp("/w/notes", TOKEN);

  expect(await screen.findByRole("heading", { level: 1, name: "notes" })).toBeInTheDocument();
  expect(history.location.pathname).toBe("/w/notes");

  const members = await screen.findByRole("list", { name: "Members" });
  expect(
    within(members)
      .getAllByRole("listitem")
      .map((item) => item.textContent),
  ).toEqual(["ana", "demo"]);

  const harnesses = screen.getByRole("region", { name: "Harnesses" });
  expect(within(harnesses).getByRole("list", { name: "Harnesses" })).toBeInTheDocument();
  expect(within(harnesses).getByRole("link", { name: "tidy" })).toHaveAttribute(
    "href",
    "/w/notes/harnesses/tidy",
  );
  expect(harnesses).toHaveTextContent("v1");
  expect(within(harnesses).getByRole("link", { name: "New harness" })).toHaveAttribute(
    "href",
    "/w/notes/new-harness",
  );

  const changes = screen.getByRole("region", { name: "Recent changes" });
  const table = await within(changes).findByRole("table", { name: "Events" });
  expect(limits).toEqual(["5"]);
  expect(within(table).getAllByRole("row")).toHaveLength(1 + 5);
  expect(table).toHaveTextContent("version 7");
  expect(table).not.toHaveTextContent("version 2");
  // The toggles are named by the cells shown: there is no workspace column.
  for (const toggle of within(table).getAllByRole("button", { name: /^Details of/ })) {
    for (const id of toggle.getAttribute("aria-labelledby")?.split(" ") ?? []) {
      expect(document.getElementById(id)).not.toBeNull();
    }
  }
  expect(within(changes).queryByRole("button", { name: "Load more" })).not.toBeInTheDocument();
  expect(within(changes).getByRole("link", { name: "All changes" })).toHaveAttribute(
    "href",
    "/w/notes/audit",
  );

  const manage = screen.getByRole("navigation", { name: "Manage" });
  expect(within(manage).getByRole("link", { name: "Overview" })).toHaveAttribute(
    "aria-current",
    "page",
  );
});

test("says when the workspace has no harnesses or changes yet", async () => {
  renderApp("/w/notes", TOKEN);

  const harnesses = await screen.findByRole("region", { name: "Harnesses" });
  expect(await within(harnesses).findByText(/No harnesses yet/)).toBeInTheDocument();
  const changes = screen.getByRole("region", { name: "Recent changes" });
  expect(await within(changes).findByText("No changes yet.")).toBeInTheDocument();
});

test("says why the members cannot be loaded, and shows the rest", async () => {
  server.use(
    http.get(`${apiUrl}/v1/workspaces/notes`, () =>
      HttpResponse.json({ error: "boom" }, { status: 500 }),
    ),
  );
  renderApp("/w/notes", TOKEN);

  const members = await screen.findByRole("region", { name: "Members" });
  expect(await within(members).findByRole("alert")).toHaveTextContent("boom");
  expect(screen.getByRole("region", { name: "Harnesses" })).toBeInTheDocument();
});

test("is not found for a workspace the user is not a member of", async () => {
  renderApp("/w/secret", TOKEN);

  expect(await screen.findByRole("heading", { name: "Workspace not found" })).toBeInTheDocument();
});
