import { screen, waitFor } from "@testing-library/react";
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
  expect(screen.getByText("Version 4")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Harness" })).toHaveAttribute(
    "href",
    "/w/notes/harnesses/notes",
  );
  expect(screen.queryByRole("list", { name: "Conversation" })).not.toBeInTheDocument();
  expect(screen.getByRole("textbox", { name: "Message" })).toHaveFocus();
});

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
});

test("sends nothing but whitespace", async () => {
  let sent = false;
  server.use(
    http.post(`${base}/runs`, () => {
      sent = true;
      return HttpResponse.json(run(), { status: 201 });
    }),
  );
  const { user } = renderApp(path, TOKEN);

  await user.type(await screen.findByRole("textbox", { name: "Message" }), "   {Enter}");
  await user.click(screen.getByRole("button", { name: "Send" }));

  expect(sent).toBe(false);
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
