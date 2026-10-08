import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, expect, test } from "vitest";
import { apiUrl } from "@/config";
import { auditRun } from "@/test/fixtures";
import { renderApp } from "@/test/render";
import { emptyWorkspaceHandlers, meHandler, type Schemas, server, TOKEN } from "@/test/server";

// auditRunsHandler answers GET /v1/audit/runs with pages, keyed by their
// before parameter, "" for the first, recording each request's query.
function auditRunsHandler(
  pages: Record<string, Schemas["AuditRunList"]>,
  queries: URLSearchParams[] = [],
) {
  return http.get(`${apiUrl}/v1/audit/runs`, ({ request }) => {
    const query = new URL(request.url).searchParams;
    queries.push(query);
    const page = pages[query.get("before") ?? ""];
    return page === undefined
      ? HttpResponse.json({ error: "no such page" }, { status: 400 })
      : HttpResponse.json(page);
  });
}

beforeEach(() => {
  server.use(
    meHandler({ user: "demo", workspaces: ["notes"], auditor: true }),
    ...emptyWorkspaceHandlers("notes"),
  );
});

test("lists the runs of every workspace, without what was said in them", async () => {
  server.use(
    auditRunsHandler({
      "": {
        runs: [
          auditRun({
            id: "run-2",
            workspace: "ops",
            harness: "triage",
            started_by: "ana",
            status: "waiting",
          }),
          auditRun(),
        ],
      },
    }),
  );
  renderApp("/audit", TOKEN);

  const table = await screen.findByRole("table", { name: "Runs" });
  const [, first, second] = within(table).getAllByRole("row");
  expect(first).toHaveTextContent("ops");
  expect(first).toHaveTextContent("ana");
  expect(first).toHaveTextContent("waiting");
  expect(within(first as HTMLElement).getByRole("link", { name: "triage v3" })).toHaveAttribute(
    "href",
    "/audit/runs/run-2",
  );
  expect(second).toHaveTextContent("notes");
  expect(within(table).queryByRole("columnheader", { name: "Input" })).not.toBeInTheDocument();
});

test("filters the runs", async () => {
  const queries: URLSearchParams[] = [];
  server.use(auditRunsHandler({ "": { runs: [] } }, queries));
  const { user, history } = renderApp("/audit", TOKEN);

  await screen.findByText("No runs yet.");
  await user.type(screen.getByRole("textbox", { name: "Workspace" }), "ops");
  await user.type(screen.getByRole("textbox", { name: "Started by" }), "ana");
  await user.selectOptions(screen.getByRole("combobox", { name: "Status" }), "failed");
  await user.click(screen.getByRole("button", { name: "Filter" }));

  await waitFor(() =>
    expect(Object.fromEntries(queries.at(-1) ?? [])).toEqual({
      workspace: "ops",
      started_by: "ana",
      status: "failed",
    }),
  );
  expect(history.location.search).toBe("?workspace=ops&started_by=ana&status=failed");
  expect(await screen.findByText("No runs match these filters.")).toBeInTheDocument();
});

test("loads more runs", async () => {
  server.use(
    auditRunsHandler({
      "": { runs: [auditRun({ id: "run-2" })], next: "run-2" },
      "run-2": { runs: [auditRun({ id: "run-1", harness: "older" })] },
    }),
  );
  const { user } = renderApp("/audit", TOKEN);

  await user.click(await screen.findByRole("button", { name: "Load more" }));

  expect(await screen.findByRole("link", { name: "older v3" })).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Load more" })).not.toBeInTheDocument();
});

test("is not found for anyone but an auditor", async () => {
  server.use(meHandler({ user: "demo", workspaces: ["notes"] }));
  renderApp("/audit", TOKEN);

  expect(await screen.findByRole("heading", { name: "Page not found" })).toBeInTheDocument();
});
