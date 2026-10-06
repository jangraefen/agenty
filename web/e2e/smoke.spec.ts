import { expect, test } from "@playwright/test";
import { api, token } from "../playwright.config";

// A harness without tools; its runs fail, as the model key is a dummy.
const harness = {
  name: "smoke",
  instructions: "Answer briefly.",
  model: { provider: "anthropic", name: "claude-haiku-4-5" },
  tools: [],
  limits: { max_steps: 2, max_tool_calls: 1 },
};

test.beforeAll(async ({ request }) => {
  const response = await request.put(`${api}/v1/workspaces/smoke/harnesses/smoke`, {
    headers: { Authorization: `Bearer ${token}` },
    data: harness,
  });
  expect(response.ok()).toBe(true);
});

test("signs in, starts a run, follows it to its end, and signs out", async ({ page }) => {
  const problems: string[] = [];
  page.on("console", (message) => {
    if (message.type() === "error" || message.type() === "warning") {
      problems.push(message.text());
    }
  });
  page.on("pageerror", (error) => problems.push(error.message));

  await page.goto("/");
  await expect(page).toHaveURL(/\/sign-in$/);
  await expect(page.locator('meta[http-equiv="Content-Security-Policy"]')).toHaveAttribute(
    "content",
    /default-src 'none'.*connect-src http:\/\/127\.0\.0\.1:18080/,
  );

  await page.getByLabel("Token").fill(token);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(/\/w\/smoke\/runs$/);
  expect(page.url()).not.toContain(token);

  const pages = page.getByRole("navigation", { name: "Pages" });
  await pages.getByRole("link", { name: "Harnesses" }).click();
  await page.getByRole("link", { name: "smoke", exact: true }).click();
  await expect(page.getByRole("figure", { name: "As YAML" })).toContainText("name: smoke");

  await page.getByLabel("Input").fill("Say hello.");
  await page.getByRole("button", { name: "Start run" }).click();
  await expect(page).toHaveURL(/\/w\/smoke\/runs\/[^/]+$/);
  await expect(page.getByText("failed", { exact: true })).toBeVisible({ timeout: 45_000 });
  await expect(page.getByRole("heading", { name: "Error" })).toBeVisible();

  await pages.getByRole("link", { name: "Approvals" }).click();
  await expect(page.getByText("Nothing is waiting for approval.")).toBeVisible();

  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/sign-in$/);
  expect(await page.evaluate(() => localStorage.getItem("agenty.token"))).toBeNull();

  expect(problems).toEqual([]);
});
