import { act, screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, expect, test } from "vitest";
import { apiUrl } from "@/config";
import { run, storedHarness } from "@/test/fixtures";
import { renderApp } from "@/test/render";
import { liveEventStream, meHandler, server, TOKEN } from "@/test/server";

const base = `${apiUrl}/v1/workspaces/notes`;
const path = "/w/notes/harnesses/notes/new";
const notes = storedHarness(
  {
    name: "notes",
    instructions: "Keep the notes tidy.",
    model: { provider: "anthropic", name: "a-model" },
    limits: { max_steps: 10, max_tool_calls: 20 },
  },
  { version: 4 },
);

beforeEach(() => {
  server.use(
    meHandler({ user: "demo", workspaces: ["notes"] }),
    http.get(`${base}/approvals`, () => HttpResponse.json([])),
    http.get(`${base}/harnesses/notes`, () => HttpResponse.json(notes)),
  );
});

test("is an empty chat with the harness", async () => {
  renderApp(path, TOKEN);

  expect(
    await screen.findByRole("heading", { name: "New conversation with notes" }),
  ).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Back to notes" })).toHaveAttribute(
    "href",
    "/w/notes/harnesses/notes",
  );
  expect(screen.queryByRole("list", { name: "Conversation" })).not.toBeInTheDocument();
  expect(screen.getByRole("textbox", { name: "Message" })).toHaveFocus();
});

// startsWith answers a new run of the notes harness, run-9, whose first
// message is input, after answer if set, and records the request bodies it
// got in bodies.
function startsWith(input: string, bodies: unknown[], answer?: Promise<void>) {
  const first = run({ id: "run-9", input, status: "queued", output: "" });
  server.use(
    http.post(`${base}/runs`, async ({ request }) => {
      bodies.push(await request.json());
      await answer;
      return HttpResponse.json(first, { status: 201 });
    }),
    http.get(`${base}/runs/run-9`, () => HttpResponse.json(first)),
    http.get(`${base}/runs/run-9/conversation`, () => HttpResponse.json([first])),
    http.get(`${base}/runs/run-9/transcript`, () => HttpResponse.json([])),
    http.get(`${base}/runs/run-9/events`, () => liveEventStream().response()),
  );
}

test("starts the conversation with the first message and opens it", async () => {
  let body: unknown = null;
  const first = run({ id: "run-9", input: "tidy my notes", status: "queued", output: "" });
  server.use(
    http.post(`${base}/runs`, async ({ request }) => {
      body = await request.json();
      return HttpResponse.json(first, { status: 201 });
    }),
    http.get(`${base}/runs/run-9`, () => HttpResponse.json(first)),
    http.get(`${base}/runs/run-9/conversation`, () => HttpResponse.json([first])),
    http.get(`${base}/runs/run-9/transcript`, () => HttpResponse.json([])),
    http.get(`${base}/runs/run-9/events`, () => liveEventStream().response()),
  );
  const { history, user } = renderApp(path, TOKEN);

  await user.type(await screen.findByRole("textbox", { name: "Message" }), "tidy my notes{Enter}");

  await waitFor(() => {
    expect(history.location.pathname).toBe("/w/notes/runs/run-9");
  });
  expect(body).toEqual({ harness: "notes", input: "tidy my notes" });
  expect(await screen.findByText("tidy my notes")).toBeInTheDocument();
  expect(screen.getByRole("textbox", { name: "Message" })).toHaveFocus();
});

test("leaves no empty chat to go back to", async () => {
  startsWith("tidy my notes", []);
  const { history, user } = renderApp("/w/notes/harnesses/notes", TOKEN);
  await user.click(await screen.findByRole("link", { name: "New conversation" }));

  await user.type(await screen.findByRole("textbox", { name: "Message" }), "tidy my notes{Enter}");
  await waitFor(() => {
    expect(history.location.pathname).toBe("/w/notes/runs/run-9");
  });
  act(() => history.back());

  await waitFor(() => {
    expect(history.location.pathname).toBe("/w/notes/harnesses/notes");
  });
});

test("sends one message, however often it is sent while it is on its way", async () => {
  const bodies: unknown[] = [];
  let answer = () => {};
  startsWith(
    "go",
    bodies,
    new Promise<void>((resolve) => {
      answer = resolve;
    }),
  );
  const { history, user } = renderApp(path, TOKEN);

  const box = await screen.findByRole("textbox", { name: "Message" });
  await user.type(box, "go{Enter}");
  await waitFor(() => expect(bodies).toHaveLength(1));
  await user.type(box, "{Enter}");
  await user.click(screen.getByRole("button", { name: "Send" }));
  answer();

  await waitFor(() => {
    expect(history.location.pathname).toBe("/w/notes/runs/run-9");
  });
  expect(bodies).toHaveLength(1);
});

test("does not send a blank message", async () => {
  const bodies: unknown[] = [];
  startsWith("   ", bodies);
  const { history, user } = renderApp(path, TOKEN);

  const box = await screen.findByRole("textbox", { name: "Message" });
  await user.type(box, "   {Enter}");
  await user.click(screen.getByRole("button", { name: "Send" }));

  expect(history.location.pathname).toBe(path);
  expect(box).not.toHaveAttribute("aria-invalid", "true");
  expect(bodies).toEqual([]);
});

test("reports a run the server refuses to start and keeps the message", async () => {
  server.use(
    http.post(`${base}/runs`, () =>
      HttpResponse.json({ error: "cannot start run: the server is stopping" }, { status: 503 }),
    ),
  );
  const { user } = renderApp(path, TOKEN);

  const box = await screen.findByRole("textbox", { name: "Message" });
  await user.type(box, "go{Enter}");

  expect(await screen.findByRole("alert")).toHaveTextContent("the server is stopping");
  expect(box).toHaveAttribute("aria-invalid", "true");
  expect(box).toHaveAccessibleDescription(/the server is stopping/);
  expect(box).toHaveValue("go");
});
