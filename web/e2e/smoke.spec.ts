import { expect, test } from "@playwright/test";
import { token } from "../playwright.config";

test("signs in, makes a harness, chats with it, and signs out", async ({ page }) => {
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
  await expect(page).toHaveURL((url) => url.pathname === "/");
  await expect(page.getByRole("heading", { name: "New chat" })).toBeVisible();
  expect(page.url()).not.toContain(token);

  // A harness without tools, made with the form; its runs fail, as the
  // model provider is a closed port.
  const manage = page.getByRole("navigation", { name: "Manage" });
  await manage.getByRole("link", { name: "Harnesses" }).click();
  await page.getByRole("link", { name: "New harness" }).click();
  // A name of its own, so the test also runs against a database it ran on.
  const name = `smoke-${Date.now().toString(36)}`;
  await page.getByLabel("Name", { exact: true }).fill(name);
  await page.getByLabel("Instructions").fill("Answer briefly.");
  await page.getByLabel("Model").fill("claude-haiku-4-5");
  await page.getByLabel("Steps at most").fill("2");
  await page.getByRole("button", { name: "Create harness" }).click();
  await expect(page).toHaveURL(new RegExp(`/w/smoke/harnesses/${name}$`));
  await expect(page.getByRole("figure", { name: "As YAML" })).toContainText(`name: ${name}`);

  await page.getByRole("link", { name: "New conversation" }).click();
  await expect(page.getByLabel("Harness")).toHaveValue(`smoke/${name}`);
  await page.getByLabel("Message").fill("Say hello.");
  await page.getByRole("button", { name: "Send" }).click();
  await expect(page).toHaveURL(/\/c\/[^/]+$/);
  await expect(
    page
      .getByRole("navigation", { name: "Recent chats" })
      .getByRole("link", { name: "Say hello." }),
  ).toBeVisible();
  await expect(page.getByText("failed", { exact: true })).toBeVisible({ timeout: 45_000 });
  await expect(page.getByRole("heading", { name: "The run failed" })).toBeVisible();
  await expect(
    page.getByText("The last run failed. A reply continues from where it stopped."),
  ).toBeVisible();
  await page.getByLabel("Message").fill("Try again.");
  await page.getByRole("button", { name: "Send" }).click();
  await expect(page.getByText("failed", { exact: true })).toHaveCount(2, { timeout: 45_000 });

  await manage.getByRole("link", { name: "Approvals" }).click();
  await expect(page.getByText("Nothing is waiting for approval.")).toBeVisible();

  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/sign-in$/);
  expect(await page.evaluate(() => localStorage.getItem("agenty.token"))).toBeNull();

  expect(problems).toEqual([]);
});
