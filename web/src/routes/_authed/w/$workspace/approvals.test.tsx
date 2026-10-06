import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, test } from "vitest";
import { apiUrl } from "@/config";
import { approvalRequest } from "@/test/fixtures";
import { renderApp } from "@/test/render";
import { meHandler, type Schemas, server, TOKEN } from "@/test/server";

const base = `${apiUrl}/v1/workspaces/notes`;
const inAnHour = () => new Date(Date.now() + 3_600_000).toISOString();

// Serves the waiting approvals, dropping each one once it is answered, and
// records the answers.
function approvalsServer(waiting: Schemas["ApprovalRequest"][]) {
  const answers: { url: string; body: unknown; authorization: string | null }[] = [];
  let pending = [...waiting];
  server.use(
    http.get(`${base}/approvals`, () => HttpResponse.json(pending)),
    http.post(`${base}/runs/:run/approvals/:approval`, async ({ request, params }) => {
      answers.push({
        url: new URL(request.url).pathname,
        body: await request.json(),
        authorization: request.headers.get("Authorization"),
      });
      pending = pending.filter((approval) => approval.id !== params.approval);
      return new HttpResponse(null, { status: 204 });
    }),
  );
  return answers;
}

beforeEach(() => {
  server.use(meHandler({ user: "demo", workspaces: ["notes"] }));
});

function card(tool: string) {
  const item = screen
    .getAllByRole("listitem")
    .find((li) => within(li).queryByText(tool, { selector: "code" }) !== null);
  if (item === undefined) {
    throw new Error(`no request for ${tool}`);
  }
  return item;
}

describe("the approvals page", () => {
  test("lists the waiting requests with what they would do", async () => {
    approvalsServer([
      approvalRequest({
        id: "a",
        run_id: "run-7",
        harness: "notes",
        tool: "files_write_file",
        args: { path: "notes.md" },
        reasons: ["file changes need approval"],
        expires_at: inAnHour(),
      }),
    ]);
    renderApp("/w/notes/approvals", TOKEN);

    expect(await screen.findByRole("heading", { name: "Approvals" })).toBeInTheDocument();
    await screen.findByText("files_write_file", { selector: "code" });
    const request = card("files_write_file");
    expect(request).toHaveTextContent("file changes need approval");
    expect(request).toHaveTextContent('"path": "notes.md"');
    expect(request).toHaveTextContent(/59m \d+s left|1h 0m left/);
    expect(within(request).getByRole("link", { name: /notes/ })).toHaveAttribute(
      "href",
      "/w/notes/runs/run-7",
    );
  });

  test("approves a request as the signed-in user", async () => {
    const answers = approvalsServer([
      approvalRequest({ id: "a", run_id: "run-7", expires_at: inAnHour() }),
    ]);
    const { user } = renderApp("/w/notes/approvals", TOKEN);

    await screen.findByText("files_write_file", { selector: "code" });
    await user.click(within(card("files_write_file")).getByRole("button", { name: "Approve" }));

    expect(await screen.findByText("Nothing is waiting for approval.")).toBeInTheDocument();
    expect(screen.getByRole("status", { name: "Answers" })).toHaveTextContent(
      "Approved files_write_file.",
    );
    expect(answers).toEqual([
      {
        url: "/v1/workspaces/notes/runs/run-7/approvals/a",
        body: { approved: true },
        authorization: `Bearer ${TOKEN}`,
      },
    ]);
  });

  test("rejects a request with a reason", async () => {
    const answers = approvalsServer([approvalRequest({ id: "a", expires_at: inAnHour() })]);
    const { user } = renderApp("/w/notes/approvals", TOKEN);

    await screen.findByText("files_write_file", { selector: "code" });
    const request = card("files_write_file");
    await user.type(within(request).getByLabelText("Reason (optional)"), "  not today  ");
    await user.click(within(request).getByRole("button", { name: "Reject" }));

    await screen.findByText("Nothing is waiting for approval.");
    expect(answers.map((answer) => answer.body)).toEqual([
      { approved: false, reason: "not today" },
    ]);
  });

  test("reports a request that was answered or expired meanwhile", async () => {
    // As the server does: once answered elsewhere, the request is no longer listed.
    let gone = false;
    server.use(
      http.get(`${base}/approvals`, () =>
        HttpResponse.json(gone ? [] : [approvalRequest({ id: "a", expires_at: inAnHour() })]),
      ),
      http.post(`${base}/runs/:run/approvals/:approval`, () => {
        gone = true;
        return HttpResponse.json(
          { error: "run run-1 is not waiting for approval a" },
          { status: 404 },
        );
      }),
    );
    const { user } = renderApp("/w/notes/approvals", TOKEN);

    await screen.findByText("files_write_file", { selector: "code" });
    await user.click(within(card("files_write_file")).getByRole("button", { name: "Approve" }));

    await screen.findByText("Nothing is waiting for approval.");
    expect(screen.getByRole("alert")).toHaveTextContent(
      "files_write_file was already answered, or it expired: your answer did not count.",
    );
  });

  test("an expired request can no longer be answered", async () => {
    approvalsServer([approvalRequest({ id: "a", expires_at: "2020-01-01T00:00:00Z" })]);
    renderApp("/w/notes/approvals", TOKEN);

    await screen.findByText("files_write_file", { selector: "code" });
    const request = card("files_write_file");
    expect(request).toHaveTextContent("expired");
    expect(within(request).getByRole("button", { name: "Approve" })).toHaveAttribute(
      "aria-disabled",
      "true",
    );
    expect(within(request).getByRole("button", { name: "Reject" })).toHaveAttribute(
      "aria-disabled",
      "true",
    );
    // Read-only, not disabled, so a user typing a reason keeps the focus.
    const reason = within(request).getByLabelText("Reason (optional)");
    expect(reason).toHaveAttribute("readonly");
    expect(reason).toBeEnabled();
  });

  test("names the call each button answers", async () => {
    approvalsServer([
      approvalRequest({ id: "a", tool: "files_write_file", expires_at: inAnHour() }),
    ]);
    renderApp("/w/notes/approvals", TOKEN);

    expect(await screen.findByRole("button", { name: "Approve" })).toHaveAccessibleDescription(
      "files_write_file",
    );
    expect(screen.getByRole("button", { name: "Reject" })).toHaveAccessibleDescription(
      "files_write_file",
    );
  });

  test("an expired request ignores clicks", async () => {
    const answers = approvalsServer([
      approvalRequest({ id: "a", expires_at: "2020-01-01T00:00:00Z" }),
    ]);
    const { user } = renderApp("/w/notes/approvals", TOKEN);

    await user.click(await screen.findByRole("button", { name: "Approve" }));

    expect(answers).toEqual([]);
  });

  test("says when nothing is waiting", async () => {
    approvalsServer([]);
    renderApp("/w/notes/approvals", TOKEN);

    expect(await screen.findByText("Nothing is waiting for approval.")).toBeInTheDocument();
  });

  test("the header links to the approvals and counts the waiting ones", async () => {
    approvalsServer([
      approvalRequest({ id: "a", expires_at: inAnHour() }),
      approvalRequest({ id: "b", expires_at: inAnHour() }),
    ]);
    server.use(http.get(`${base}/runs`, () => HttpResponse.json({ runs: [] })));
    server.use(http.get(`${base}/harnesses`, () => HttpResponse.json([])));
    const { history, user } = renderApp("/w/notes/runs", TOKEN);

    const pages = await screen.findByRole("navigation", { name: "Pages" });
    const link = within(pages).getByRole("link", { name: /Approvals/ });
    await waitFor(() => {
      expect(link).toHaveAccessibleName("Approvals (2 waiting)");
    });
    await user.click(link);

    await waitFor(() => {
      expect(history.location.pathname).toBe("/w/notes/approvals");
    });
  });
});
