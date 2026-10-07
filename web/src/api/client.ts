import createClient from "openapi-fetch";
import type { Session } from "@/auth/session";
import { apiUrl } from "@/config";
import type { components, paths } from "./schema";

export type Api = ReturnType<typeof makeClient>;

/** The API's schemas, by name, such as Schemas["Run"]. */
export type Schemas = components["schemas"];

/** An API request that failed; status is undefined when no answer came. */
export class ApiError extends Error {
  readonly status: number | undefined;

  constructor(message: string, status: number | undefined, options?: ErrorOptions) {
    super(message, options);
    this.name = "ApiError";
    this.status = status;
  }
}

// makeClient returns the API client, typed from the OpenAPI spec. It signs
// requests in with the session's token, unless they bring their own, and
// signs the session out when the server refuses that token.
export function makeClient(session: Session) {
  const client = createClient<paths>({ baseUrl: apiUrl });
  client.use({
    onRequest({ request }) {
      if (session.token !== null && !request.headers.has("Authorization")) {
        request.headers.set("Authorization", `Bearer ${session.token}`);
      }
      return request;
    },
    onResponse({ request, response }) {
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

type Result<T> =
  | { data: T; error?: never; response: Response }
  | { data?: never; error: unknown; response: Response };

/** Returns a request's data, or throws an ApiError. */
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

function errorMessage(body: unknown, response: Response): string {
  if (typeof body === "object" && body !== null && "error" in body) {
    const { error } = body;
    if (typeof error === "string" && error !== "") {
      return error;
    }
  }
  return `${response.status} ${response.statusText}`.trim();
}
