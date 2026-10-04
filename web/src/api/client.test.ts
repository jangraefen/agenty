import { describe, expect, it } from "vitest"
import { createApiClient } from "./client"

/** Returns a fetch that records each request and answers with the given response. */
function fakeFetch(status: number, contentType: string, body: unknown) {
	const requests: Request[] = []
	const fetch = (input: Request) => {
		requests.push(input)
		return Promise.resolve(
			new Response(JSON.stringify(body), {
				status,
				headers: { "Content-Type": contentType },
			})
		)
	}
	return { fetch, requests }
}

describe("createApiClient", () => {
	it("requests operations relative to the base URL", async () => {
		const { fetch, requests } = fakeFetch(200, "application/json", { status: "ok", roles: ["api"] })
		const client = createApiClient({ baseUrl: "https://agenty.example/base", fetch })

		const { data, error } = await client.GET("/healthz")

		expect(error).toBeUndefined()
		expect(data).toEqual({ status: "ok", roles: ["api"] })
		expect(requests).toHaveLength(1)
		expect(requests[0]?.method).toBe("GET")
		expect(requests[0]?.url).toBe("https://agenty.example/base/healthz")
	})

	it("returns problem details as the error", async () => {
		const problem = {
			type: "about:blank",
			title: "Service Unavailable",
			status: 503,
			detail: "The server is not ready to serve traffic.",
			instance: "/readyz",
		}
		const { fetch } = fakeFetch(503, "application/problem+json", problem)
		const client = createApiClient({ baseUrl: "https://agenty.example", fetch })

		const { data, error, response } = await client.GET("/readyz")

		expect(data).toBeUndefined()
		expect(response.status).toBe(503)
		expect(error).toEqual(problem)
	})

	it("uses the global fetch by default", () => {
		const client = createApiClient({ baseUrl: "https://agenty.example" })

		expect(typeof client.GET).toBe("function")
	})
})
