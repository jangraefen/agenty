import { act, screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, test } from "vitest";
import { apiUrl } from "@/config";
import { harnessVersion, run } from "@/test/fixtures";
import { renderApp } from "@/test/render";
import { meHandler, type Schemas, server, TOKEN } from "@/test/server";

const runsUrl = `${apiUrl}/v1/workspaces/notes/runs`;

// Serves pages of runs: the first without before, the next after it, and
// records each request's query.
function runsHandler(pages: Record<string, Schemas["RunList"]>, queries: URLSearchParams[] = []) {
  return http.get(runsUrl, ({ request }) => {
    const query = new URL(request.url).searchParams;
    queries.push(query);
    return HttpResponse.json(pages[query.get("before") ?? ""] ?? { runs: [] });
  });
}

beforeEach(() => {
  server.use(
    meHandler({ user: "demo", workspaces: ["notes"] }),
    http.get(`${apiUrl}/v1/workspaces/notes/harnesses`, () =>
      HttpResponse.json([harnessVersion("notes"), harnessVersion("triage")]),
    ),
  );
});

function rows() {
  const [, ...body] = within(screen.getByRole("table", { name: "Runs" })).getAllByRole("row");
  return body;
}

describe("the runs page", () => {
  test("lists the workspace's runs as the API returns them", async () => {
    server.use(
      runsHandler({
        "": {
          runs: [
            run({ id: "run-2", status: "running", input: "sort the inbox", started_by: "ana" }),
            run({ id: "run-1", status: "failed", harness: "triage", harness_version: 5 }),
          ],
        },
      }),
    );
    renderApp("/w/notes/runs", TOKEN);

    await screen.findByRole("table", { name: "Runs" });
    const [first, second] = rows();
    if (first === undefined || second === undefined) {
      throw new Error("expected two rows");
    }
    expect(first).toHaveTextContent("notes v3");
    expect(first).toHaveTextContent("running");
    expect(first).toHaveTextContent("ana");
    expect(first).toHaveTextContent("sort the inbox");
    expect(second).toHaveTextContent("triage v5");
    expect(within(first).getByRole("link", { name: "notes v3" })).toHaveAttribute(
      "href",
      "/w/notes/runs/run-2",
    );
    expect(second).toHaveTextContent("failed");
    expect(within(second).getByText(/./, { selector: "time" })).toHaveAttribute(
      "dateTime",
      "2026-10-06T10:00:00Z",
    );
  });

  test("renders run input as text, never as HTML", async () => {
    server.use(runsHandler({ "": { runs: [run({ input: "<img src=x onerror=alert(1)>" })] } }));
    renderApp("/w/notes/runs", TOKEN);

    await screen.findByRole("table", { name: "Runs" });
    expect(screen.getByText("<img src=x onerror=alert(1)>")).toBeInTheDocument();
    expect(document.querySelector("img")).toBeNull();
  });

  test("says when the workspace has no runs", async () => {
    server.use(runsHandler({}));
    renderApp("/w/notes/runs", TOKEN);

    expect(await screen.findByText("No runs yet.")).toBeInTheDocument();
  });

  test("loads more runs from where the page ended", async () => {
    const queries: URLSearchParams[] = [];
    server.use(
      runsHandler(
        {
          "": { runs: [run({ id: "run-3" }), run({ id: "run-2" })], next: "run-2" },
          "run-2": { runs: [run({ id: "run-1", input: "the oldest" })] },
        },
        queries,
      ),
    );
    const { user } = renderApp("/w/notes/runs", TOKEN);

    await user.click(await screen.findByRole("button", { name: "Load more" }));

    expect(await screen.findByText("the oldest")).toBeInTheDocument();
    expect(rows()).toHaveLength(3);
    expect(queries.map((query) => query.get("before"))).toEqual([null, "run-2"]);
    expect(screen.queryByRole("button", { name: "Load more" })).not.toBeInTheDocument();
  });

  test("filters by status, keeping the filter in the URL", async () => {
    const queries: URLSearchParams[] = [];
    server.use(runsHandler({ "": { runs: [run()] } }, queries));
    const { history, user } = renderApp("/w/notes/runs", TOKEN);

    await user.selectOptions(await screen.findByLabelText("Status"), "failed");

    await waitFor(() => {
      expect(queries.at(-1)?.get("status")).toBe("failed");
    });
    expect(history.location.search).toContain("status=failed");
  });

  test("a filter whose runs cannot be loaded says so under the filters", async () => {
    server.use(
      http.get(runsUrl, ({ request }) =>
        new URL(request.url).searchParams.get("status") === "failed"
          ? HttpResponse.json({ error: "boom" }, { status: 500 })
          : HttpResponse.json({ runs: [run({ input: "all of them" })] }),
      ),
    );
    const { user } = renderApp("/w/notes/runs", TOKEN);

    await user.selectOptions(await screen.findByLabelText("Status"), "failed");
    expect(await screen.findByRole("alert")).toHaveTextContent("boom");

    await user.selectOptions(screen.getByLabelText("Status"), "");
    expect(await screen.findByText("all of them")).toBeInTheDocument();
  });

  test("filters by harness, offering the workspace's harnesses", async () => {
    const queries: URLSearchParams[] = [];
    server.use(runsHandler({ "": { runs: [run()] } }, queries));
    const { history, user } = renderApp("/w/notes/runs", TOKEN);

    const picker = await screen.findByLabelText("Harness");
    await screen.findByRole("option", { name: "triage" });
    await user.selectOptions(picker, "triage");

    await waitFor(() => {
      expect(queries.at(-1)?.get("harness")).toBe("triage");
    });
    expect(history.location.search).toContain("harness=triage");
  });

  test("applies the filters in the URL", async () => {
    const queries: URLSearchParams[] = [];
    server.use(runsHandler({}, queries));
    renderApp("/w/notes/runs?status=running&harness=notes", TOKEN);

    expect(await screen.findByText("No runs match these filters.")).toBeInTheDocument();
    expect(screen.getByLabelText("Status")).toHaveValue("running");
    expect(queries[0]?.get("status")).toBe("running");
    expect(queries[0]?.get("harness")).toBe("notes");
  });

  test("ignores a status the API does not know", async () => {
    const queries: URLSearchParams[] = [];
    server.use(runsHandler({ "": { runs: [run()] } }, queries));
    renderApp("/w/notes/runs?status=bogus", TOKEN);

    await screen.findByRole("table", { name: "Runs" });
    expect(screen.getByLabelText("Status")).toHaveValue("");
    expect(queries.map(String)).toEqual([""]);
  });

  test("sends no search parameter but the filters to the API", async () => {
    const queries: URLSearchParams[] = [];
    server.use(runsHandler({ "": { runs: [run()] } }, queries));
    renderApp("/w/notes/runs?before=run-9&limit=999&status=failed", TOKEN);

    await screen.findByRole("table", { name: "Runs" });
    expect(queries.map(String)).toEqual(["status=failed"]);
  });

  test.each(["true", "null", "1e3"])("filters by a harness named %s", async (name) => {
    const queries: URLSearchParams[] = [];
    server.use(runsHandler({ "": { runs: [run()] } }, queries));
    renderApp(`/w/notes/runs?harness=${name}`, TOKEN);

    await screen.findByRole("table", { name: "Runs" });
    expect(queries[0]?.get("harness")).toBe(name);
  });

  test("moves the focus to the first run loaded", async () => {
    server.use(
      runsHandler({
        "": { runs: [run({ id: "run-3" })], next: "run-3" },
        "run-3": { runs: [run({ id: "run-2", harness_version: 9 })] },
      }),
    );
    const { user } = renderApp("/w/notes/runs", TOKEN);

    await user.click(await screen.findByRole("button", { name: "Load more" }));

    await waitFor(() => {
      expect(screen.getByRole("link", { name: "notes v9" })).toHaveFocus();
    });
  });

  test("an empty last page moves the focus to the table", async () => {
    server.use(
      runsHandler({
        "": { runs: [run({ id: "run-3" })], next: "run-3" },
        "run-3": { runs: [] },
      }),
    );
    const { user } = renderApp("/w/notes/runs", TOKEN);

    await user.click(await screen.findByRole("button", { name: "Load more" }));

    await waitFor(() => {
      expect(screen.getByRole("table", { name: "Runs" })).toHaveFocus();
    });
  });

  test("keeps the focus on Load more while it loads and when it fails", async () => {
    let release: (() => void) | null = null;
    server.use(
      http.get(runsUrl, async ({ request }) => {
        if (!new URL(request.url).searchParams.has("before")) {
          return HttpResponse.json({ runs: [run({ id: "run-3" })], next: "run-3" });
        }
        await new Promise<void>((resolve) => {
          release = resolve;
        });
        return HttpResponse.json({ error: "boom" }, { status: 500 });
      }),
    );
    const { user } = renderApp("/w/notes/runs", TOKEN);

    const button = await screen.findByRole("button", { name: "Load more" });
    await user.click(button);
    await waitFor(() => {
      expect(release).not.toBeNull();
    });

    // Busy, but not disabled, which would drop the focus.
    expect(button).toHaveAttribute("aria-disabled", "true");
    expect(button).toBeEnabled();
    act(() => release?.());

    expect(await screen.findByRole("alert")).toHaveTextContent("boom");
    expect(button).toHaveFocus();
  });

  test("filters by a harness whose name is all digits", async () => {
    const queries: URLSearchParams[] = [];
    server.use(runsHandler({ "": { runs: [run()] } }, queries));
    renderApp("/w/notes/runs?harness=123", TOKEN);

    await screen.findByRole("table", { name: "Runs" });
    expect(queries[0]?.get("harness")).toBe("123");
    expect(screen.getByLabelText("Harness")).toHaveValue("123");
  });

  test("keeps the loaded runs when loading more fails", async () => {
    server.use(
      http.get(runsUrl, ({ request }) =>
        new URL(request.url).searchParams.has("before")
          ? HttpResponse.json({ error: "boom" }, { status: 500 })
          : HttpResponse.json({ runs: [run({ input: "kept" })], next: "run-1" }),
      ),
    );
    const { user } = renderApp("/w/notes/runs", TOKEN);

    await user.click(await screen.findByRole("button", { name: "Load more" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("boom");
    expect(screen.getByText("kept")).toBeInTheDocument();
  });

  test("says how many runs are shown, for screen readers", async () => {
    server.use(runsHandler({ "": { runs: [run({ id: "a" }), run({ id: "b" })] } }));
    renderApp("/w/notes/runs", TOKEN);

    await screen.findByRole("table", { name: "Runs" });
    expect(screen.getByRole("status")).toHaveTextContent("2 runs shown.");
  });

  test("names a running run's duration in words", async () => {
    const { finished_at: _, ...running } = run({ status: "running" });
    server.use(runsHandler({ "": { runs: [running] } }));
    renderApp("/w/notes/runs", TOKEN);

    expect(await screen.findByText("still running")).toBeInTheDocument();
  });

  test("says when the harnesses for the filter cannot be loaded", async () => {
    server.use(
      runsHandler({ "": { runs: [run()] } }),
      http.get(`${apiUrl}/v1/workspaces/notes/harnesses`, () =>
        HttpResponse.json({ error: "nope" }, { status: 500 }),
      ),
    );
    renderApp("/w/notes/runs", TOKEN);

    expect(
      await screen.findByRole("option", { name: "Harnesses could not be loaded" }),
    ).toBeDisabled();
  });

  test("reports an error from the API", async () => {
    server.use(http.get(runsUrl, () => HttpResponse.json({ error: "boom" }, { status: 500 })));
    renderApp("/w/notes/runs", TOKEN);

    expect(await screen.findByRole("alert")).toHaveTextContent("boom");
  });
});
