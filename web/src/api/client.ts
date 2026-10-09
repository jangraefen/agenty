/**
 * The API client: the one way the frontend talks to the agenty server.
 *
 * The api/ directory is the frontend's data layer. schema.ts holds the types
 * openapi-typescript generates from schema/openapi.yaml (`task generate`), so
 * every path, parameter and body the frontend uses is checked against the
 * same spec the Go server is generated from. This module wraps openapi-fetch
 * in a client typed by those paths; queries.ts builds the TanStack Query
 * options (cache keys and fetchers) on top of it, and run-events.ts the
 * streamed query that follows a run's server-sent events. Routes and
 * components reach the API only through those two modules, or, for writes,
 * through this client and unwrap directly:
 *
 *   routes/components -> queries.ts / run-events.ts -> client.ts -> API
 *
 * App.tsx makes the one client for the app, from the Session, and hands it to
 * the router context, from which loaders and components take it.
 */
import createClient from "openapi-fetch";
import type { Session } from "@/auth/session";
import { apiUrl } from "@/config";
import type { components, paths } from "./schema";

/**
 * The app's API client, as makeClient returns it: openapi-fetch methods (GET,
 * PUT, POST, ...) typed by the spec's paths. Passed around rather than
 * imported as a singleton, so tests and the router context decide which
 * session it signs requests with.
 */
export type Api = ReturnType<typeof makeClient>;

/** The API's schemas, by name, such as Schemas["Run"]. */
export type Schemas = components["schemas"];

/**
 * An API request that failed; status is undefined when no answer came.
 *
 * unwrap throws it for every failure, so callers branch on status alone: the
 * signed-in layout redirects to sign in on 401, and lib/not-found.ts turns a
 * 404 into the route's not-found page. The message is the server's error
 * text when it sent one, and is shown to the user as plain text.
 */
export class ApiError extends Error {
  readonly status: number | undefined;

  constructor(message: string, status: number | undefined, options?: ErrorOptions) {
    super(message, options);
    this.name = "ApiError";
    this.status = status;
  }
}

/**
 * Returns the API client, typed from the OpenAPI spec. It signs requests in
 * with the session's token, unless they bring their own, and signs the
 * session out when the server refuses that token.
 *
 * Both happen in one openapi-fetch middleware. The token is read from the
 * session at request time, not when the client is made, so one client
 * serves the app across sign-ins and sign-outs.
 */
export function makeClient(session: Session) {
  const client = createClient<paths>({ baseUrl: apiUrl });
  client.use({
    onRequest({ request }) {
      // The token travels only in the Authorization header, never in the URL,
      // where it would end up in server logs, history and Referer headers.
      // A request that already carries one is left alone: the sign-in page
      // checks a candidate token with GET /v1/me before keeping it.
      if (session.token !== null && !request.headers.has("Authorization")) {
        request.headers.set("Authorization", `Bearer ${session.token}`);
      }
      return request;
    },
    onResponse({ request, response }) {
      // Sign out only when the server refused the token the session holds
      // now. A 401 for a sign-in page's candidate token, or for a request
      // sent with a token since replaced, says nothing about the current one.
      // Signing out notifies App.tsx, which clears the query cache and sends
      // the router back through its sign-in checks.
      if (
        response.status === 401 &&
        session.token !== null &&
        request.headers.get("Authorization") === `Bearer ${session.token}`
      ) {
        session.signOut();
      }
      return response;
    },
  });
  return client;
}

/** The shape of an openapi-fetch result, with data or error, and the response. */
type Result<T> =
  | { data: T; error?: never; response: Response }
  | { data?: never; error: unknown; response: Response };

/**
 * Returns a request's data, or throws an ApiError.
 *
 * openapi-fetch resolves with an error value rather than rejecting; TanStack
 * Query and the route loaders need a rejection to show a failure, so every
 * call is wrapped in unwrap. A rejected request (no answer at all) becomes an
 * ApiError without status; an answer that is an error, or not ok, one with
 * the response's status.
 */
export async function unwrap<T>(request: Promise<Result<T>>): Promise<T> {
  let result: Result<T>;
  try {
    result = await request;
  } catch (cause) {
    throw new ApiError("The server cannot be reached.", undefined, { cause });
  }
  if (result.error !== undefined || !result.response.ok) {
    throw new ApiError(errorMessage(result.error, result.response), result.response.status);
  }
  // Without an error, openapi-fetch has returned the data of the response's
  // type, which is undefined for one without a body.
  return result.data as T;
}

/**
 * The message for a failed response: the error text of the API's Error
 * schema when the body has one, else the status line. The body is unknown,
 * as a proxy or a crashed server may answer with anything, so it is checked
 * before use.
 */
function errorMessage(body: unknown, response: Response): string {
  if (typeof body === "object" && body !== null && "error" in body) {
    const { error } = body;
    if (typeof error === "string" && error !== "") {
      return error;
    }
  }
  return `${response.status} ${response.statusText}`.trim();
}
