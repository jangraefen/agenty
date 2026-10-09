import { QueryClient } from "@tanstack/react-query";
import { http } from "msw";
import { expect, test } from "vitest";
import { Session } from "@/auth/session";
import { apiUrl } from "@/config";
import { conversationOf, run } from "@/test/fixtures";
import { eventStream, server, TOKEN } from "@/test/server";
import { makeClient } from "./client";
import { conversationQuery } from "./queries";
import { runEventsQuery } from "./run-events";

test("following a run to its end does not cancel itself, and stores the run in its conversation", async () => {
  server.use(
    http.get(`${apiUrl}/v1/workspaces/notes/runs/run-1/events`, () =>
      eventStream([{ event: "finished", data: run() }]),
    ),
  );
  const session = new Session(localStorage);
  session.signIn(TOKEN);
  const api = makeClient(session);
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const { queryKey } = conversationQuery(api, "run-1");
  queryClient.setQueryData(queryKey, conversationOf([run({ status: "running", output: "" })]));

  await expect(
    queryClient.fetchQuery(runEventsQuery(api, "notes", "run-1", "run-1")),
  ).resolves.toEqual([]);
  expect(queryClient.getQueryData(queryKey)).toEqual(conversationOf([run()]));
});

test("a run's end reaches its conversation also when the server could not say which it is", async () => {
  // A server that cannot read the finished run sends what it knows of it.
  const end = { ...run({ id: "run-2", status: "failed", error: "boom" }), conversation_id: "" };
  server.use(
    http.get(`${apiUrl}/v1/workspaces/notes/runs/run-2/events`, () =>
      eventStream([{ event: "finished", data: end }]),
    ),
  );
  const session = new Session(localStorage);
  session.signIn(TOKEN);
  const api = makeClient(session);
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const { queryKey } = conversationQuery(api, "run-1");
  queryClient.setQueryData(
    queryKey,
    conversationOf([run(), run({ id: "run-2", status: "running" })]),
  );

  await queryClient.fetchQuery(runEventsQuery(api, "notes", "run-1", "run-2"));

  expect(queryClient.getQueryData(queryKey)).toMatchObject({ status: "failed" });
});
