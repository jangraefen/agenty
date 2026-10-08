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

type Message = Schemas["TranscriptMessage"];

// Answers for the runs of one conversation: each run, the conversation from
// any of them, and each run's transcript, by run ID.
function conversationHandlers(runs: Schemas["Run"][], transcripts: Record<string, Message[]> = {}) {
  const find = (id: unknown) => runs.find((r) => r.id === id);
  return [
    http.get(`${base}/runs/:id`, ({ params }) => {
      const found = find(params.id);
      return found === undefined
        ? HttpResponse.json({ error: "not found" }, { status: 404 })
        : HttpResponse.json(found);
    }),
    http.get(`${base}/runs/:id/conversation`, ({ params }) =>
      find(params.id) === undefined
        ? HttpResponse.json({ error: "not found" }, { status: 404 })
        : HttpResponse.json(runs),
    ),
    http.get(`${base}/runs/:id/transcript`, ({ params }) =>
      HttpResponse.json(transcripts[String(params.id)] ?? []),
    ),
  ];
}

function finishedEvents(id: string, finished: Schemas["Run"]) {
  return http.get(`${base}/runs/${id}/events`, () =>
    eventStream([{ event: "finished", data: finished }]),
  );
}

function approvalsHandler(requests: Schemas["ApprovalRequest"][]) {
  return http.get(`${base}/approvals`, () => HttpResponse.json(requests));
}

function message(position: number, fields: Partial<Message>): Message {
  return { position, role: "user", created_at: "2026-10-06T10:00:00Z", ...fields };
}

const { finished_at: _, ...runningRun } = run({ status: "running", output: "" });

const second = run({
  id: "run-2",
  follows: "run-1",
  input: "and sort them",
  output: "Sorted.",
  started_by: "ben",
  created_at: "2026-10-06T10:05:00Z",
  finished_at: "2026-10-06T10:05:10Z",
});

beforeEach(() => {
  server.use(meHandler({ user: "demo", workspaces: ["notes"] }), approvalsHandler([]));
});

describe("the conversation page", () => {
  test("shows the conversation as a chat", async () => {
    const first = run({ input: "tidy my notes", output: "All tidy.", started_by: "ana" });
    server.use(
      ...conversationHandlers([first, second], {
        "run-1": [
          message(0, { text: "tidy my notes" }),
          message(1, { role: "assistant", text: "All tidy." }),
        ],
        "run-2": [
          message(0, { text: "and sort them" }),
          message(1, { role: "assistant", text: "Sorted." }),
        ],
      }),
      finishedEvents("run-2", second),
    );
    renderApp(path, TOKEN);

    expect(await screen.findByRole("heading", { name: "notes v3" })).toBeInTheDocument();
    const chat = screen.getByRole("list", { name: "Conversation" });
    await within(chat).findByText("Sorted.");
    const messages = within(chat)
      .getAllByRole("listitem")
      .filter((item) => item.dataset.from !== undefined);
    expect(messages.map((item) => [item.dataset.from, item.textContent])).toEqual([
      ["user", expect.stringContaining("tidy my notes")],
      ["agent", expect.stringContaining("All tidy.")],
      ["user", expect.stringContaining("and sort them")],
      ["agent", expect.stringContaining("Sorted.")],
    ]);
    expect(messages[0]).toHaveTextContent("ana");
    expect(messages[2]).toHaveTextContent("ben");
    expect(within(chat).getAllByText("succeeded")).toHaveLength(2);
  });

  test("shows each run's token usage, once it has any", async () => {
    const first = run({
      usage: {
        input_tokens: 500,
        output_tokens: 200,
        cache_write_tokens: 1_500,
        cache_read_tokens: 8_000,
      },
    });
    server.use(
      ...conversationHandlers([first, second], {
        "run-1": [message(0, { text: "tidy my notes" })],
        "run-2": [message(0, { text: "and sort them" })],
      }),
      finishedEvents("run-2", second),
    );
    renderApp(path, TOKEN);

    const chat = await screen.findByRole("list", { name: "Conversation" });
    expect(
      await within(chat).findByText("10k tokens in, 200 out, 80% from the cache"),
    ).toBeInTheDocument();
    expect(within(chat).queryAllByText(/tokens in/)).toHaveLength(1);
  });

  test("shows a run's tool calls with their results in the agent's reply", async () => {
    server.use(
      ...conversationHandlers([run()], {
        "run-1": [
          message(0, { text: "tidy my notes" }),
          message(1, {
            role: "assistant",
            text: "Reading them.",
            tool_calls: [
              { id: "c1", name: "files_read_file", args: { path: "notes.md" } },
              { id: "c2", name: "files_delete", args: { path: "notes.md" } },
            ],
          }),
          message(2, {
            tool_results: [
              { call_id: "c1", content: "the notes" },
              { call_id: "c2", content: "tool call denied: not granted", is_error: true },
            ],
          }),
          message(3, { role: "assistant", text: "Done." }),
        ],
      }),
      finishedEvents("run-1", run()),
    );
    const { user } = renderApp(path, TOKEN);

    const chat = await screen.findByRole("list", { name: "Conversation" });
    const read = await within(chat).findByRole("group", { name: /files_read_file/ });
    const denied = within(chat).getByRole("group", { name: /files_delete/ });
    expect(denied).toHaveAccessibleName(/error/);
    expect(read).not.toHaveAccessibleName(/error/);
    await user.click(within(read).getByText("files_read_file"));
    expect(read).toHaveTextContent('"path": "notes.md"');
    expect(read).toHaveTextContent("the notes");
    expect(denied).toHaveTextContent("tool call denied: not granted");
    expect(within(chat).getByText("Reading them.")).toBeInTheDocument();
    expect(within(chat).getByText("Done.")).toBeInTheDocument();
    expect(within(chat).queryByText("Tool results")).not.toBeInTheDocument();
  });

  test("shows why a run failed", async () => {
    const failed = run({ status: "failed", output: "", error: "the model refused" });
    server.use(...conversationHandlers([failed]), finishedEvents("run-1", failed));
    renderApp(path, TOKEN);

    expect(await screen.findByText("the model refused")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "The run failed" })).toBeInTheDocument();
  });

  test("follows the running run's events until it finishes", async () => {
    const live = liveEventStream();
    let authorization: string | null = null;
    server.use(
      ...conversationHandlers([runningRun]),
      http.get(`${base}/runs/run-1/events`, ({ request }) => {
        authorization = request.headers.get("Authorization");
        return live.response();
      }),
    );
    const { user } = renderApp(path, TOKEN);
    expect(await screen.findByText("running")).toBeInTheDocument();
    expect(screen.getByText("Working…")).toBeInTheDocument();

    live.send(
      "audit",
      auditRecord({ event: "decision", decision: "allow", tool: "files_read_file" }),
    );
    await user.click(screen.getByText("Audit log"));
    const audit = screen.getByRole("list", { name: "Audit log" });
    expect(await within(audit).findByText("files_read_file")).toBeInTheDocument();

    server.use(...conversationHandlers([run({ output: "All tidy." })]));
    live.send("audit", auditRecord({ event: "result", result: "the notes" }));
    live.send("finished", run({ output: "All tidy." }));
    live.close();

    expect(await screen.findByText("All tidy.")).toBeInTheDocument();
    expect(screen.getByText("succeeded")).toBeInTheDocument();
    expect(screen.queryByText("Working…")).not.toBeInTheDocument();
    expect(within(audit).getAllByRole("listitem")).toHaveLength(2);
    expect(authorization).toBe(`Bearer ${TOKEN}`);
  });

  test("shows an earlier run's audit log", async () => {
    let asked = false;
    server.use(
      ...conversationHandlers([run(), second]),
      finishedEvents("run-2", second),
      http.get(`${base}/runs/run-1/audit`, () => {
        asked = true;
        return HttpResponse.json([auditRecord({ tool: "files_list" })]);
      }),
    );
    const { user } = renderApp(path, TOKEN);

    const chat = await screen.findByRole("list", { name: "Conversation" });
    await within(chat).findByText("and sort them");
    expect(asked).toBe(false);
    await user.click(within(chat).getAllByText("Audit log")[0] as HTMLElement);

    expect(await within(chat).findByText("files_list")).toBeInTheDocument();
  });

  test("a run response older than the run's end does not undo it", async () => {
    const live = liveEventStream();
    let hold = false;
    let release: (() => void) | null = null;
    server.use(...conversationHandlers([runningRun]));
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
    server.use(...conversationHandlers([run()]));
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
    server.use(...conversationHandlers([run()]), finishedEvents("run-1", run()));
    server.use(
      http.get(`${base}/runs/run-1`, () =>
        fail ? HttpResponse.json({ error: "boom" }, { status: 500 }) : HttpResponse.json(run()),
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
    const finished = run({ input: html, output: html });
    server.use(
      ...conversationHandlers([finished], {
        "run-1": [
          message(0, { text: html }),
          message(1, {
            role: "assistant",
            text: html,
            tool_calls: [{ id: "c", name: "files_read_file", args: { html } }],
          }),
          message(2, { tool_results: [{ call_id: "c", content: html, is_error: true }] }),
          message(3, { role: "assistant", text: html }),
        ],
      }),
      http.get(`${base}/runs/run-1/events`, () =>
        eventStream([
          { event: "audit", data: auditRecord({ event: "result", args: { html }, result: html }) },
          { event: "finished", data: finished },
        ]),
      ),
    );
    const { user } = renderApp(path, TOKEN);

    const chat = await screen.findByRole("list", { name: "Conversation" });
    await within(chat).findAllByText(html);
    await user.click(within(chat).getByText("Audit log"));
    expect(document.querySelector("img")).toBeNull();
  });

  test("shows the running run's waiting approvals, not those of other runs", async () => {
    const live = liveEventStream();
    server.use(
      ...conversationHandlers([runningRun]),
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

  test("answers a waiting approval in the chat", async () => {
    const live = liveEventStream();
    let answer: unknown = null;
    let pending = [
      approvalRequest({ id: "a", expires_at: new Date(Date.now() + 60_000).toISOString() }),
    ];
    server.use(
      ...conversationHandlers([runningRun]),
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
      ...conversationHandlers([runningRun]),
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

  test("cancels the running run", async () => {
    const live = liveEventStream();
    let cancelled = false;
    server.use(
      ...conversationHandlers([runningRun]),
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
    const cancelledRun = run({ status: "cancelled", error: "cancelled by demo" });
    server.use(...conversationHandlers([cancelledRun]));
    live.send("finished", cancelledRun);
    live.close();
    expect(await screen.findByText("cancelled")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "The run was cancelled" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Cancel run" })).not.toBeInTheDocument();
  });

  test("reports a cancel the server refuses", async () => {
    server.use(
      ...conversationHandlers([runningRun]),
      http.get(`${base}/runs/run-1/events`, () => liveEventStream().response()),
      http.post(`${base}/runs/run-1/cancel`, () =>
        HttpResponse.json({ error: "run run-1 has finished" }, { status: 409 }),
      ),
    );
    const { user } = renderApp(path, TOKEN);

    await user.click(await screen.findByRole("button", { name: "Cancel run" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("run run-1 has finished");
  });

  test.each(["queued", "waiting"] as const)(
    "treats a %s run as one that has not finished",
    async (status) => {
      const { finished_at: _q, ...unfinishedRun } = run({ status, output: "" });
      server.use(
        ...conversationHandlers([unfinishedRun]),
        http.get(`${base}/runs/run-1/events`, () => liveEventStream().response()),
      );
      const { user } = renderApp(path, TOKEN);

      expect(await screen.findByText(status)).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "Cancel run" })).toBeInTheDocument();
      await user.type(await screen.findByRole("textbox", { name: "Message" }), "hurry{Enter}");
      expect(screen.getByRole("button", { name: "Send" })).toHaveAttribute("aria-disabled", "true");
    },
  );

  test("offers no cancel for a finished run", async () => {
    server.use(...conversationHandlers([run()]), finishedEvents("run-1", run()));
    renderApp(path, TOKEN);

    await screen.findByRole("heading", { name: "notes v3" });
    expect(screen.queryByRole("button", { name: "Cancel run" })).not.toBeInTheDocument();
  });

  test("reports a broken event stream and reconnects", async () => {
    const first = liveEventStream();
    let connections = 0;
    server.use(
      ...conversationHandlers([runningRun]),
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
    server.use(...conversationHandlers([run()]));
    await user.click(screen.getByRole("button", { name: "Reconnect" }));

    expect(await screen.findByText("succeeded")).toBeInTheDocument();
    await user.click(screen.getByText("Audit log"));
    const audit = screen.getByRole("list", { name: "Audit log" });
    expect(within(audit).getAllByRole("listitem")).toHaveLength(1);
  });

  test("a run that does not exist is not found", async () => {
    server.use(
      http.get(`${base}/runs/run-1`, () =>
        HttpResponse.json({ error: "not found" }, { status: 404 }),
      ),
    );
    renderApp(path, TOKEN);

    expect(await screen.findByRole("heading", { name: "Run not found" })).toBeInTheDocument();
  });
});

describe("replying", () => {
  test("follows up the conversation's latest run and shows the new run", async () => {
    let sent: { id: unknown; body: unknown } | null = null;
    const third = run({
      id: "run-3",
      follows: "run-2",
      input: "thanks\nbye",
      status: "running",
      output: "",
    });
    server.use(
      ...conversationHandlers([run(), second]),
      finishedEvents("run-2", second),
      http.post(`${base}/runs/:id/follow-up`, async ({ params, request }) => {
        sent = { id: params.id, body: await request.json() };
        server.use(
          ...conversationHandlers([run(), second, third]),
          http.get(`${base}/runs/run-3/events`, () => liveEventStream().response()),
        );
        return HttpResponse.json(third, { status: 201 });
      }),
    );
    const { user, history } = renderApp(path, TOKEN);

    const box = await screen.findByRole("textbox", { name: "Message" });
    await screen.findByText("Sorted.");
    await user.type(box, "thanks");
    await user.keyboard("{Shift>}{Enter}{/Shift}bye");
    expect(sent).toBeNull();
    await user.keyboard("{Enter}");

    await waitFor(() => {
      expect(history.location.pathname).toBe("/w/notes/runs/run-3");
    });
    expect(sent).toEqual({ id: "run-2", body: { input: "thanks\nbye" } });
    expect(await screen.findByText("Working…")).toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "Message" })).toHaveValue("");
  });

  test("keeps the conversation in view while the new run loads, and the focus in the box", async () => {
    const third = run({
      id: "run-3",
      follows: "run-2",
      input: "and file them",
      status: "running",
      output: "",
    });
    server.use(
      ...conversationHandlers([run(), second]),
      finishedEvents("run-2", second),
      http.post(`${base}/runs/:id/follow-up`, () => {
        server.use(
          ...conversationHandlers([run(), second, third]),
          // The new run's conversation is slow to come.
          http.get(`${base}/runs/run-3/conversation`, () => new Promise<never>(() => undefined)),
          http.get(`${base}/runs/run-3/events`, () => liveEventStream().response()),
        );
        return HttpResponse.json(third, { status: 201 });
      }),
    );
    const { user, history } = renderApp(path, TOKEN);

    const box = await screen.findByRole("textbox", { name: "Message" });
    await screen.findByText("Sorted.");
    await user.type(box, "and file them");
    await user.click(screen.getByRole("button", { name: "Send" }));

    await waitFor(() => {
      expect(history.location.pathname).toBe("/w/notes/runs/run-3");
    });
    expect(screen.getByText("Sorted.")).toBeInTheDocument();
    expect(screen.getByText("and sort them")).toBeInTheDocument();
    expect(await screen.findByText("and file them")).toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "Message" })).toHaveFocus();
  });

  test("after someone else replied first, shows their run", async () => {
    const { finished_at: _f, ...answering } = run({ ...second, status: "running", output: "" });
    server.use(
      ...conversationHandlers([run()]),
      finishedEvents("run-1", run()),
      http.post(`${base}/runs/run-1/follow-up`, () => {
        server.use(
          ...conversationHandlers([run(), answering]),
          http.get(`${base}/runs/run-2/events`, () => liveEventStream().response()),
        );
        return HttpResponse.json(
          { error: "run run-1 is already followed up; follow up the conversation's latest run" },
          { status: 409 },
        );
      }),
    );
    const { user } = renderApp(path, TOKEN);

    const box = await screen.findByRole("textbox", { name: "Message" });
    await user.type(box, "again{Enter}");

    expect(await screen.findByText("and sort them")).toBeInTheDocument();
    expect(
      await screen.findByText(/You can reply once the agent has answered/),
    ).toBeInTheDocument();
    expect(box).toHaveValue("again");
  });

  test("sends nothing but whitespace", async () => {
    server.use(...conversationHandlers([run()]), finishedEvents("run-1", run()));
    const { user } = renderApp(path, TOKEN);

    const box = await screen.findByRole("textbox", { name: "Message" });
    await user.type(box, "   ");
    await user.click(screen.getByRole("button", { name: "Send" }));

    expect(box).toHaveValue("   ");
  });

  test("waits for the running run's answer", async () => {
    server.use(
      ...conversationHandlers([runningRun]),
      http.get(`${base}/runs/run-1/events`, () => liveEventStream().response()),
    );
    const { user } = renderApp(path, TOKEN);

    const box = await screen.findByRole("textbox", { name: "Message" });
    await user.type(box, "hurry{Enter}");

    expect(screen.getByRole("button", { name: "Send" })).toHaveAttribute("aria-disabled", "true");
    expect(screen.getByText(/You can reply once the agent has answered/)).toBeInTheDocument();
    expect(box).toHaveValue("hurry");
  });

  test.each([
    ["failed", "the model refused", "The last run failed."],
    ["cancelled", "cancelled by ana", "The last run was cancelled."],
  ] as const)("continues a conversation whose last run %s", async (status, error, hint) => {
    let sent: unknown = null;
    const ended = run({ status, output: "", error });
    const next = run({
      id: "run-2",
      follows: "run-1",
      input: "try again",
      status: "running",
      output: "",
    });
    server.use(
      ...conversationHandlers([ended]),
      finishedEvents("run-1", ended),
      http.post(`${base}/runs/:id/follow-up`, ({ params }) => {
        sent = params.id;
        server.use(
          ...conversationHandlers([ended, next]),
          http.get(`${base}/runs/run-2/events`, () => liveEventStream().response()),
        );
        return HttpResponse.json(next, { status: 201 });
      }),
    );
    const { user, history } = renderApp(path, TOKEN);

    const box = await screen.findByRole("textbox", { name: "Message" });
    await waitFor(() =>
      expect(box).toHaveAccessibleDescription(`${hint} A reply continues from where it stopped.`),
    );
    await user.type(box, "try again{Enter}");

    await waitFor(() => expect(history.location.pathname).toBe("/w/notes/runs/run-2"));
    expect(sent).toBe("run-1");
  });

  test("reports a reply the server refuses and keeps the message", async () => {
    server.use(
      ...conversationHandlers([run()]),
      finishedEvents("run-1", run()),
      http.post(`${base}/runs/run-1/follow-up`, () =>
        HttpResponse.json({ error: "run run-1 is already followed up" }, { status: 409 }),
      ),
    );
    const { user } = renderApp(path, TOKEN);

    const box = await screen.findByRole("textbox", { name: "Message" });
    await user.type(box, "again{Enter}");

    expect(await screen.findByRole("alert")).toHaveTextContent("run run-1 is already followed up");
    expect(box).toHaveValue("again");
  });
});
