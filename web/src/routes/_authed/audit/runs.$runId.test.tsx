import { screen, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, expect, test } from "vitest";
import { apiUrl } from "@/config";
import { auditRecord, auditRun } from "@/test/fixtures";
import { renderApp } from "@/test/render";
import { emptyWorkspaceHandlers, meHandler, server, TOKEN } from "@/test/server";

beforeEach(() => {
  server.use(
    meHandler({ user: "demo", workspaces: ["notes"], auditor: true }),
    ...emptyWorkspaceHandlers("notes"),
  );
});

test("shows a run and what the gateway recorded for it", async () => {
  server.use(
    http.get(`${apiUrl}/v1/audit/runs/run-1`, () =>
      HttpResponse.json({
        run: auditRun({ started_by: "ana", error: "cancelled by ana", status: "cancelled" }),
        records: [
          auditRecord({
            event: "decision",
            decision: "require_approval",
            reason: "writes need a human",
          }),
          auditRecord({
            event: "approval",
            decision: "deny",
            approver: "ana",
            reason: "not today",
          }),
        ],
      }),
    ),
  );
  renderApp("/audit/runs/run-1", TOKEN);

  expect(await screen.findByRole("heading", { name: "notes v3" })).toBeInTheDocument();
  expect(screen.getByText("cancelled by ana")).toBeInTheDocument();
  const records = screen.getByRole("list", { name: "Audit records" });
  const [decision, approval] = within(records).getAllByRole("listitem");
  expect(decision).toHaveTextContent("Policy decision");
  expect(decision).toHaveTextContent("writes need a human");
  expect(approval).toHaveTextContent("by ana");
  expect(screen.getByRole("link", { name: "All runs" })).toHaveAttribute("href", "/audit");
});

test("says when a run recorded no tool calls", async () => {
  server.use(
    http.get(`${apiUrl}/v1/audit/runs/run-1`, () =>
      HttpResponse.json({ run: auditRun(), records: [] }),
    ),
  );
  renderApp("/audit/runs/run-1", TOKEN);

  expect(await screen.findByText("No tool calls.")).toBeInTheDocument();
});

test("a run that does not exist is not found", async () => {
  server.use(
    http.get(`${apiUrl}/v1/audit/runs/ghost`, () =>
      HttpResponse.json({ error: "run ghost: not found" }, { status: 404 }),
    ),
  );
  renderApp("/audit/runs/ghost", TOKEN);

  expect(await screen.findByRole("heading", { name: "Run not found" })).toBeInTheDocument();
});
