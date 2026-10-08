import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, expect, test } from "vitest";
import { apiUrl } from "@/config";
import { storedHarness } from "@/test/fixtures";
import { renderApp } from "@/test/render";
import { meHandler, server, TOKEN } from "@/test/server";

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

test("offers a new conversation, before editing", async () => {
  const { history, user } = renderApp("/w/notes/harnesses/notes", TOKEN);

  const start = await screen.findByRole("link", { name: "New conversation" });
  const edit = screen.getByRole("link", { name: "Edit" });
  expect(start.compareDocumentPosition(edit) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  await user.click(start);

  await waitFor(() => {
    expect(history.location.pathname).toBe("/");
  });
  expect(history.location.search).toBe("?harness=notes%2Fnotes");
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
