import { expect, type Page, test } from "@playwright/test";
import { loginAtMockIdp, uniqueIdpUser } from "./support/mock-idp";

/** Opens the sign-in page once it is hydrated (the button is disabled until then). */
async function gotoSignIn(page: Page, query = "") {
  await page.goto(`/sign-in${query}`);
  await expect(page.getByRole("button", { name: "Sign in", exact: true })).toBeEnabled();
}

// Next's route announcer is also a role=alert element, so match the text.
const alertWith = (page: Page, text: string) => page.getByRole("alert").filter({ hasText: text });

test("signed-out visitors see the landing page without a header", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { level: 1, name: "Agenty", exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "Sign in", exact: true })).toBeVisible();
  await expect(page.getByRole("banner")).toHaveCount(0);
});

test("signing in through the identity provider shows the home page and the user", async ({
  page,
}) => {
  const user = uniqueIdpUser("ada", "Ada Lovelace");
  await gotoSignIn(page);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await loginAtMockIdp(page, user);

  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByText("Agents arrive in the next milestone")).toBeVisible();
  await expect(
    page.getByRole("banner").getByRole("button", { name: user.name, exact: true }),
  ).toBeVisible();
});

test("signing out ends the session and returns to the landing page", async ({ page }) => {
  const user = uniqueIdpUser("grace", "Grace Hopper");
  await gotoSignIn(page);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await loginAtMockIdp(page, user);
  await expect(page.getByText("Agents arrive in the next milestone")).toBeVisible();

  await page.getByRole("button", { name: user.name, exact: true }).click();
  await expect(page.getByText(user.email, { exact: true })).toBeVisible();
  await page.getByRole("menuitem", { name: "Sign out", exact: true }).click();

  await expect(page.getByRole("link", { name: "Sign in", exact: true })).toBeVisible();
  await expect(page.getByRole("banner")).toHaveCount(0);
  await page.reload();
  await expect(page.getByRole("link", { name: "Sign in", exact: true })).toBeVisible();
  await expect(page.getByText("Agents arrive in the next milestone")).toHaveCount(0);
});

test("a cancelled sign-in shows a fixed message", async ({ page }) => {
  await gotoSignIn(page, "?error=access_denied&error_description=Injected%20IdP%20text");
  await expect(alertWith(page, "Sign-in was cancelled.")).toBeVisible();
  await expect(page.getByText("Injected IdP text")).toHaveCount(0);
});

test("an unknown error code shows only the generic message", async ({ page }) => {
  await gotoSignIn(page, "?error=%3Cscript%3E");
  await expect(alertWith(page, "Sign-in failed. Please try again.")).toBeVisible();
  await expect(page.getByText("<script>")).toHaveCount(0);
});
