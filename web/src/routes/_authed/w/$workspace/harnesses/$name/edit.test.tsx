import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, test } from "vitest";
import { apiUrl } from "@/config";
import { storedHarness } from "@/test/fixtures";
import { renderApp } from "@/test/render";
import { meHandler, server, TOKEN } from "@/test/server";

const base = `${apiUrl}/v1/workspaces/notes`;
const notes = storedHarness(
  {
    name: "notes",
    instructions: "Keep the notes tidy.",
    model: { provider: "anthropic", name: "a-model" },
    tools: ["files_read_text_file"],
    limits: { max_steps: 10, max_tool_calls: 20 },
    policy: [{ name: "notes (inline policy)", source: "package agenty.tool\n" }],
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

describe("editing a harness", () => {
  test("starts from the latest version, its name fixed", async () => {
    renderApp("/w/notes/harnesses/notes/edit", TOKEN);

    expect(await screen.findByLabelText("Instructions")).toHaveValue("Keep the notes tidy.");
    expect(screen.getByLabelText("Name")).toHaveValue("notes");
    expect(screen.getByLabelText("Name")).toHaveAttribute("readonly");
    expect(screen.getByLabelText(/Granted tools/)).toHaveValue("files_read_text_file");
    expect(screen.getByLabelText("Module name")).toHaveValue("notes (inline policy)");
  });

  test("stores the change as the next version, keeping what was not changed", async () => {
    let body: unknown = null;
    server.use(
      http.put(`${base}/harnesses/notes`, async ({ request }) => {
        body = await request.json();
        return HttpResponse.json({ ...notes, version: 5 });
      }),
    );
    const { history, user } = renderApp("/w/notes/harnesses/notes/edit", TOKEN);

    const instructions = await screen.findByLabelText("Instructions");
    await user.clear(instructions);
    await user.type(instructions, "Keep them sorted.");
    await user.click(screen.getByRole("button", { name: "Save as a new version" }));

    await waitFor(() => {
      expect(history.location.pathname).toBe("/w/notes/harnesses/notes");
    });
    expect(body).toEqual({ ...notes.harness, instructions: "Keep them sorted." });
  });

  test("the harness page links to it", async () => {
    renderApp("/w/notes/harnesses/notes", TOKEN);

    expect(await screen.findByRole("link", { name: "Edit" })).toHaveAttribute(
      "href",
      "/w/notes/harnesses/notes/edit",
    );
  });
});

test("a harness that does not exist is not found", async () => {
  server.use(
    http.get(`${base}/harnesses/gone`, () =>
      HttpResponse.json({ error: "not found" }, { status: 404 }),
    ),
  );
  renderApp("/w/notes/harnesses/gone/edit", TOKEN);

  expect(await screen.findByRole("heading", { name: "Harness not found" })).toBeInTheDocument();
});
