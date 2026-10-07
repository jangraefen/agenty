import { focusManager } from "@tanstack/react-query";
import { act, screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { apiUrl } from "@/config";
import { approvalRequest, auditRecord, run } from "@/test/fixtures";
import { renderApp } from "@/test/render";
import {
  eventStream,
  liveEventStream,
  meHandler,
  type Schemas,
  server,
  TOKEN,
} from "@/test/server";

const base = `${apiUrl}/v1/workspaces/notes`;
const path = "/w/notes/runs/run-1";

afterEach(() => {
  vi.useRealTimers();
});

// Returns to the page once the run it shows is stale: a page loads its data
// before it shows, and a loaded query counts as fresh for a second.
function refocusLater() {
  vi.setSystemTime(Date.now() + 1000);
  act(() => {
    focusManager.setFocused(false);
    focusManager.setFocused(true);
  });
}

function runHandler(value: Schemas["Run"]) {
  return http.get(`${base}/runs/run-1`, () => HttpResponse.json(value));
}

function transcriptHandler(messages: Schemas["TranscriptMessage"][]) {
  return http.get(`${base}/runs/run-1/transcript`, () => HttpResponse.json(messages));
}

function approvalsHandler(requests: Schemas["ApprovalRequest"][]) {
  return http.get(`${base}/approvals`, () => HttpResponse.json(requests));
}

const { finished_at: _, ...runningRun } = run({ status: "running", output: "" });

beforeEach(() => {
  server.use(
    meHandler({ user: "demo", workspaces: ["notes"] }),
    transcriptHandler([]),
    approvalsHandler([]),
  );
});

describe("the run page", () => {
  test("shows the run", async () => {
    const finished = run({ input: "tidy my notes", output: "All tidy.", started_by: "ana" });
    server.use(
      runHandler(finished),
      http.get(`${base}/runs/run-1/events`, () =>
        eventStream([{ event: "finished", data: finished }]),
      ),
    );
    renderApp(path, TOKEN);

    expect(await screen.findByRole("heading", { name: "notes v3" })).toBeInTheDocument();
    expect(screen.getByText("succeeded")).toBeInTheDocument();
    expect(screen.getByText("ana")).toBeInTheDocument();
    expect(screen.getByText("tidy my notes")).toBeInTheDocument();
    expect(screen.getByText("All tidy.")).toBeInTheDocument();
  });

  test("shows why a run failed", async () => {
    const failed = run({ status: "failed", output: "", error: "the model refused" });
    server.use(
      runHandler(failed),
      http.get(`${base}/runs/run-1/events`, () =>
        eventStream([{ event: "finished", data: failed }]),
      ),
    );
    renderApp(path, TOKEN);

    expect(await screen.findByText("the model refused")).toBeInTheDocument();
  });

  test("follows a running run's events until it finishes", async () => {
    const live = liveEventStream();
    let authorization: string | null = null;
    server.use(
      runHandler(runningRun),
      http.get(`${base}/runs/run-1/events`, ({ request }) => {
        authorization = request.headers.get("Authorization");
        return live.response();
      }),
    );
    renderApp(path, TOKEN);
    expect(await screen.findByText("running")).toBeInTheDocument();

    live.send(
      "audit",
      auditRecord({ event: "decision", decision: "allow", tool: "files_read_file" }),
    );
    const activity = screen.getByRole("list", { name: "Activity" });
    expect(await within(activity).findByText("files_read_file")).toBeInTheDocument();

    live.send("audit", auditRecord({ event: "result", result: "the notes" }));
    live.send("finished", run({ output: "All tidy." }));
    live.close();

    expect(await screen.findByText("All tidy.")).toBeInTheDocument();
    expect(screen.getByText("succeeded")).toBeInTheDocument();
    expect(within(activity).getAllByRole("listitem")).toHaveLength(2);
    expect(authorization).toBe(`Bearer ${TOKEN}`);
  });

  test("a run response older than the run's end does not undo it", async () => {
    const live = liveEventStream();
    let hold = false;
    let release: (() => void) | null = null;
    server.use(
      http.get(`${base}/runs/run-1`, async () => {
        if (hold) {
          // A response the server produced before the run ended, arriving late.
          await new Promise<void>((resolve) => {
            release = resolve;
          });
        }
        return HttpResponse.json(runningRun);
      }),
      http.get(`${base}/runs/run-1/events`, () => live.response()),
    );
    renderApp(path, TOKEN);
    await screen.findByText("running");

    hold = true;
    refocusLater();
    await waitFor(() => {
      expect(release).not.toBeNull();
    });
    live.send("finished", run());
    live.close();
    await screen.findByText("succeeded");
    await act(async () => {
      release?.();
      await new Promise((resolve) => setTimeout(resolve, 100));
    });

    expect(screen.getByText("succeeded")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Cancel run" })).not.toBeInTheDocument();
  });

  test("keeps showing the run when refreshing it fails", async () => {
    let fail = false;
    server.use(
      http.get(`${base}/runs/run-1`, () =>
        fail ? HttpResponse.json({ error: "boom" }, { status: 500 }) : HttpResponse.json(run()),
      ),
      http.get(`${base}/runs/run-1/events`, () =>
        eventStream([{ event: "finished", data: run() }]),
      ),
    );
    renderApp(path, TOKEN);
    await screen.findByRole("heading", { name: "notes v3" });

    fail = true;
    refocusLater();

    expect(await screen.findByRole("alert")).toHaveTextContent("boom");
    expect(screen.getByRole("heading", { name: "notes v3" })).toBeInTheDocument();
  });

  test("renders what the model and tools wrote as text, never as HTML", async () => {
    const html = "<img src=x onerror=alert(1)>";
    server.use(
      runHandler(run({ input: html, output: html })),
      http.get(`${base}/runs/run-1/events`, () =>
        eventStream([
          { event: "audit", data: auditRecord({ event: "result", args: { html }, result: html }) },
          { event: "finished", data: run() },
        ]),
      ),
      transcriptHandler([
        { position: 0, role: "user", text: html, created_at: "2026-10-06T10:00:00Z" },
        {
          position: 1,
          role: "assistant",
          text: html,
          tool_calls: [{ id: "c", name: "files_read_file", args: { html } }],
          created_at: "2026-10-06T10:00:01Z",
        },
        {
          position: 2,
          role: "user",
          tool_results: [{ call_id: "c", content: html, is_error: true }],
          created_at: "2026-10-06T10:00:02Z",
        },
      ]),
    );
    renderApp(path, TOKEN);

    const transcript = await screen.findByRole("list", { name: "Transcript" });
    await within(transcript).findAllByText(html);
    expect(document.querySelector("img")).toBeNull();
  });

  test("shows the transcript", async () => {
    server.use(
      runHandler(run()),
      http.get(`${base}/runs/run-1/events`, () =>
        eventStream([{ event: "finished", data: run() }]),
      ),
      transcriptHandler([
        { position: 0, role: "user", text: "tidy my notes", created_at: "2026-10-06T10:00:00Z" },
        {
          position: 1,
          role: "assistant",
          text: "Reading them.",
          tool_calls: [{ id: "c", name: "files_read_file", args: { path: "notes.md" } }],
          created_at: "2026-10-06T10:00:01Z",
        },
        {
          position: 2,
          role: "user",
          tool_results: [{ call_id: "c", content: "the notes", is_error: true }],
          created_at: "2026-10-06T10:00:02Z",
        },
      ]),
    );
    renderApp(path, TOKEN);

    const transcript = await screen.findByRole("list", { name: "Transcript" });
    const [input, reply, results] = await within(transcript).findAllByRole("listitem");
    expect(input).toHaveTextContent("Input");
    expect(input).toHaveTextContent("tidy my notes");
    expect(reply).toHaveTextContent("Model");
    expect(reply).toHaveTextContent("Reading them.");
    expect(reply).toHaveTextContent("files_read_file");
    expect(reply).toHaveTextContent('"path": "notes.md"');
    expect(results).toHaveTextContent("Tool results");
    expect(results).toHaveTextContent("the notes");
    expect(results).toHaveTextContent("error");
  });

  test("shows the run's waiting approvals, not those of other runs", async () => {
    const live = liveEventStream();
    server.use(
      runHandler(runningRun),
      http.get(`${base}/runs/run-1/events`, () => live.response()),
      approvalsHandler([
        approvalRequest({
          id: "a",
          tool: "files_write_file",
          reasons: ["file changes need approval"],
        }),
        approvalRequest({ id: "b", run_id: "run-2", tool: "files_delete" }),
      ]),
    );
    renderApp(path, TOKEN);

    const waiting = await screen.findByRole("region", { name: "Waiting for approval" });
    expect(await within(waiting).findByText("files_write_file")).toBeInTheDocument();
    expect(within(waiting).getByText("file changes need approval")).toBeInTheDocument();
    expect(within(waiting).queryByText("files_delete")).not.toBeInTheDocument();
  });

  test("answers a waiting approval on the run's page", async () => {
    const live = liveEventStream();
    let answer: unknown = null;
    let pending = [
      approvalRequest({ id: "a", expires_at: new Date(Date.now() + 60_000).toISOString() }),
    ];
    server.use(
      runHandler(runningRun),
      http.get(`${base}/runs/run-1/events`, () => live.response()),
      http.get(`${base}/approvals`, () => HttpResponse.json(pending)),
      http.post(`${base}/runs/run-1/approvals/a`, async ({ request }) => {
        answer = await request.json();
        pending = [];
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const { user } = renderApp(path, TOKEN);

    const waiting = await screen.findByRole("region", { name: "Waiting for approval" });
    await user.click(await within(waiting).findByRole("button", { name: "Approve" }));

    await waitFor(() => {
      expect(
        screen.queryByRole("region", { name: "Waiting for approval" }),
      ).not.toBeInTheDocument();
    });
    expect(answer).toEqual({ approved: true });
    expect(screen.getByRole("status", { name: "Answers" })).toHaveTextContent(
      "Approved files_write_file.",
    );
  });

  test("refreshes the waiting approvals when the run asks for one", async () => {
    const live = liveEventStream();
    let asked = false;
    let fetched = false;
    server.use(
      runHandler(runningRun),
      http.get(`${base}/runs/run-1/events`, () => live.response()),
      http.get(`${base}/approvals`, () => {
        fetched = true;
        return HttpResponse.json(asked ? [approvalRequest()] : []);
      }),
    );
    renderApp(path, TOKEN);
    await screen.findByText("running");
    await waitFor(() => {
      expect(fetched).toBe(true);
    });

    asked = true;
    live.send("approval", approvalRequest());

    expect(await screen.findByRole("region", { name: "Waiting for approval" })).toBeInTheDocument();
  });

  test("cancels a running run", async () => {
    const live = liveEventStream();
    let cancelled = false;
    server.use(
      runHandler(runningRun),
      http.get(`${base}/runs/run-1/events`, () => live.response()),
      http.post(`${base}/runs/run-1/cancel`, () => {
        cancelled = true;
        return new HttpResponse(null, { status: 202 });
      }),
    );
    const { user } = renderApp(path, TOKEN);

    await user.click(await screen.findByRole("button", { name: "Cancel run" }));

    expect(await screen.findByText("Cancelling…")).toBeInTheDocument();
    expect(cancelled).toBe(true);
    live.send("finished", run({ status: "cancelled", error: "cancelled by demo" }));
    live.close();
    expect(await screen.findByText("cancelled")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Cancel run" })).not.toBeInTheDocument();
  });

  test("reports a cancel the server refuses", async () => {
    server.use(
      runHandler(runningRun),
      http.get(`${base}/runs/run-1/events`, () => liveEventStream().response()),
      http.post(`${base}/runs/run-1/cancel`, () =>
        HttpResponse.json({ error: "run run-1 has finished" }, { status: 409 }),
      ),
    );
    const { user } = renderApp(path, TOKEN);

    await user.click(await screen.findByRole("button", { name: "Cancel run" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("run run-1 has finished");
  });

  test("offers no cancel for a finished run", async () => {
    server.use(
      runHandler(run()),
      http.get(`${base}/runs/run-1/events`, () =>
        eventStream([{ event: "finished", data: run() }]),
      ),
    );
    renderApp(path, TOKEN);

    await screen.findByRole("heading", { name: "notes v3" });
    expect(screen.queryByRole("button", { name: "Cancel run" })).not.toBeInTheDocument();
  });

  test("reports a broken event stream and reconnects", async () => {
    const first = liveEventStream();
    let connections = 0;
    server.use(
      runHandler(runningRun),
      http.get(`${base}/runs/run-1/events`, () => {
        connections += 1;
        return connections === 1
          ? first.response()
          : eventStream([
              { event: "audit", data: auditRecord() },
              { event: "finished", data: run() },
            ]);
      }),
    );
    const { user } = renderApp(path, TOKEN);
    await screen.findByText("running");

    first.send("audit", auditRecord());
    first.fail();

    expect(await screen.findByRole("alert")).toHaveTextContent("Live updates stopped");
    await user.click(screen.getByRole("button", { name: "Reconnect" }));

    expect(await screen.findByText("succeeded")).toBeInTheDocument();
    const activity = screen.getByRole("list", { name: "Activity" });
    expect(within(activity).getAllByRole("listitem")).toHaveLength(1);
  });

  test("a run that does not exist is not found", async () => {
    server.use(
      http.get(`${base}/runs/run-1`, () =>
        HttpResponse.json({ error: "not found" }, { status: 404 }),
      ),
      http.get(`${base}/runs/run-1/events`, () =>
        HttpResponse.json({ error: "not found" }, { status: 404 }),
      ),
    );
    renderApp(path, TOKEN);

    expect(await screen.findByRole("heading", { name: "Run not found" })).toBeInTheDocument();
  });
});
