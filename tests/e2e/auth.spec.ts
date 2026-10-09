import { expect, type Page, test } from "@playwright/test";
import { loginAtMockIdp, uniqueEmail, uniqueOrg } from "./support/mock-idp";

async function signIn(
  page: Page,
  email: string,
  claims: { name: string; org?: string; groups?: string[] },
  idpEmail = email,
) {
  await page.goto("/sign-in");
  await page.waitForLoadState("networkidle"); // the form needs hydration, or submit reloads the page
  await page.getByLabel("Work email").fill(email);
  await page.getByRole("button", { name: "Continue with SSO" }).click();
  await loginAtMockIdp(page, { email: idpEmail, ...claims });
}

// Next's route announcer is also a role=alert element, so match the text.
const alertWith = (page: Page, text: string) => page.getByRole("alert").filter({ hasText: text });

test("signed-out visitors are sent to sign-in from app pages", async ({ page }) => {
  await page.goto("/settings/members");
  await expect(page).toHaveURL(/\/sign-in$/);
});

test("an admin sees the organization and its members", async ({ page }) => {
  const org = uniqueOrg("acme");
  const email = uniqueEmail("admin", "corp.test");
  await signIn(page, email, { name: "Ada Admin", org, groups: ["agenty-admins"] });
  await expect(page.getByText(org, { exact: true })).toBeVisible();
  await page.getByRole("link", { name: "Members", exact: true }).click();
  await expect(page.getByRole("cell", { name: email, exact: true })).toBeVisible();
});

test("a member has no Members link and sees the not-found page", async ({ page, browser }) => {
  const org = uniqueOrg("acme");
  const adminEmail = uniqueEmail("admin", "corp.test");
  const memberEmail = uniqueEmail("member", "corp.test");

  const adminContext = await browser.newContext();
  const adminPage = await adminContext.newPage();
  await signIn(adminPage, adminEmail, { name: "Ada Admin", org, groups: ["agenty-admins"] });
  await expect(adminPage.getByText(org, { exact: true })).toBeVisible();
  await adminContext.close();

  await signIn(page, memberEmail, { name: "Max Member", org });
  await expect(page.getByText(org, { exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "Members", exact: true })).toHaveCount(0);

  // Not-found renders with HTTP 200 under Cache Components, so assert the UI only.
  await page.goto("/settings/members");
  await expect(page.getByText("This page could not be found.")).toBeVisible();
  await expect(page.getByRole("table")).toHaveCount(0);
  await expect(page.getByText(adminEmail)).toHaveCount(0);
});

test("a user of another organization sees only their own organization and members", async ({
  page,
  browser,
}) => {
  const acme = uniqueOrg("acme");
  const globex = uniqueOrg("globex");
  const acmeAdmin = uniqueEmail("acme-admin", "corp.test");
  const globexAdmin = uniqueEmail("globex-admin", "corp.test");

  const otherContext = await browser.newContext();
  const otherPage = await otherContext.newPage();
  await signIn(otherPage, acmeAdmin, { name: "Acme Admin", org: acme, groups: ["agenty-admins"] });
  await expect(otherPage.getByText(acme, { exact: true })).toBeVisible();
  await otherContext.close();

  await signIn(page, globexAdmin, {
    name: "Globex Admin",
    org: globex,
    groups: ["agenty-admins"],
  });
  await expect(page.getByText(globex, { exact: true })).toBeVisible();
  await expect(page.getByText(acme, { exact: true })).toHaveCount(0);
  await page.getByRole("link", { name: "Members", exact: true }).click();
  await expect(page.getByRole("cell", { name: globexAdmin, exact: true })).toBeVisible();
  await expect(page.getByRole("cell", { name: acmeAdmin, exact: true })).toHaveCount(0);
});

test("an organization owned by another provider cannot be joined", async ({ page, browser }) => {
  const org = uniqueOrg("acme");
  const corpContext = await browser.newContext();
  const corpPage = await corpContext.newPage();
  await signIn(corpPage, uniqueEmail("admin", "corp.test"), {
    name: "Ada Admin",
    org,
    groups: ["agenty-admins"],
  });
  await expect(corpPage.getByText(org, { exact: true })).toBeVisible();
  await corpContext.close();

  await signIn(page, uniqueEmail("pat", "partner.test"), { name: "Pat Partner", org });
  await expect(page).toHaveURL(/\/sign-in/);
  await expect(
    alertWith(page, "This organization belongs to a different identity provider."),
  ).toBeVisible();
});

test("an email domain without an identity provider is rejected", async ({ page }) => {
  await page.goto("/sign-in");
  await page.waitForLoadState("networkidle");
  await page.getByLabel("Work email").fill("eve@evil.test");
  await page.getByRole("button", { name: "Continue with SSO" }).click();
  await expect(
    alertWith(page, "No identity provider is configured for this email domain."),
  ).toBeVisible();
});

test("signing out ends the session", async ({ page }) => {
  const org = uniqueOrg("acme");
  await signIn(page, uniqueEmail("admin", "corp.test"), {
    name: "Ada Admin",
    org,
    groups: ["agenty-admins"],
  });
  await expect(page.getByText(org, { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Ada Admin", exact: true }).click();
  await page.getByRole("menuitem", { name: "Sign out", exact: true }).click();
  await expect(page).toHaveURL(/\/sign-in$/);
  await page.goto("/settings/members");
  await expect(page).toHaveURL(/\/sign-in$/);
});
