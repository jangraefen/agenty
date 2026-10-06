import { HttpResponse, http } from "msw";
import { setupServer } from "msw/node";
import type { components } from "@/api/schema";
import { apiUrl } from "@/config";

export type Schemas = components["schemas"];

// The mocked API. Tests add their handlers with server.use; a request no
// handler answers fails the test.
export const server = setupServer();

export const TOKEN = "a-test-token-of-at-least-32-characters";

// Answers GET /v1/me with me for TOKEN and 401 for any other token.
export function meHandler(me: Schemas["Me"]) {
  return http.get(`${apiUrl}/v1/me`, ({ request }) => {
    if (request.headers.get("Authorization") !== `Bearer ${TOKEN}`) {
      return HttpResponse.json<Schemas["Error"]>({ error: "unauthorized" }, { status: 401 });
    }
    return HttpResponse.json(me);
  });
}
