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

  await expect(queryClient.fetchQuery(runEventsQuery(api, "notes", "run-1"))).resolves.toEqual([]);
  expect(queryClient.getQueryData(queryKey)).toEqual(conversationOf([run()]));
});
