import { HttpResponse, http } from "msw";
import { setupServer } from "msw/node";
import type { Schemas } from "@/api/client";
import { apiUrl } from "@/config";

export type { Schemas };

// Answers GET /v1/conversations with pages, keyed by their before parameter,
// "" for the first, recording each request's query in queries if given.
export function conversationsHandler(
  pages: Record<string, Schemas["ConversationList"]>,
  queries: URLSearchParams[] = [],
) {
  return http.get(`${apiUrl}/v1/conversations`, ({ request }) => {
    const query = new URL(request.url).searchParams;
    queries.push(query);
    const page = pages[query.get("before") ?? ""];
    return page === undefined
      ? HttpResponse.json<Schemas["Error"]>({ error: "no such page" }, { status: 400 })
      : HttpResponse.json(page);
  });
}

// The mocked API. Tests add their handlers with server.use; a request no
// handler answers fails the test. The sidebar of every signed-in page lists
// the user's conversations, none unless a test says otherwise.
export const server = setupServer(conversationsHandler({ "": { conversations: [] } }));

export const TOKEN = "a-test-token-of-at-least-32-characters";

// Answers GET /v1/me with me, no auditor unless it says so, for TOKEN and 401
// for any other token.
export function meHandler(me: Omit<Schemas["Me"], "auditor"> & { auditor?: boolean }) {
  return http.get(`${apiUrl}/v1/me`, ({ request }) => {
    if (request.headers.get("Authorization") !== `Bearer ${TOKEN}`) {
      return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    }
    const full: Schemas["Me"] = { auditor: false, ...me };
    return HttpResponse.json(full);
  });
}

// A server-sent event stream that tests write to as the run goes.
export function liveEventStream() {
  const encoder = new TextEncoder();
  let controller: ReadableStreamDefaultController<Uint8Array> | undefined;
  const body = new ReadableStream<Uint8Array>({
    start(c) {
      controller = c;
    },
  });
  function stream(): ReadableStreamDefaultController<Uint8Array> {
    if (controller === undefined) {
      throw new Error("the stream has not started");
    }
    return controller;
  }
  return {
    response: () => new HttpResponse(body, { headers: { "Content-Type": "text/event-stream" } }),
    send(event: string, data: unknown) {
      stream().enqueue(encoder.encode(`event:${event}\ndata:${JSON.stringify(data)}\n\n`));
    },
    close() {
      stream().close();
    },
    fail() {
      stream().error(new TypeError("network error"));
    },
  };
}

// A finished run's event stream: these events, then the end.
export function eventStream(events: { event: string; data: unknown }[]) {
  const live = liveEventStream();
  const response = live.response();
  for (const { event, data } of events) {
    live.send(event, data);
  }
  live.close();
  return response;
}

// Answers what a workspace's pages load with nothing: no harnesses or waiting
// approvals.
export function emptyWorkspaceHandlers(workspace: string) {
  const base = `${apiUrl}/v1/workspaces/${workspace}`;
  return [
    http.get(`${base}/harnesses`, () => HttpResponse.json([])),
    http.get(`${base}/approvals`, () => HttpResponse.json([])),
  ];
}
