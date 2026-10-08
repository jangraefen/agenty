import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, expect, test } from "vitest";
import { apiUrl } from "@/config";
import { logEvent } from "@/test/fixtures";
import { renderApp } from "@/test/render";
import { emptyWorkspaceHandlers, meHandler, type Schemas, server, TOKEN } from "@/test/server";

beforeEach(() => {
  server.use(
    meHandler({ user: "dana", workspaces: [], auditor: true }),
    ...emptyWorkspaceHandlers("notes"),
  );
});

test("lists every event, filtered, with links to runs", async () => {
  const queries: URLSearchParams[] = [];
  server.use(
    http.get(`${apiUrl}/v1/audit/events`, ({ request }) => {
      queries.push(new URL(request.url).searchParams);
      return HttpResponse.json<Schemas["AuditLogEventList"]>({
        events: [
          logEvent({
            id: 2,
            actor: "",
            action: "run.finished",
            run_id: "run-1",
            target: "",
            details: { status: "failed", steps: 0, error: "boom" },
          }),
          logEvent({
            id: 1,
            actor: "",
            action: "server.started",
            workspace: "",
            target: "",
            details: { policy: [] },
          }),
        ],
      });
    }),
  );
  const { user, history } = renderApp("/audit/events", TOKEN);

  const table = await screen.findByRole("table", { name: "Events" });
  const [, finished, started] = within(table).getAllByRole("row");
  expect(finished).toHaveTextContent("Run finished");
  expect(finished).toHaveTextContent("failed: boom");
  expect(within(finished as HTMLElement).getByRole("link", { name: "run-1" })).toHaveAttribute(
    "href",
    "/audit/runs/run-1",
  );
  expect(started).toHaveTextContent("Server started");
  expect(started).toHaveTextContent("the server");

  await user.type(screen.getByRole("textbox", { name: "Who" }), "ana");
  await user.type(screen.getByRole("textbox", { name: "Action" }), "harness.changed");
  await user.click(screen.getByRole("button", { name: "Filter" }));
  await waitFor(() =>
    expect(Object.fromEntries(queries.at(-1) ?? [])).toEqual({
      actor: "ana",
      action: "harness.changed",
    }),
  );
  expect(history.location.search).toBe("?actor=ana&action=harness.changed");
});

test("is a view of the audit log beside its runs", async () => {
  server.use(
    http.get(`${apiUrl}/v1/audit/runs`, () =>
      HttpResponse.json<Schemas["AuditRunList"]>({ runs: [] }),
    ),
    http.get(`${apiUrl}/v1/audit/events`, () =>
      HttpResponse.json<Schemas["AuditLogEventList"]>({ events: [] }),
    ),
  );
  const { user } = renderApp("/audit", TOKEN);

  const views = await screen.findByRole("navigation", { name: "Audit log views" });
  expect(within(views).getByRole("link", { name: "Runs" })).toHaveAttribute("aria-current", "page");
  await user.click(within(views).getByRole("link", { name: "Events" }));
  expect(await screen.findByText("No events match.")).toBeInTheDocument();
});
