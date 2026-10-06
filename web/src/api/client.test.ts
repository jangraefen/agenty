import { HttpResponse, http } from "msw";
import { expect, test } from "vitest";
import { Session } from "@/auth/session";
import { apiUrl } from "@/config";
import { server, TOKEN } from "@/test/server";
import { ApiError, makeClient, unwrap } from "./client";

test("requests carry the session's token as a bearer token", async () => {
  let authorization: string | null = null;
  server.use(
    http.get(`${apiUrl}/v1/me`, ({ request }) => {
      authorization = request.headers.get("Authorization");
      return HttpResponse.json({ user: "demo", workspaces: [] });
    }),
  );
  const session = new Session(localStorage);
  session.signIn(TOKEN);

  await unwrap(makeClient(session).GET("/v1/me"));

  expect(authorization).toBe(`Bearer ${TOKEN}`);
});

test("requests without a session carry no token", async () => {
  let authorization: string | null = "unset";
  server.use(
    http.get(`${apiUrl}/v1/me`, ({ request }) => {
      authorization = request.headers.get("Authorization");
      return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    }),
  );

  await expect(unwrap(makeClient(new Session(localStorage)).GET("/v1/me"))).rejects.toThrow(
    ApiError,
  );
  expect(authorization).toBeNull();
});

test("a 401 signs the session out", async () => {
  server.use(
    http.get(`${apiUrl}/v1/me`, () =>
      HttpResponse.json({ error: "unauthorized" }, { status: 401 }),
    ),
  );
  const session = new Session(localStorage);
  session.signIn(TOKEN);

  await expect(unwrap(makeClient(session).GET("/v1/me"))).rejects.toMatchObject({
    status: 401,
    message: "unauthorized",
  });
  expect(session.token).toBeNull();
});

test("other errors keep the session and carry the API's message", async () => {
  server.use(
    http.get(`${apiUrl}/v1/me`, () => HttpResponse.json({ error: "stopping" }, { status: 503 })),
  );
  const session = new Session(localStorage);
  session.signIn(TOKEN);

  await expect(unwrap(makeClient(session).GET("/v1/me"))).rejects.toMatchObject({
    status: 503,
    message: "stopping",
  });
  expect(session.token).toBe(TOKEN);
});

test("an unreachable server is an ApiError without a status", async () => {
  server.use(http.get(`${apiUrl}/v1/me`, () => HttpResponse.error()));

  await expect(unwrap(makeClient(new Session(localStorage)).GET("/v1/me"))).rejects.toMatchObject({
    status: undefined,
    message: "The server cannot be reached.",
  });
});
