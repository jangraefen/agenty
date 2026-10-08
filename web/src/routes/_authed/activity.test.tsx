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

test("lists what the user did, newest first, and loads more", async () => {
  server.use(
    http.get(`${apiUrl}/v1/me/activity`, ({ request }) => {
      const before = new URL(request.url).searchParams.get("before");
      return HttpResponse.json<Schemas["AuditLogEventList"]>(
        before === null
          ? {
              events: [
                logEvent({
                  id: 3,
                  action: "tool.approval",
                  run_id: "run-1",
                  target: "",
                  details: { tool: "files_write", decision: "allow", call_id: "c1" },
                }),
                logEvent({
                  id: 2,
                  action: "run.started",
                  run_id: "run-1",
                  target: "",
                  details: { harness: "notes", version: 2 },
                }),
              ],
              next: 2,
            }
          : { events: [logEvent({ id: 1 })] },
      );
    }),
  );
  const { user } = renderApp("/activity", TOKEN);

  expect(await screen.findByRole("heading", { name: "Your activity" })).toBeInTheDocument();
  const table = await screen.findByRole("table", { name: "Events" });
  const [, approval, started] = within(table).getAllByRole("row");
  expect(approval).toHaveTextContent("Approval");
  expect(approval).toHaveTextContent("files_write: allow");
  expect(started).toHaveTextContent("Run started");
  expect(started).toHaveTextContent("notes v2");
  expect(within(table).queryByRole("columnheader", { name: "Who" })).not.toBeInTheDocument();

  await user.click(screen.getByRole("button", { name: "Load more" }));
  expect(await within(table).findByText("Harness changed")).toBeInTheDocument();
  expect(within(table).getByText("version 2")).toBeInTheDocument();
});

test("is reached from the user's name in the sidebar", async () => {
  server.use(
    http.get(`${apiUrl}/v1/me/activity`, () =>
      HttpResponse.json<Schemas["AuditLogEventList"]>({ events: [] }),
    ),
  );
  const { user } = renderApp("/", TOKEN);

  await user.click(await screen.findByRole("link", { name: "demo, your activity" }));

  expect(await screen.findByText("Nothing yet.")).toBeInTheDocument();
});
