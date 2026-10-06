import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, test } from "vitest";
import { apiUrl } from "@/config";
import { storedHarness } from "@/test/fixtures";
import { renderApp } from "@/test/render";
import { meHandler, type Schemas, server, TOKEN } from "@/test/server";

const base = `${apiUrl}/v1/workspaces/notes`;

// Serves the harnesses in existing, and records each PUT, storing it as
// version 1.
function harnessServer(existing: Schemas["HarnessVersion"][] = []) {
  const puts: { name: string; body: unknown }[] = [];
  server.use(
    http.get(`${base}/harnesses`, () => HttpResponse.json(existing)),
    http.get(`${base}/harnesses/:name`, ({ params }) => {
      const found = existing.find((version) => version.harness.name === params.name);
      return found === undefined
        ? HttpResponse.json({ error: "not found" }, { status: 404 })
        : HttpResponse.json(found);
    }),
    http.put(`${base}/harnesses/:name`, async ({ request, params }) => {
      const body = (await request.json()) as Schemas["Harness"];
      puts.push({ name: String(params.name), body });
      existing.push(storedHarness(body, { version: 1 }));
      return HttpResponse.json(storedHarness(body, { version: 1 }));
    }),
  );
  return puts;
}

beforeEach(() => {
  server.use(
    meHandler({ user: "demo", workspaces: ["notes"] }),
    http.get(`${base}/approvals`, () => HttpResponse.json([])),
    http.get(`${base}/harnesses`, () => HttpResponse.json([])),
  );
});

describe("the new harness form", () => {
  test("creates a harness and opens it", async () => {
    const puts = harnessServer();
    const { history, user } = renderApp("/w/notes/new-harness", TOKEN);

    await user.type(await screen.findByLabelText("Name"), "triage");
    await user.type(screen.getByLabelText("Instructions"), "Sort the tickets.");
    await user.type(screen.getByLabelText("Model"), "a-model");
    await user.type(screen.getByLabelText(/Granted tools/), "tickets_list{Enter}tickets_label");
    await user.clear(screen.getByLabelText("Steps at most"));
    await user.type(screen.getByLabelText("Steps at most"), "5");
    await user.click(screen.getByRole("button", { name: "Create harness" }));

    await waitFor(() => {
      expect(history.location.pathname).toBe("/w/notes/harnesses/triage");
    });
    expect(puts).toEqual([
      {
        name: "triage",
        body: {
          name: "triage",
          instructions: "Sort the tickets.",
          model: { provider: "anthropic", name: "a-model" },
          tools: ["tickets_list", "tickets_label"],
          limits: { max_steps: 5, max_tool_calls: 20 },
        },
      },
    ]);
  });

  test("says what is wrong before sending anything", async () => {
    const puts = harnessServer();
    const { user } = renderApp("/w/notes/new-harness", TOKEN);

    await user.type(await screen.findByLabelText("Name"), "Bad Name");
    await user.type(screen.getByLabelText(/Granted tools/), "ok{Enter}not ok");
    await user.clear(screen.getByLabelText("Tool calls at most"));
    await user.type(screen.getByLabelText("Tool calls at most"), "0");
    await user.click(screen.getByRole("button", { name: "Create harness" }));

    const name = screen.getByLabelText("Name");
    expect(name).toHaveAttribute("aria-invalid", "true");
    expect(name).toHaveAccessibleDescription(/lowercase letters, digits and single hyphens/);
    expect(screen.getByLabelText("Instructions")).toHaveAccessibleDescription(
      /Instructions are required/,
    );
    expect(screen.getByLabelText(/Granted tools/)).toHaveAccessibleDescription(
      /not ok: use 1 to 64/,
    );
    expect(screen.getByLabelText("Tool calls at most")).toHaveAccessibleDescription(/at least 1/);
    expect(puts).toEqual([]);
  });

  test("refuses a name another harness has", async () => {
    const puts = harnessServer([storedHarness({ name: "notes" })]);
    const { user } = renderApp("/w/notes/new-harness", TOKEN);

    await user.type(await screen.findByLabelText("Name"), "notes");
    await user.type(screen.getByLabelText("Instructions"), "x");
    await user.type(screen.getByLabelText("Model"), "a-model");
    await user.click(screen.getByRole("button", { name: "Create harness" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "A harness named notes exists already",
    );
    expect(puts).toEqual([]);
  });

  test("reports what the server refuses", async () => {
    harnessServer();
    server.use(
      http.put(`${base}/harnesses/:name`, () =>
        HttpResponse.json({ error: "invalid harness: rego_parse_error" }, { status: 400 }),
      ),
    );
    const { user } = renderApp("/w/notes/new-harness", TOKEN);

    await user.type(await screen.findByLabelText("Name"), "triage");
    await user.type(screen.getByLabelText("Instructions"), "x");
    await user.type(screen.getByLabelText("Model"), "a-model");
    await user.click(screen.getByRole("button", { name: "Create harness" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("rego_parse_error");
  });

  test("adds and removes policy modules", async () => {
    const puts = harnessServer();
    const { user } = renderApp("/w/notes/new-harness", TOKEN);

    await user.type(await screen.findByLabelText("Name"), "triage");
    await user.type(screen.getByLabelText("Instructions"), "x");
    await user.type(screen.getByLabelText("Model"), "a-model");
    await user.click(screen.getByRole("button", { name: "Add policy module" }));
    await user.click(screen.getByRole("button", { name: "Add policy module" }));
    const modules = screen.getAllByRole("group", { name: /Policy module/ });
    expect(modules).toHaveLength(2);
    const [first, second] = modules as [HTMLElement, HTMLElement];
    expect(within(first).getByLabelText("Rego")).toHaveValue("package agenty.tool\n\n");
    await user.type(within(first).getByLabelText("Module name"), "limits.rego");
    await user.type(within(first).getByLabelText("Rego"), 'deny contains "no" if false');
    await user.click(within(second).getByRole("button", { name: "Remove policy module 2" }));
    await user.click(screen.getByRole("button", { name: "Create harness" }));

    await waitFor(() => {
      expect(puts).toHaveLength(1);
    });
    expect(puts[0]?.body).toMatchObject({
      policy: [
        { name: "limits.rego", source: 'package agenty.tool\n\ndeny contains "no" if false' },
      ],
    });
  });

  test("previews the harness as YAML while it is written", async () => {
    harnessServer();
    const { user } = renderApp("/w/notes/new-harness", TOKEN);

    await user.type(await screen.findByLabelText("Name"), "triage");

    const preview = screen.getByRole("figure", { name: "YAML preview" });
    expect(preview).toHaveTextContent("name: triage");
  });

  test("the harnesses page links to it", async () => {
    renderApp("/w/notes/harnesses", TOKEN);

    expect(await screen.findByRole("link", { name: "New harness" })).toHaveAttribute(
      "href",
      "/w/notes/new-harness",
    );
  });
});
