import { act, screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, expect, test } from "vitest";
import { apiUrl } from "@/config";
import { conversation, harnessVersion, run } from "@/test/fixtures";
import { renderApp } from "@/test/render";
import {
  conversationsHandler,
  emptyWorkspaceHandlers,
  liveEventStream,
  meHandler,
  server,
  TOKEN,
} from "@/test/server";

const base = `${apiUrl}/v1/workspaces/notes`;
const path = "/";

beforeEach(() => {
  server.use(
    meHandler({ user: "demo", workspaces: ["notes"] }),
    http.get(`${base}/harnesses`, () => HttpResponse.json([harnessVersion("notes")])),
    http.get(`${base}/harnesses/notes`, () => HttpResponse.json(harnessVersion("notes"))),
    ...emptyWorkspaceHandlers("notes"),
  );
});

// twoWorkspaces makes the user a member of notes, with the harnesses notes
// and tidy, and of work, with the harness triage.
function twoWorkspaces() {
  server.use(
    meHandler({ user: "demo", workspaces: ["notes", "work"] }),
    http.get(`${base}/harnesses`, () =>
      HttpResponse.json([harnessVersion("notes"), harnessVersion("tidy")]),
    ),
    http.get(`${apiUrl}/v1/workspaces/work/harnesses`, () =>
      HttpResponse.json([harnessVersion("triage")]),
    ),
    ...emptyWorkspaceHandlers("work"),
  );
}

test("is an empty chat with the harness", async () => {
  renderApp(path, TOKEN);

  expect(await screen.findByRole("heading", { name: "New chat" })).toBeInTheDocument();
  expect(await screen.findByRole("combobox", { name: "Harness" })).toHaveValue("notes/notes");
  expect(screen.queryByRole("group")).not.toBeInTheDocument();
  expect(screen.queryByRole("list", { name: "Conversation" })).not.toBeInTheDocument();
  expect(screen.getByRole("textbox", { name: "Message" })).toHaveFocus();
});

test("offers the harnesses of every workspace, by workspace", async () => {
  twoWorkspaces();
  renderApp(path, TOKEN);

  const picker = await screen.findByRole("combobox", { name: "Harness" });
  await within(picker).findByRole("option", { name: "triage" });
  const groups = within(picker).getAllByRole("group");
  expect(groups.map((group) => group.getAttribute("label"))).toEqual(["notes", "work"]);
  expect(within(groups[1] as HTMLElement).getByRole("option", { name: "triage" })).toHaveValue(
    "work/triage",
  );
  expect(picker).toHaveValue("notes/notes");
});

test("picks the harness the link asks for over that of the latest chat", async () => {
  twoWorkspaces();
  server.use(
    conversationsHandler({
      "": { conversations: [conversation({ workspace: "notes", harness: "tidy" })] },
    }),
  );
  renderApp("/?harness=work/triage", TOKEN);
  const picker = await screen.findByRole("combobox", { name: "Harness" });
  await waitFor(() => expect(picker).toHaveValue("work/triage"));
});

test("picks the harness of the latest chat", async () => {
  twoWorkspaces();
  server.use(
    conversationsHandler({
      "": { conversations: [conversation({ workspace: "notes", harness: "tidy" })] },
    }),
  );
  renderApp("/?harness=work/ghost", TOKEN);

  const picker = await screen.findByRole("combobox", { name: "Harness" });
  await waitFor(() => expect(picker).toHaveValue("notes/tidy"));
});

test("starts the chat in the picked harness's workspace", async () => {
  twoWorkspaces();
  let body: unknown = null;
  const first = run({ id: "run-9", harness: "triage", input: "sort the inbox", status: "queued" });
  const work = `${apiUrl}/v1/workspaces/work`;
  server.use(
    http.post(`${work}/runs`, async ({ request }) => {
      body = await request.json();
      return HttpResponse.json(first, { status: 201 });
    }),
    http.get(`${apiUrl}/v1/conversations/run-9`, () =>
      HttpResponse.json(
        conversation({
          id: "run-9",
          workspace: "work",
          harness: "triage",
          title: "sort the inbox",
        }),
      ),
    ),
    http.get(`${work}/runs/run-9`, () => HttpResponse.json(first)),
    http.get(`${work}/runs/run-9/conversation`, () => HttpResponse.json([first])),
    http.get(`${work}/runs/run-9/transcript`, () => HttpResponse.json([])),
    http.get(`${work}/runs/run-9/events`, () => liveEventStream().response()),
  );
  const { history, user } = renderApp(path, TOKEN);

  const picker = await screen.findByRole("combobox", { name: "Harness" });
  await within(picker).findByRole("option", { name: "triage" });
  await user.selectOptions(picker, "work/triage");
  await user.type(screen.getByRole("textbox", { name: "Message" }), "sort the inbox{Enter}");

  await waitFor(() => expect(history.location.pathname).toBe("/c/run-9"));
  expect(body).toEqual({ harness: "triage", input: "sort the inbox" });
  expect(await screen.findByText("triage · work")).toBeInTheDocument();
});

test("says so when there is no harness to chat with", async () => {
  server.use(http.get(`${base}/harnesses`, () => HttpResponse.json([])));
  renderApp(path, TOKEN);

  expect(await screen.findByText(/No harness to chat with yet/)).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Manage harnesses" })).toHaveAttribute(
    "href",
    "/w/notes/harnesses",
  );
  expect(screen.queryByRole("textbox", { name: "Message" })).not.toBeInTheDocument();
});

test("says so when the user is in no workspace", async () => {
  server.use(meHandler({ user: "demo", workspaces: [] }));
  renderApp(path, TOKEN);

  expect(await screen.findByText("You are not a member of any workspace yet.")).toBeInTheDocument();
  expect(screen.queryByRole("navigation", { name: "Manage" })).not.toBeInTheDocument();
});

test("offers the harnesses that loaded when a workspace's do not", async () => {
  twoWorkspaces();
  server.use(
    http.get(`${apiUrl}/v1/workspaces/work/harnesses`, () =>
      HttpResponse.json({ error: "boom" }, { status: 500 }),
    ),
  );
  renderApp(path, TOKEN);

  expect(await screen.findByRole("alert")).toHaveTextContent("boom");
  expect(screen.getByRole("combobox", { name: "Harness" })).toHaveValue("notes/notes");
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
    http.get(`${apiUrl}/v1/conversations/run-9`, () =>
      HttpResponse.json(conversation({ id: "run-9", title: input })),
    ),
    http.get(`${base}/runs/run-9`, () => HttpResponse.json(first)),
    http.get(`${base}/runs/run-9/conversation`, () => HttpResponse.json([first])),
    http.get(`${base}/runs/run-9/transcript`, () => HttpResponse.json([])),
    http.get(`${base}/runs/run-9/events`, () => liveEventStream().response()),
  );
}

test("starts the conversation with the first message and opens it", async () => {
  const bodies: unknown[] = [];
  startsWith("tidy my notes", bodies);
  const { history, user } = renderApp(path, TOKEN);

  await user.type(await screen.findByRole("textbox", { name: "Message" }), "tidy my notes{Enter}");

  await waitFor(() => {
    expect(history.location.pathname).toBe("/c/run-9");
  });
  expect(bodies).toEqual([{ harness: "notes", input: "tidy my notes" }]);
  const chat = await screen.findByRole("list", { name: "Conversation" });
  expect(within(chat).getByText("tidy my notes")).toBeInTheDocument();
  expect(screen.getByPlaceholderText("Write a reply…")).toHaveFocus();
});

test("leaves no empty chat to go back to", async () => {
  startsWith("tidy my notes", []);
  const { history, user } = renderApp("/w/notes/harnesses/notes", TOKEN);
  await user.click(await screen.findByRole("link", { name: "New conversation" }));
  await waitFor(() => expect(history.location.search).toBe("?harness=notes%2Fnotes"));

  await user.type(await screen.findByRole("textbox", { name: "Message" }), "tidy my notes{Enter}");
  await waitFor(() => {
    expect(history.location.pathname).toBe("/c/run-9");
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
    expect(history.location.pathname).toBe("/c/run-9");
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
