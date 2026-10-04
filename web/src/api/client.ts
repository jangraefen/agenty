// Typed client for the public API (ARCHITECTURE §13.1, §13.3). The types in schema.gen.ts are generated
// from api/openapi.yaml by `task generate`; the web app talks to the server only through this client.
import createClient, { type Client } from "openapi-fetch"
import type { components, paths } from "./schema.gen"

export type { components, paths }

/** A client for every operation of the public API. */
export type ApiClient = Client<paths>

/** RFC 9457 problem details, the body of every error response. */
export type Problem = components["schemas"]["Problem"]

export interface ApiClientOptions {
	/** Base URL of the API, e.g. from the runtime config.json. */
	baseUrl: string
	/** Fetch implementation; defaults to the global fetch. */
	fetch?: (input: Request) => Promise<Response>
}

/** Creates a client for the public API at the given base URL. */
export function createApiClient({ baseUrl, fetch }: ApiClientOptions): ApiClient {
	return createClient<paths>(fetch ? { baseUrl, fetch } : { baseUrl })
}
