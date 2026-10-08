import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, expect, test } from "vitest";
import { apiUrl } from "@/config";
import { run, storedHarness } from "@/test/fixtures";
import { renderApp } from "@/test/render";
import { eventStream, meHandler, server, TOKEN } from "@/test/server";

const base = `${apiUrl}/v1/workspaces/notes`;
const notes = storedHarness(
  {
    name: "notes",
    instructions: "Keep the <b>notes</b> tidy.",
    model: { provider: "anthropic", name: "a-model" },
    tools: ["files_read_text_file", "files_write_file"],
    limits: { max_steps: 10, max_tool_calls: 20 },
    policy: [
      {
        name: "notes (inline policy)",
        source: 'package agenty.tool\n\ndeny contains "no" if { false }\n',
      },
    ],
  },
  { version: 4, created_at: "2026-10-05T08:00:00Z" },
);

beforeEach(() => {
  server.use(
    meHandler({ user: "demo", workspaces: ["notes"] }),
    http.get(`${base}/approvals`, () => HttpResponse.json([])),
    http.get(`${base}/harnesses/notes`, () => HttpResponse.json(notes)),
  );
});

test("shows the harness's latest version", async () => {
  renderApp("/w/notes/harnesses/notes", TOKEN);

  expect(await screen.findByRole("heading", { name: "notes" })).toBeInTheDocument();
  expect(screen.getByText("Version 4")).toBeInTheDocument();
  expect(screen.getByText("Keep the <b>notes</b> tidy.")).toBeInTheDocument();
  expect(screen.getByText("anthropic / a-model")).toBeInTheDocument();
  const tools = screen.getByRole("list", { name: "Granted tools" });
  expect(
    within(tools)
      .getAllByRole("listitem")
      .map((li) => li.textContent),
  ).toEqual(["files_read_text_file", "files_write_file"]);
  expect(screen.getByText("notes (inline policy)")).toBeInTheDocument();
  expect(screen.getByText(/deny contains "no"/)).toBeInTheDocument();
  expect(document.querySelector("b")).toBeNull();
});

test("shows the harness as YAML, read-only", async () => {
  renderApp("/w/notes/harnesses/notes", TOKEN);

  const figure = await screen.findByRole("figure", { name: "As YAML" });
  const yaml = figure.querySelector("pre");
  expect(yaml).toHaveTextContent("name: notes");
  expect(yaml).toHaveTextContent("max_tool_calls: 20");
});

test("starts a run and opens it", async () => {
  let body: unknown = null;
  server.use(
    http.post(`${base}/runs`, async ({ request }) => {
      body = await request.json();
      return HttpResponse.json(run({ id: "run-9", status: "running" }), { status: 201 });
    }),
    http.get(`${base}/runs/run-9`, () => HttpResponse.json(run({ id: "run-9" }))),
    http.get(`${base}/runs/run-9/events`, () => eventStream([{ event: "finished", data: run() }])),
    http.get(`${base}/runs/run-9/transcript`, () => HttpResponse.json([])),
  );
  const { history, user } = renderApp("/w/notes/harnesses/notes", TOKEN);

  await user.type(await screen.findByLabelText("Input"), "tidy my notes");
  await user.click(screen.getByRole("button", { name: "Start run" }));

  await waitFor(() => {
    expect(history.location.pathname).toBe("/w/notes/runs/run-9");
  });
  expect(body).toEqual({ harness: "notes", input: "tidy my notes" });
});

test("reports a run the server refuses to start", async () => {
  server.use(
    http.post(`${base}/runs`, () =>
      HttpResponse.json({ error: "cannot start run: the server is stopping" }, { status: 503 }),
    ),
  );
  const { user } = renderApp("/w/notes/harnesses/notes", TOKEN);

  await user.type(await screen.findByLabelText("Input"), "go");
  await user.click(screen.getByRole("button", { name: "Start run" }));

  expect(await screen.findByRole("alert")).toHaveTextContent("the server is stopping");
  const input = screen.getByLabelText("Input");
  expect(input).toHaveAttribute("aria-invalid", "true");
  expect(input).toHaveAccessibleDescription(/the server is stopping/);
});

test("links to the harness's runs", async () => {
  renderApp("/w/notes/harnesses/notes", TOKEN);

  expect(await screen.findByRole("link", { name: "Runs of this harness" })).toHaveAttribute(
    "href",
    "/w/notes/runs?harness=notes",
  );
});

test("a harness that does not exist is not found", async () => {
  server.use(
    http.get(`${base}/harnesses/missing`, () =>
      HttpResponse.json({ error: "not found" }, { status: 404 }),
    ),
  );
  renderApp("/w/notes/harnesses/missing", TOKEN);

  expect(await screen.findByRole("heading", { name: "Harness not found" })).toBeInTheDocument();
});
