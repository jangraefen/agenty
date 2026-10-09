import { expect, test } from "@playwright/test";

test("health endpoint reports ok and is not cached", async ({ request }) => {
  const response = await request.get("/api/health");
  expect(response.status()).toBe(200);
  expect(await response.json()).toEqual({ status: "ok", db: "ok" });
  expect(response.headers()["cache-control"]).toContain("no-store");
});
