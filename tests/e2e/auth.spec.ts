import { expect, type Page, type Route, test } from "@playwright/test";
import { loginAtMockIdp, uniqueIdpUser } from "./support/mock-idp";

/** Opens the sign-in page once it is hydrated (the button is disabled until then). */
async function gotoSignIn(page: Page, query = "") {
  await page.goto(`/sign-in${query}`);
  await expect(page.getByRole("button", { name: "Sign in", exact: true })).toBeEnabled();
}

// Next's route announcer is also a role=alert element, so match the text.
const alertWith = (page: Page, text: string) => page.getByRole("alert").filter({ hasText: text });

/** Signs in through the mock IdP and waits for the signed-in home page. */
async function signIn(page: Page, user: { sub: string; email: string; name: string }) {
  await gotoSignIn(page);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await loginAtMockIdp(page, user);
  await expect(page.getByText("Agents arrive in the next milestone")).toBeVisible();
}

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
  await signIn(page, user);

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

test("a signed-in user who opens the sign-in page lands on the home page", async ({ page }) => {
  await signIn(page, uniqueIdpUser("alan", "Alan Turing"));

  await page.goto("/sign-in");

  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByText("Agents arrive in the next milestone")).toBeVisible();
});

for (const [failure, idpUser, fail] of [
  ["answers with an error", "edsger", (route: Route) => route.fulfill({ status: 500, json: {} })],
  ["is unreachable", "barbara", (route: Route) => route.abort()],
] as const) {
  test(`a sign-out that ${failure} shows a fixed message and keeps the session`, async ({
    page,
  }) => {
    const user = uniqueIdpUser(idpUser, "Signed In User");
    await signIn(page, user);
    await page.route("**/api/auth/sign-out", fail);

    await page.getByRole("button", { name: user.name, exact: true }).click();
    await page.getByRole("menuitem", { name: "Sign out", exact: true }).click();

    await expect(alertWith(page, "Sign-out failed. Please try again.")).toBeVisible();
    await page.unroute("**/api/auth/sign-out");
    await page.reload();
    await expect(page.getByText("Agents arrive in the next milestone")).toBeVisible();
  });
}

test.describe("with an unreachable identity provider", () => {
  // Second web server in playwright.config.ts: same build, OIDC discovery fails.
  test.use({ baseURL: "http://127.0.0.1:3101" });

  test("signing in shows that sign-in is temporarily unavailable", async ({ page }) => {
    await gotoSignIn(page);
    await page.getByRole("button", { name: "Sign in", exact: true }).click();

    await expect(
      alertWith(page, "Sign-in is temporarily unavailable. Please try again later."),
    ).toBeVisible();
  });
});
