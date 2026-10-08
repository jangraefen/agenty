import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, expect, test } from "vitest";
import { apiUrl } from "@/config";
import { logEvent } from "@/test/fixtures";
import { renderApp } from "@/test/render";
import { emptyWorkspaceHandlers, meHandler, type Schemas, server, TOKEN } from "@/test/server";

beforeEach(() => {
  server.use(
    meHandler({ user: "demo", workspaces: ["notes"] }),
    ...emptyWorkspaceHandlers("notes"),
  );
});

test("lists the workspace's changes, with who made them", async () => {
  server.use(
    http.get(`${apiUrl}/v1/workspaces/notes/audit`, () =>
      HttpResponse.json<Schemas["AuditLogEventList"]>({
        events: [logEvent({ actor: "ana", details: { version: 3 } })],
      }),
    ),
  );
  renderApp("/w/notes/audit", TOKEN);

  expect(await screen.findByRole("heading", { name: "Audit log" })).toBeInTheDocument();
  const table = await screen.findByRole("table", { name: "Events" });
  const [, change] = within(table).getAllByRole("row");
  expect(change).toHaveTextContent("ana");
  expect(change).toHaveTextContent("Harness changed");
  expect(change).toHaveTextContent("notes");
  expect(change).toHaveTextContent("version 3");
  const manage = screen.getByRole("navigation", { name: "Manage" });
  expect(within(manage).getByRole("link", { name: "Audit log" })).toHaveAttribute(
    "aria-current",
    "page",
  );
});
